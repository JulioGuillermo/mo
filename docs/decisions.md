# Decisions

## Context budget

- The built-in system prompt is four lines plus working directory, OS and date (~90 tokens). The
  15 tool definitions are ~5.6 KB (~1,400 tokens) in full mode. Tool descriptions stay one line;
  parameters carry no description: their names must be enough for a model that reads nothing else.
  What an action takes is in the tool's `help` action, asked when needed, not sent every time.
- Requests that fail at the network level are sent once more; llama-server closes keep-alive
  connections after streaming, and a repeated model request has no side effect.
- No limit without a way past it. Every tool that returns text takes the same view fields, applied
  by the registry to what the tool returns: `offset` (negative from the end), `limit` (2000 lines,
  200 for `fetch`), `col`, `width` (2000 bytes a line), `match` and `numbered`. What is left out is
  said with the `offset` or `col` that shows it. Seeing another part runs the tool again: listing,
  grep, git and fetch are cheap, and a page stays cached. The fixed caps (500 entries, 200
  matches, 400 blame lines, 8 KB pages) are gone.
- Output of other tools, MCP's, is cut to `tools.max_output` (10 KB) keeping 60 % head and 40 %
  tail: commands put their summary and errors at the end, files and listings the useful part first.
- No line numbers unless asked with `numbered`, everywhere alike: `edit` matches text, not lines, so
  numbers would mostly cost tokens. With `match` they are always there: the place is the point.
- One tool per subject, with `action` where it does several things: `bash` runs, reads output and
  kills; `memory` keeps notes and searches them and past conversations. `action` defaults to run in
  `bash`, so a plain command call is what every model expects.
- Nothing a command printed is lost: every `bash` call is a job whose output goes to disk in the
  conversation's folder, so it can be read later by lines or searched, also after a two-hour build
  that would cost as much again. `fetch`, `read` and `grep` are not kept: doing them again is cheap.

## Tool modes

- `full` sends every tool with its description, `compact` drops tool and parameter descriptions
  (names and schemas usually say enough to a capable model), `search` declares only the
  `always_loaded` tools and the search tools.
- Search mode has `tool_list` (names only, filtered by words of the name or description), and
  `tool_search` (what the matching tools do and their parameters, by keywords or `select:`). Names
  are cheap, so the model can look around before paying for descriptions. `tool_search` no longer
  lists the hidden tools in its own description: that list grew with every MCP server.
- `tools.load = "context"` (default): what `tool_search` returns stays in the conversation as its
  result, and the model calls those tools through `tool_call {name, args}`. The system prompt and
  the declared tools never change, so the prompt cache holds and no request carries more. A
  wrapper is needed because llama-server and Anthropic only take calls to declared tools.
- `tool_call` is unwrapped before anything else: approvals, rules, the loop guard and the UI see
  the tool it stands for, so it is no way around a confirmation.
- `tools.load = "declare"`: the tools found are declared from the next request on, as before, for
  providers that handle the wrapper badly. What is declared is read from the `tool_search` calls
  in the history, not kept apart, so resume, rewind, fork and `/clear` declare what that
  conversation found. After a compaction the search calls are gone and so are the declarations.
- A call to a tool that exists but is not declared still runs, with the usual approvals.
- `always_loaded` is `read`, `edit`, `write`, `bash`, `ask` and `memory` by default: the core of
  editing code; `write` because `edit` cannot create a file; `bash` covers `ls`, `grep`, `mkdir`
  and `rm`, and the default allow rules let its reading commands through; `ask` and `memory`
  because a model that does not see them does not think of looking for them, so it guesses
  instead of asking and never finds what earlier conversations and memories hold.

## Context and compaction

- Context window: `context.window`, else llama-server's `/props` `n_ctx`, else 32768 for local
  servers and 200000 for Anthropic.
- The size used is the provider's token count at the last response plus 4 bytes per token for
  the messages added since (tool results, the new prompt). With no count, it is all estimated.
- At `compact_at` (0.75) of the window the agent summarizes before the next request. The summary
  request is the history rendered as a plain transcript (tool results cut to 2 KB each, the whole
  cut to 60 % of the window) with no tools: with tools offered, small models answered with a tool
  call instead of a summary, and the transcript always fits.
- The turn in progress is kept verbatim when it takes under 30 % of the window. Otherwise it is
  summarized too and the user's current request is repeated after the summary, so the model does
  not redo finished steps.
- No new automatic compaction until the context has grown 10 % of the window past the size right
  after the last one, so a prompt that cannot shrink enough is not summarized before every request.
  A failed automatic compaction lets the turn go on and tells the user. A full window always tries
  again, unless the last compaction could not get below the window, where it cannot help.
- The request repeated after a summary is the user's original prompt, kept by the agent, not the
  first message of the history, which after a compaction already holds the previous summary.
- `compact_at = 0` turns automatic compaction off; `/compact` still works.
- `context.strategy = "drop"` is the other way to make room: whole turns are removed from the
  start, oldest first, until the context is under 75 % of the threshold, so it does not have to
  drop again on the next request. A turn starts at a user message, so no tool result is left
  without its call, and the turn in progress is never touched. It costs no model call and keeps
  the recent messages verbatim, but what was dropped is gone; `/compact` still summarizes.

## Memory

- One `memory` tool (list/read/write/delete) instead of four, to keep the tool list small.
- Files live in `memory.dir` (`~/.local/share/mo/memory`): `global/` and `projects/<dir>-<hash>/`.
  The hash of the full path keeps `/x/a-b` and `/x/a/b` apart.
- Memories are not put in the system prompt, not even their first lines: most are of no use to a
  given request and every request would carry them. The model finds them with `memory search`,
  or `memory list`, when it needs them.
- Writing or deleting a global memory always asks: a prompt-injected model could otherwise plant a
  note that reaches every future session in every project. Project memories do not ask.
- The memory folder is a protected path for `write`/`edit`, and the project file cannot move it.

## Providers

- Two protocols only: OpenAI chat completions (every local server speaks it) and Anthropic Messages.
  Gemini, Bedrock and Vertex can come later as more adapters behind `llm.Provider`.
- `anthropic` takes an API key only. A Claude account's sign-in is the separate `claude-code`
  provider, asked for by the user: Anthropic's terms limit it to Claude Code and have blocked other
  uses since 2026, so it can stop working. It signs in with Claude Code's OAuth client (token
  endpoint speaks JSON, hence `oauth.json`), sends the `oauth-2025-04-20` beta header, and puts
  "You are Claude Code, Anthropic's official CLI for Claude." first in the system prompt, which
  the API requires for these tokens, then a line saying the model runs inside mo and that mo's
  instructions take the place of Claude Code's. `preamble = false` leaves both out, at the risk
  of the API refusing the tokens; the bridge line is checked by tests, not against the API.
- `codex` is the Responses API as the ChatGPT backend serves it (`chatgpt.com/backend-api/codex`):
  system prompt as `instructions` (never empty), `store: false`, no `max_output_tokens`, and the
  ChatGPT account id, read from the access token's own claims, as a header. Reasoning items are
  not sent back, so the model re-reasons each turn.
- Antigravity is a third protocol, `cloudcode`: Gemini's own format inside the envelope of Google's
  Cloud Code Assist API (`v1internal:streamGenerateContent`), the one Antigravity speaks. It signs
  in with Antigravity's own OAuth client (id and secret are public, from its app), through the
  browser flow with a fixed loopback redirect (`oauth.redirect_url`), since that client has one
  registered. mo keeps its own token; it does not read opencode's or the IDE's. Only the `daily`
  sandbox endpoint serves it for a plain account: prod answers 429 and autopush 403. The request is
  sent as Antigravity's (`requestType: agent`, its system preamble and user agent), since that is
  what the API expects. A line right after the preamble says the model runs inside mo and that mo's
  instructions take the place of Antigravity's, so it does not read two identities. Both go with
  `preamble = false` on the provider, which the provider's form offers; on by default, as the
  API's expectations are not known for every model. Function calls keep their thought signature in `ToolCall.Extra`; a call
  without one gets Gemini's skip value.
- Anthropic requests mark the system prompt, the last tool and the last message as cache
  breakpoints, so every turn rereads the shared prefix from the prompt cache.
- Tool arguments cut off by the output token limit become `{}` plus `ToolCall.ArgsError`. Sending
  the broken JSON back would make every later request fail; the error tells the model to split the
  work.
- A stream fragment without `index` continues the last tool call unless it carries an id or a name.

## Safety

- The project file `.mo/settings.toml` may come with the repository, so it cannot change provider
  URLs or keys, define providers, pick a provider that needs a key (that would send the code to a
  paid cloud), move the memory folder, remove tools from `confirm` or `disabled`. It can pick a
  keyless provider, set models, tool mode and context settings, and add confirmations or disabled
  tools.
- A project file the user saves from `/settings` is trusted while its bytes stay the same: its
  sha256 is kept by path in `~/.local/share/mo/trusted-projects.json`, outside the repository.
  Trusted, it may also pick any provider and say which tools to confirm or disable. A pull or an
  edit by hand changes the hash, and the file is held back again until the user saves it; mo says
  so at start only when that changes something. Copying a trusted file to another folder does not
  carry the trust, as the path is part of the key.
- `write`, `edit`, `mkdir` and `rm` always ask, also with `-y` and after "always", when the path
  lands outside the working directory or on a protected path: mo's config, anything under a `.git`
  directory, or a hidden entry of the home directory. That closes the way from a prompt-injected
  model to `~/.bashrc`, git hooks or mo's own config, also when mo runs from `$HOME`.
- Paths are compared after resolving symlinks, so a link in a cloned repo cannot point the check
  elsewhere. `rm` resolves only the directories above the target, since it removes a link itself.
- Tool arguments must use the schema's names exactly. Go's JSON decoding ignores case, so `"Path"`
  would otherwise reach the tool while the guard read `"path"`.
- `rm` refuses `/`, top-level directories, the home and working directories and anything above them.
- `bash` runs in its own session (`setsid`): no controlling terminal, so password prompts fail at
  once instead of hanging, and a cancel kills the whole process group. Its output buffer keeps
  1 MB of head and tail, so `yes` cannot eat the memory. A background child holding the pipe open
  ends the call after 2 s as a success.
- Enter presses the selected button, and "No" is selected first: approving takes `y`, or moving to
  "Yes" on purpose, so typing into the input cannot approve a command by accident.

## Permissions

- Four modes, strictest first: `ask`, `edits`, `auto`, `all`. `edits` is the default and is what mo
  did before modes: `tools.confirm` asks, file changes in the working directory do not. `ask` adds
  file changes and memory writes; reading never asks, it is what the model needs to do anything.
- Rules apply to the tools that run arbitrary things, `bash` and `fetch`. Other tools in `confirm`
  (`rm`, MCP tools) keep "always this session", per tool.
- A script is split with mvdan.cc/sh, the parser of shfmt, not by hand: quotes, `$(...)`,
  backquotes, loops and heredocs are where hand splitting lets a second command slip past a rule
  for the first. Every simple command must be allowed. A script that does not parse asks.
- Words are matched with their quotes removed; a word with an expansion stays as written
  (`"$HOME/x"`), so a rule naming a path cannot be met by a variable that points elsewhere.
- A command writing to a file through a redirection asks even when a rule allows it, since
  `echo *` would otherwise allow `echo x >> ~/.bashrc`. `/dev/null` and `2>&1` do not count.
- Deny rules ask in every mode, also `all`, and the question offers no "always". They do not block:
  the user may still want to run the command once.
- `*` alone matches any words or none, so `ls *` takes `ls`; inside a word it does not cross a
  space, so `git log*` does not take `git log -p`. `re:` covers the rest.
- Per-folder rules live in the user file under `[permissions.projects."<path>"]`, never in the
  repository, trusted or not: a cloned project file with `allow = ["*"]` would run anything. The project file may add
  deny rules and pick a stricter mode only.
- Rules added from a question are saved at once, without waiting for `/settings`. With no
  `allow` list in the user file yet, the defaults are written first, so adding one rule does not
  drop the built-in read-only ones.
- `auto` asks the session's model in a separate request with only the user's request (cut to
  2000 characters), the working directory and the commands; no tool output, so injected text
  cannot vouch for itself. The answer must start with SAFE or UNSAFE; anything else asks. A command
  judged safe is not judged again in the session. Each judgement costs one request; rules avoid it.
- The mode chosen with shift+tab stays when `/settings` is saved, unless the saved settings change
  the mode to start in.
- On a narrow terminal the status line drops, in order: the shift+tab hint, the provider, the
  prompt speed, the token counts, the generation speed, then shortens the context bar; the mode,
  model and context share stay.

## TUI

- Full screen (alt screen) instead of inline: the conversation is a viewport that follows the end
  unless scrolled up, and the input (shaded) and the status line always hold the last rows. The
  price is the terminal's own scrollback: the conversation is gone from the screen after quitting.
  Kitty image uploads are sent to the terminal once and only their placeholders stay in the
  transcript, so redrawing never re-sends an image.

- The user's messages print as a full-width shaded band; commands and approvals show their command
  on shaded lines, and an approval is a framed question with its keys in colour.
- Attached files are found in the prompt itself (paths, quoted or with escaped spaces, that exist
  as regular files), so pasting and dropping work in any terminal. Images (png, jpeg, gif, webp, up
  to 8 MB) go with the message; images marked in read and MCP results follow the tool results as a
  `[mo]` note, which drop, compact and the loop guard never take for the user's prompt. A request
  that fails while the history holds images is sent once more without them.
- Speeds are llama-server's `timings` when present, else measured: prompt tokens over the time to
  the first piece, answer tokens over the rest. A cached prompt reads as fast.

- Inline, not alternate screen: finished output goes to the terminal's own scrollback, where
  selection, search and copy work as usual. Only input, streaming text and status are redrawn.
- The agent goroutine prints with `Program.Println`, which queues in the program's message channel.
  Printing through `tea.Println` commands from `Update` would run each command in its own
  goroutine and could reorder lines.
- Quitting while a run is active cancels it and waits up to 3 s so running commands get killed.
  Prints from the agent stop once the program has exited, because `Program.Println` blocks forever
  when nobody reads the channel.

## Settings and detection

- Every key of the config file has a place in the UI, so the file is never the only way:
  `/settings` holds the single values, `/providers` and `/mcp` the lists, each a picker form that
  opens an edit form for one entry. The project's own settings are the second screen of `/settings`.
- `/settings` is a huh v2 form embedded in the inline UI: model, tools, context (with the token
  cost of the system prompt and of each tool), tool descriptions, loop guard, web, memory and
  display. Descriptions cover the MCP and plugin tools too, compared with each tool's own; saved
  ones of tools not loaded now are kept.
- A provider entry keeps only what differs from its preset, since the file is laid over the presets
  field by field. The same layering means a preset field cannot be emptied from the file: emptied
  in the form it falls back to the preset. For keys that does not matter, as `api_key` and then
  `api_key_from` win over the preset's `api_key_env`. An empty key field keeps the saved key;
  forgetting it is its own choice. Deleting a preset's entry resets it; deleting the provider in
  use goes back to auto. The provider in use takes a top-level `model` into its own entry, and
  choosing another provider leaves the top-level model behind, since it would override any choice.
- Secrets typed in a form are never shown again: key fields are password fields, empty keeps the
  saved key, and MCP environment and header values show as `••••••` unless they only read the
  environment (`${NAME}`, `Bearer ${NAME}`). Leaving `••••••` keeps the saved value.
- MCP edits reconnect the servers: a new hub is connected and the session built on it, and it
  replaces the old one only once the file is saved; if saving fails the new hub is closed instead.
  A hub that connects after mo closed its hubs at exit is closed as it arrives. Ceiling: any MCP
  change restarts every server; reconnecting only the changed ones is the way out if it matters.
- The setup screen at start replaces the fatal "no provider" error in interactive mode, only for
  errors about the provider; any other bad setting fails as before. It shows the error, and offers
  the providers mo knows or found (asking the key of one that needs it, even when a key is set,
  since the set one may be wrong, and a model), a new one with the same fields as `/providers`,
  going on anyway (only with a session that was built and just does not answer) and quitting.
  Each pick is built and checked before it is saved. A pick that was built but does not answer
  becomes what going on keeps, and the next round starts from it; a pick that could not be built
  is not kept. A provider is checked by listing an OpenAI-compatible server's models (3 s), and a
  404 there counts as an answer; an Anthropic key is only checked when used. One-shot mode keeps
  failing fast instead.
- The form starts from the defaults plus the user file, not from the session: a provider picked
  with `-p` or by a project's settings is never copied into the global config. Provider and model
  are written only when changed in the form; a typed model goes where the file already keeps one
  (top-level `model`, else the provider's entry); an empty model keeps following the server.
  Everything else is written only when it differs from the defaults.
- Saving first builds a session from the edited settings and writes the file only if that works,
  so a provider without a key never ends up saved as the default.
- Model suggestions are gathered once when the form opens: the provider in use (with its key) and
  the local servers detection already asked. Nothing is requested while moving through the list.
- While the settings load or save no run can start, and agent messages bypass the form.
- "Always" approvals do not survive a settings change; the new confirm list decides.
- The file is encoded from a map of the fields that are set, not from the struct: the TOML encoder
  drops empty lists with `omitempty`, and `confirm = []` and `compact_at = 0` must survive.
- After saving, the session is rebuilt from the files and takes over the conversation, so a change
  of provider, tools or prompt applies on the next message.
- A key comes from the first of `api_key`, `api_key_from` and `api_key_env` that is set: a login
  picked for a preset must win over the preset's own variable.
- Detection reads env keys, probes keyless local providers in parallel (500 ms each), and reads
  codex and opencode files. Keys from those files stay there: the config gets `api_key_from`,
  resolved when a request needs it. OAuth subscription logins are reported, never read for a key.
- A codex `OPENAI_API_KEY` stored next to a ChatGPT login is used only when `auth_mode` is
  `apikey`: some codex versions mint that key from the subscription login.
- An imported provider keeps the preset's address but none of mo's own key settings, so the key
  it uses is the imported one.
- An opencode provider with a literal `apiKey` is listed but not imported, to avoid copying it.

## Web

- `fetch` keeps the article readability finds, else the whole page, converted to markdown with
  absolute links, and shows 200 lines unless the model asks for more, so a long page costs what
  it actually reads. The last 16 pages are cached per session.
- `fetch` asks by default: a prompt-injected model could send what it read to any URL. Reading on
  in a page already fetched does not ask again. Redirects are followed within the same host only;
  one to another host ends the fetch with its target, which then goes through approval itself,
  so an approved public URL cannot bounce to localhost or a metadata address.
  A hostname that resolves to a private or loopback address is not refused: the approval shows the
  host, and refusing them would keep fetch away from the user's own dev servers.
- `web_search` does not ask: its queries go to the configured engine only. Its client never
  follows redirects, since a DuckDuckGo `!bang` answers with a redirect that would carry the query
  to another site. Searches stop after 30 s and read at most 2 MB.
- DuckDuckGo's HTML page is the default search because it needs no key; it can answer with a bot
  check, and the error says which backend to configure instead.
- `[web]` is user-only: a project could otherwise point searches, and a key, at its own server.

## MCP

- The official Go SDK. Servers are connected in parallel once at startup (20 s each) and kept
  across settings changes; a failing server is reported and leaves no tools.
- Tools are named `mcp__<server>__<tool>` with unsafe characters replaced, which every provider
  accepts. A name over 64 characters, or one another tool already took after that cleaning, is cut
  and ends in a hash of the original server and tool, so no two tools share a name.
- Calls stop after 5 minutes. A server whose connection broke shows as failed in `/mcp`.
- Closing signals each stdio server's whole process group: started in its own session, the
  helpers it spawns (npx, browsers) would otherwise outlive mo.
- `${NAME}` in env and header values reads the environment; any other `$` stays, so tokens with a
  `$` in them survive.
- They ask before running unless the server has `trust = true`: an MCP tool can do anything.
- stdio servers run in their own session (`setsid`), so they cannot read the terminal, and their
  stderr goes to a log file instead of the screen.
- `[mcp]` is user-only: a project adding a server would be running any command it likes.
- Images and audio in tool results are only named for now.

## Rendering

- Images go into the scrollback, so they scroll with the conversation. In kitty and ghostty they use
  kitty's Unicode placeholders: the image is sent once (quiet, `q=2`, so the terminal answers
  nothing on stdin) as a virtual placement, and the visible part is ordinary text, one placeholder
  per cell with the image id in its colour. The UI measures and moves it like any text. Inside tmux
  the transmission is wrapped for passthrough.
- Elsewhere, with truecolor, half blocks: two pixel rows per text row. Without truecolor, a name.
- Detection reads the environment only (TERM, TERM_PROGRAM, KITTY_WINDOW_ID, COLORTERM): querying
  the terminal would race with the UI reading the keyboard. Inside tmux, kitty graphics are used
  only when `allow-passthrough` is on, else half blocks. `[render] images` overrides it all.
- Images are checked by their header first and refused over 50 megapixels, so a tiny file that
  declares a huge size cannot exhaust memory. Kitty gets the image scaled to the cells it fills,
  not its full resolution, which keeps the transmission small and the terminal's image store free.
- SVG goes through resvg compiled to WebAssembly, so the binary stays free of cgo. Its own system
  font loading finds nothing, so mo loads one common sans font as data and puts it first in every
  font-family (attribute, style attribute, style element): this resvg tries only the first family
  and drops text whose font is missing. SVG is checked as XML first, since resvg prints its parse
  panics to the terminal; a root without xmlns gets it, one in another namespace is refused. The
  size comes from the viewBox, scaled by an absolute width or height (px, pt, pc, mm, cm, in);
  % and em have no size of their own. The root is rewritten with that size, because resvg scales
  each axis against its own reading of it, and the SVG is drawn straight at the size it is shown.
- Mermaid: mermaid-ascii draws flowcharts and sequence diagrams with box characters anywhere; with
  mermaid-cli installed and kitty graphics, the diagram is drawn as an image, with labels as SVG
  text since resvg draws no HTML labels. Half blocks are too coarse for a diagram. What neither
  draws stays its source.
- Images in tool results are written to a file and marked `[image <what>: <path>]`, so the model
  knows where they are, and can show them again with `![](path)`, and the UI draws them. The files
  go to a private directory made for the run (0700) and removed at exit; a shared, predictable
  /tmp/mo-images would let another user read them or plant links.
- Code blocks are tracked when splitting an answer: an image line or a mermaid fence inside, say,
  a README in a markdown block stays text.

## Plugins

- Lua through gopher-lua: pure Go, so the binary stays single and cgo-free. JavaScript (goja) was
  the other candidate; Lua is smaller to embed and to learn for short scripts.
- Only the user's `~/.config/mo/plugins/*.lua` load, in name order; a project never brings code to
  run. The folder is a protected path, so the model cannot write a plugin without being asked.
- Each plugin has its own Lua state with base, table, string and math; `io`, `os`, `require`,
  `dofile` and `loadfile` are removed and the system is reached through the `mo` table instead.
  That is not a sandbox against the user's own code (`mo.exec` runs bash); it keeps the surface
  small and every effect visible in one place.
- A Lua state is not safe to share, so calls into one plugin take turns, each with a time limit:
  10 s to load, 5 s per hook, 2 minutes per tool or command. Waiting for the turn also ends with
  the caller's context, so a busy plugin cannot freeze a prompt and Esc works. Lua code stops at
  its limit; work stuck inside Go (a runaway pattern match) is abandoned 3 s later, longer than
  `mo.exec` waits for its output, and the plugin is turned off for the run, since its state can no
  longer be trusted. A guard turned off keeps blocking, like one that failed to load.
- Plugin output is bounded: `mo.exec` keeps 1 MB of head and tail, `string.rep` refuses results
  over 16 MB, converting a Lua value visits at most 100 000 parts, command output is cut to 64 KB
  and a prompt hook result over 256 KB is ignored.
- Declarations (`mo.tool`, `mo.command`, `mo.on`) only work while the file loads; afterwards other
  goroutines read them without locks.
- A `tool_call` hook that fails blocks the call, and so does a plugin that guards tool calls and
  failed to load: a guard that broke should not let everything through. Load errors are shown in
  the UI when it starts. A `prompt` hook that fails leaves the prompt as it was.
- mo's own command names are refused for plugin commands, and commands dispatch on their first
  word, so `/settings x` is always mo's. A plugin command runs like a turn of the agent.
- Plugin tools whose name another tool already has, `tool_search` included, are left out and
  reported. Names start with a letter or `_`, which every provider accepts. They run without
  asking unless declared with `confirm = true`: the user wrote them.
- A blocked call is shown like any other, with its reason, and the model receives the reason.

## Loop guard

- **A tool loop is the same call with the same result**, not the same call alone. Only a call
  whose arguments (keys sorted) and output both match earlier ones counts, and a different call
  that may change something (anything but read, ls, grep, fetch, web_search, tool_search) ends
  the count: running the tests again after an edit is progress even when they fail the same way.
  A read-only call in between does not, so an A, B, A, B cycle of lookups is still caught; a cycle
  of two bash commands is not, and is left to the stream guard and `warnings`. The
  blocked call gets an error result saying so, which keeps the history valid for every provider
  and needs no extra message. Results pair with calls by position, since local servers reuse ids.
- **Only the current turn counts.** The look-back stops at the user's last prompt: when the user
  asks to run something again, that is not the model looping. `replies` bounds it further, so a
  long turn that comes back to the same file much later is not punished.
- **A stream loop is one piece written `stream_repeats` times back to back** at the end of the
  stream: a phrase, a line or a paragraph, 16 to 2000 bytes, starting on a whole character, with
  at least 8 different letters. A piece with only 2 to 7, such as `Wait, ` or `ha ha `, needs four
  times the copies; one with fewer never counts, so rules (ASCII or `═`), table borders, blank
  lines, zero-filled arrays and base64 padding are left alone. The piece is quoted from where
  its run of copies begins, the way it reads. Scattered repeats are not loops; a model that
  writes the same line between other lines is not stuck. Text and reasoning are watched apart.
  Each delta checks every piece size against the last copies; mismatches exit on the first
  bytes and a letters-from-the-end table skips the rest, so it stays cheap per token.
- **A cut reply is dropped whole** and the model gets a user message, prefixed `[mo loop guard]`,
  quoting what it repeated. A system message is not used: Anthropic has no system messages mid
  conversation and several llama.cpp chat templates reject one that is not first. When the last
  message is already the user's, the note is appended to it, since some templates also refuse
  two user messages in a row; after tool results it goes as its own user message, the same shape
  as a prompt sent after a cancel.
- **`warnings` loops are told to the model per turn, the next one stops the turn** with
  `ErrLooping`, so a model that cannot get out does not burn the context. Every pending tool call
  still gets a result, as after a cancel.
- A loop note is never taken for the user's prompt: dropping and compacting look for the turn
  start past them.
- The project file may tune or turn off the guard: at worst it costs tokens, nothing leaves the
  machine.
- In one-shot mode the cut text is already on stdout; mo only adds a line break and says so on
  stderr. The TUI clears it, since streamed text is only drawn live until the reply ends.
- Ceiling: identical rows of code, five in a row, shown in a reply are cut too; `stream_repeats`
  raises the bar. A user who asks for repeated text on purpose needs it raised or the guard off.
- Ceiling: exact repeats only. A model that loops with a changing counter or reworded sentences
  is not caught; fuzzy matching would be the way out, at the price of false positives.

## Conversations

- One JSON file per conversation, the whole history in it, rewritten after each turn: a turn adds
  little next to what a model reads, and one file per conversation needs no index to keep in step.
  Files are 0600 in a 0700 folder; they hold everything said and every file read.
- Conversations belong to the folder they ran in; `/resume` lists only those of the working
  directory, so a fix in one project does not show up in another.
- The transcript is drawn again from the history on resume and rewind instead of being saved: it
  depends on the width and theme, and mo's notes to the model are left out of it.
- Going back is a fork, never an edit in place: the new branch gets a new id once its first turn
  starts, and names the conversation it came from. Nothing is saved until then, so going back and
  changing one's mind loses nothing.
- Conversations are deleted only when asked: `/delete`, ✕ in `/resume`, or `/clean <date or age>`,
  each after a confirmation. Deleting stops the jobs it runs and removes its output folder.
- Going back does not restore files. `write` and `edit` could keep copies, but `bash` changes
  (commits, installs, deletions) cannot be undone that way, and a partial undo would mislead.
- A prompt holding a compaction summary is not offered to go back to: what came before it is gone.
- Tool calls are one block, started when the call starts and filled when it ends, matched by call
  id from the end, since some servers reuse ids like `call_0` in every reply.

## Notifications

- The terminal's own notification sequences come first (OSC 777 for Ghostty, WezTerm, foot and
  urxvt; OSC 9 for iTerm2; OSC 99 for kitty): they reach the user's desktop over ssh, where
  `notify-send` would run on the remote machine. Inside tmux they are wrapped for passthrough.
- The body comes from the model, so control characters, C1 codes and invalid UTF-8 are removed and
  it is kept to one line: an answer cannot end the sequence early and write its own.
- Only while the terminal is not focused, by default: a notification for what one is watching is
  noise. Focus comes from the terminal's focus reports; a key press also means focus, for
  terminals that do not report it.
- No notification after esc: the user is there.

## Todo

- The model sends the whole list on every `todo` call, with the statuses models already know
  (`pending`, `in_progress`, `completed`); "done" and "doing" are taken too. A list that is all
  sent at once cannot drift from a stored copy, and small models get it right more often than
  edits by id.
- The tool keeps no state: the list in force is the latest call's, read from the history. Resume,
  rewind and fork therefore show the list as it was at that point with nothing else saved.
- ~70 more tokens of tool definitions in full mode. `disabled = ["todo"]` takes them back.

## Colours

- mo paints its own background (`render.background`, `#00081a` by default) through the terminal's
  background colour sequence, restored on exit, so it looks the same in any terminal theme.
  Whether the text is drawn for a dark or light screen follows that colour, not the terminal's.
  `terminal` keeps the terminal's background and asks it whether it is dark.
- Inline code and code blocks are on blue, table stripes too; glamour's grey did not sit on a
  blue screen.

## Questions and approvals

- The model asks with an `ask` tool: up to 4 questions, each free text or with up to 6 options, one
  or several to pick; the user can always answer in their own words, and an empty answer skips.
- Every call of a reply is checked before any runs, and what needs the user, the calls to approve
  and the questions, is asked at once. The user sees how many there are and in what order, and can
  go between them with tab before the agent goes on.
- The loop guard runs twice: before asking, so a call that repeats earlier replies is never offered
  for approval, and again when each call runs, since a call repeated within the same reply only
  shows as a repeat once the first one has a result.
- The dialog sits above the input, not in the conversation, so it stays in sight however the
  conversation scrolls. It is drawn with backgrounds, not a border, and buttons for the answers.
- "Always this session" answers the other calls to the same tool waiting in the dialog; rules
  added from one call answer the others they cover in every part.
- The command to approve is shown whole, wrapped rather than cut, coloured by chroma's bash lexer
  with mo's own colours (chroma's styles paint their own background), and scrolls with ↑↓ and
  pgup/pgdn past about half the screen. A bash command also lists its parts as the permission
  rules see them: allowed and by which rule, denied, not covered, or writing a file. What is
  approved is what is shown.
- Without a user (mo run once), approvals are denied and the model is told to decide its questions
  itself and say what it assumed.

## Background jobs and the side panel

- Every `bash` call is a job numbered within its conversation, in the foreground or with
  `background: true` in its own session, returning its number at once. Its output goes to
  `sessions/<id>/jobs/<n>.log` as it comes, with its state in `<n>.json`: the first 25 MB, and the
  last 25 MB in a tail file trimmed as it grows, joined after a line saying how much was dropped.
  A conversation keeps 500 MB; past it the output of its oldest ended jobs goes. Not in `/tmp`,
  which is often RAM, and not inside the conversation's JSON, rewritten whole after each turn.
- The model sees only the jobs of the conversation in progress, those of earlier runs too; one
  left running by a mo that is gone is "lost". A fork gets the output before it as hard links.
  An ended job is told to the model of its own conversation only, when that one is in progress.
- A job outlives the turn that started it and survives a settings change; mo kills what is left
  (SIGTERM, then SIGKILL after 3 s) when it exits. mo run once keeps nothing and has no jobs.
- Where the output is kept is a setting: `disk` by default, `memory` for nothing on disk (gone when
  mo exits), `none` for only what the command returned. `output_turns` lets go of the output of
  turns older than the last N, counted in the conversation's prompts so a restart keeps the count.
  The job's state stays, marked as pruned, so its number still says what ran.
- The conversation gets its id when its first turn starts, not when it is first saved, so its
  commands have a folder from the first one.
- Every command shows in the side panel while it runs, its dot red when it blocks the model and
  yellow in the background; once it ends only those run in the background stay there.
- A tool call's dot says what it does: red a command that blocks the model, yellow one in the
  background, green reading a command's output, pink a change, blue a read.
- The live viewer reads the job's file again every 300 ms while it runs, rather than streaming:
  the output is on disk already, and one reader for running and ended jobs keeps it simple. It
  takes the whole screen like settings do, and the agent goes on underneath.
- `bash` in the background with `until` (a regexp) or `wait` (seconds) returns when a line of the
  output matches, the time is up or the command ends, with what it printed: a server's start is
  read in the same call instead of after a blind sleep. One that ended meanwhile is not told
  again later.
- The `git` tool reimplements git (go-git), so it takes no git flags; its description sends what
  it does not do to git in `bash`. Its output is kept short by default: a log is one line a commit,
  about a seventh of `git log`.
- ctrl+b sends a command that blocks the model to the background: the call returns at once with
  what it printed so far, and the job goes on, told when it ends like one started in the background.
- Reading a job's output and help never ask; running and `kill` are checked against the rules, a
  kill as "kill job <n>", which no rule lets through alone.
- When a job ends the model is told with a note holding its last 15 lines: before its next step if
  it is working, appended to the last user-side message so two user messages never follow; and if
  it is idle, in a turn of its own started from the note, so it can react without waiting for the
  user. Also after esc: the user started the job to hear when it ends, and a stop of the model is
  not a stop of the job. A note says why the model goes on; esc again stops it, and nothing is left
  to start it once more. A job killed on purpose is not told as an end.
- The side panel follows what is being done: the conversation, the tasks whole and wrapped, the
  interfaces, and the jobs with their state and time. A section shows only once it has
  something, so an empty one takes no room; with none, one dim line says so. It shows from 110 columns, 36 wide; the conversation is drawn
  at the width left. Narrower, the tasks go back above the input.

## Mouse

- mo takes the mouse for clicks and the wheel, so the terminal no longer selects; mo selects
  instead. A drag picks text within the column it started in, conversation or side panel, shows
  it reversed, and on release copies it through OSC 52 and the desktop's clipboard command when
  there is one. Rows lose their trailing blanks and the indent they share, counted from the line's
  start for a first row picked mid-line, so code pastes as it was. A press and release on the
  same cell is a click.
- Clickable parts are regions recorded as the screen is laid out, from one layout shared by the
  view and the click: nothing can be clicked where it is not drawn.
- huh takes no mouse, so a click on a settings list moves its cursor there and presses enter.
- Buttons underline the key that presses them, in the warning colour, so the mouse teaches the
  keys.
- Beside the side panel, what sits over the input (the dialog, the spinner, the commands that
  match) takes the conversation's width, and the panel runs down to the input. Without the panel
  the tasks sit over the input on the panel's background.

## Recall

- `memory search` searches memories and saved conversations with BM25 and returns ids with a piece of text
  around the match; `read` gives a memory whole, a conversation's turns, or one turn whole. The
  model decides what to look for and what to read, instead of carrying an index it rarely needs.
- A conversation is split at the user's prompts: a turn is the prompt, the answers and the tool
  calls with the start of their results (300 characters when searching, whole when read). mo's own
  notes stay with the turn they belong to; a compaction summary is a turn of its own.
- `scope = here` (default) keeps to this folder's conversations and to this project's and the
  global memories; `all` takes every folder. The conversation in progress is left out: the model
  has it already.
- Conversations are parsed once and kept until their file changes, so a search after the first
  reads only what was saved since.
- No embeddings for now. BM25 is pure Go and finds names, errors and commands well; an embeddings
  endpoint could be added later and the two rankings merged.
- Ids are checked: a memory id must name a scope folder and a memory name, a conversation id is
  refused when it holds a path, so `read` cannot leave the memory or session folder.

## Git

- A `git` tool on go-git, pure Go: it works where the git command is missing, and its reading
  actions (`status`, `diff`, `log`, `show`, `blame`, `branches`, `check`) never ask. The others
  count as file changes: they ask in ask mode. go-git adds about 3.4 MB to the binary, measured.
- go-git merges only by fast-forward, so mo has its own three-way merge: Myers line diffs and a
  diff3 merge. Merge and cherry-pick use it; a cherry-pick is a merge whose base is the picked
  commit's parent, committed with its author and a "cherry picked from" line.
- Conflicts are left in the files as diff3 markers (ours, base, theirs) for the model to resolve
  with `edit`. The state is written as git writes it (`MERGE_HEAD`, `CHERRY_PICK_HEAD`,
  `MERGE_MSG`), so the git command can take over a merge mo left half done, and mo keeps the
  conflicted and written paths beside them.
- `check` lists what is in progress and where markers are left. A marker is the whole sequence,
  `<<<<<<<`, `=======`, `>>>>>>>`, so a markdown heading underlined with `=======` is not one.
  `commit` and `continue` refuse while any changed or conflicted file holds one, naming file and
  line, so the model cannot commit half a resolution.
- Merge, cherry-pick and switch refuse over uncommitted changes to tracked files: a merge could
  write over them, and go-git's checkout keeping changes carries the old branch's index along.
- A file deleted on one side and changed on the other keeps the changed one; a binary changed on
  both keeps ours; both are reported as conflicts. 
- Hooks do not run, and the result of a commit says so.
- `rebase` replays the branch's commits onto the target as picks, each with its author and message,
  and stops at a conflict like a merge: `continue` commits the resolved one and replays the rest,
  `abort` puts the branch back. Merge commits are not rebased. A commit whose changes are already
  there is dropped, as git drops it. The rebase state is mo's own (`MO_REBASE`).
- `fetch`, `pull` and `push` use go-git: an SSH agent or a key without passphrase, or a token from
  `GIT_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN` or `GITLAB_TOKEN` for HTTPS. go-git does not use git's
  credential helpers, so when it cannot get in and the git command is installed, that runs
  instead. `pull` is a fetch and the three-way merge, so it is not limited to a fast-forward.
- `pull` takes any branch of any remote; without one, the branch the current one tracks
  (`branch.<name>.remote` and `.merge`), else origin and the same name. With `rebase: true` it
  replays the branch's commits on theirs with mo's rebase, stopping at conflicts like any rebase;
  unset, it does what the user's git does: `branch.<name>.rebase`, then `pull.rebase`. Any value
  but false rebases: `merges` and `interactive` have no meaning without a terminal or merge
  commits to keep, so mo replays the commits.
- `push` never forces; a push behind the remote is refused with "pull first". It warns, so it asks
  in every mode but all: it publishes outside the machine.
- go-git's hard reset and checkout delete untracked files, which git never does. mo uses neither:
  `switch`, fast-forwards, `abort`, rebase, stash and `reset hard` reset the index and branch, then
  write back only the tracked files that differ and remove those the target lacks.
- `stash` is stored as git stores it, a commit of the index and one of the working tree on top of
  HEAD at `refs/stash`, with the list in its log, so `git stash list` shows mo's stashes and the
  other way round. Untracked files are left out and left alone. `stash_apply` and `stash_pop` use
  the three-way merge, so a stash applies over new commits; a clash leaves markers and `pop` keeps
  the stash.
- `tag` makes a light tag, or an annotated one with a message. `reset` is soft, mixed or hard;
  hard warns, so it asks in every mode but all, and so does `stash_drop`.
- go-git runs `git-upload-pack` for remotes that are folders. Without git installed, mo serves them
  with go-git's own server, which can leave objects out of a fetch that is not a fast-forward, so
  git's is used wherever it is installed. SSH and HTTPS remotes need neither.

## Tool calls that change files

- An opened `write` shows what it wrote and an opened `edit` the diff from old to new, both
  coloured by the file's language; folded they say how many lines were written, added and
  removed. Each diff line is coloured on its own, so a string spanning lines may colour wrong.

## Editing providers and MCP servers

- A provider or an MCP server is filled in like the settings: a list of its fields with their
  values, each opened alone (enter keeps, esc puts the old value back), then Save; Delete asks
  first. A save that does not work says why at the top and keeps what was typed.
- An MCP server picks its type first, a command mo starts or a URL already running, and shows only
  that type's fields; saving drops the other type's, so a server switched from one to the other
  does not keep both.
- The setup screen that runs before mo starts still asks a new provider's fields in one form, built
  from the same entries.
- Yes/no questions take a click too: the first button accepts and the second rejects, as y and n.

## Git renames and credentials

- A merge follows renames on either side: a file gone from its path and the same, or with half its
  lines in common (git's default), at a new one is the same file. Its versions merge into the new
  name, so an edit on one side and a rename on the other merge cleanly. Renamed apart on both
  sides, the content merges into ours and the clash is reported. Pairs beyond 10000 are matched
  only by identical content, and files over 1 MB or binary only that way too.
- HTTPS remotes ask git's credential helpers first, as git does, from the user's config then the
  repository's, a `credential.<url>` section before the general one: `store` is read in Go
  (`~/.git-credentials` or its `--file`), others run as `git-credential-<name> get`, or the
  command a `!` helper names, with git's protocol. Git Credential Manager, libsecret and the macOS
  keychain work that way without git. `cache` needs git's own daemon and is left to it.

## Tool calls as they form

- Providers stream a tool call in pieces, the name then its JSON arguments; those pieces now reach
  the UI as events. A call shows as soon as its tool is named, with a spinner, and what it is about
  to do forms below it: a bash command, or the last 12 lines of a file being written or edited,
  coloured by its language, with how many lines so far. A call through `tool_call` shows the tool
  it stands for. When the call is whole the view gives way to the usual block.
- The arguments are JSON not finished yet, so a field is read as far as it came: the string being
  written is taken without its closing quote, escapes decoded, a cut escape left out.
- The loop guard leaves the pieces of tool calls alone: a file with repeated lines is no loop.
- The spinner says what the agent waits on besides the reply: compacting the conversation, or the
  model judging a command in auto mode.
- While the agent works, the spinner says what it does in a word
  with its colour: reasoning (blue), writing (yellow), reading with a tool (green), changing with
  one (red: write, edit, mkdir, rm, bash, and what the registry counts as a change or a warning),
  compacting (purple), reading the prompt with how far it is (teal, llama-server only), waiting for the model (grey), and your turn when a dialog waits on the user.
- Images and diagrams start where the text beside them does, two columns in for an answer, four
  for a tool result after its ⎿, and are drawn in the width left. They are lines of placeholder
  cells or colour blocks, so the indent is spaces before each line; a kitty upload at the start of
  the first line takes no room.

## Continue

- An answer stopped with esc stays in the history, marked as stopped there, so the model knows
  what it had said. `/continue` starts a turn without a prompt: stopped before any answer, the
  model answers the prompt waiting; stopped later, a note tells it to go on from where it was and
  redo what was cancelled if still needed.

## While the model works

- A message written while the model works is queued and shown at once; the model reads it at its
  next step, after the tool call running, as a message of the user's. Queued after its last step,
  it starts a turn of its own. Esc gives what was queued back to the input.
- Commands that leave the conversation alone work meanwhile: settings, providers, MCP, help, jobs,
  kill, the side panel, view, plugins, notify. Settings saved then are written to the files at
  once, so reopening the settings shows them and quitting keeps them; the session built from them
  takes over when the turn ends, since a new session cannot replace the agent mid-step. A second
  save meanwhile replaces the waiting session with one built from the files as they now are. Those that change the conversation (clear,
  compact, rewind, fork, resume, continue) ask to wait or stop.
- A stop takes out the tool calls it cut short, not run or stopped running, with their results, as
  if the model had not made them; the calls that ran and the reply's text stay.

## Global and project settings

- `/settings` has two screens, Global and This project, switched with tab or from a row of each
  menu; save writes both files at once. The project screen shows only what a project overrides:
  provider and model, max output tokens, tools, context and prompts, the loop guard. Providers,
  MCP servers, web search, memory, notifications and display are the user's, for every project.
- A setting is overridden when the project file sets it, even to the global value, so a file
  shared by a team keeps it whatever each user's global settings are. The rest follows Global as
  it is edited, also in the same /settings session. "Use the global value" takes the key out.
- The project file lives in `.mo/` so there is room for more project files later. Saving moves an
  old `.mo.toml` there; once `.mo/settings.toml` exists the old file is no longer read. Saving a
  project that sets nothing removes the file, and `.mo/` if it is left empty. Both are protected
  paths for the model's file tools.
- Saving only the global settings never writes the project file, even an untrusted one: it is
  written, and so trusted, only once its screen has been opened.
- The model is written as the top-level `model`, whatever provider it belongs to, and a project's
  per-provider models are folded into it on save.

## Skills, and turning MCP servers off

- A skill is a folder with a SKILL.md, the format other agents use, so `~/.claude/skills` can be
  added to `skills.dirs` and read as is. Only the `name` and `description` keys of the front
  matter are read, with a small reader instead of a YAML library: plain, quoted and block values.
- Skills reach the model through a tool, not the system prompt: the prompt stays the same, and a
  conversation without skills carries nothing about them. `skill` is declared only when at least
  one skill is on; in search mode `skill_list` and `skill_search` come with it. They are declared
  in every tool mode, compact keeps the list in `skill`'s description, and `tool_list` and
  `tool_search` leave them out, as they are not tools to look for.
- `skills.mode` is apart from `tools.mode`: many skills with few tools, or the other way, are
  both common.
- Loading a skill only reads its files, so it never asks. Project skills come with the
  repository like any file the model reads; they cannot run anything by themselves.
- `tools.mcp_disabled` turns a server off without deleting its settings, so a project can drop
  the servers it does not need. Off servers are not started at all, and `/mcp` lists them.
  Turning off is harmless, so any project file may add to `mcp_disabled` and `skills.disabled`;
  only a trusted one may turn on what the user turned off. `skills.dirs` is user-only.

## Effort

- `effort` is one setting for every provider, sent only when set: Anthropic's
  `output_config.effort`, `reasoning_effort` to OpenAI-compatible servers. mo does not keep a
  list of which model takes which level, as it would age with every release; a model that does
  not take the one set answers with an error, which mo shows. It applies to the turns only:
  compaction and the auto-mode judge keep the model's default.
- A project may set it like the model, so a large repository can think harder than a script.

## Prompt progress

- llama-server streams how far it is in reading the prompt when asked with `return_progress`; mo
  asks only a server whose `/props` answers with a context size, as only llama-server has it and
  OpenAI rejects fields it does not know. The state says `prompt` and the share read, counting
  only what was not cached, since the cached part costs no time: `prompt 42% · 3.1k/7.4k · 1.0k
  cached`. Other servers say nothing while they read, so the state stays `waiting`.

## Instructions files

- `AGENTS.md`, the name most agents share, and `AGENT.md` are read by default; the list is a
  setting, so `CLAUDE.md` is one line away. The user's file comes first, then the repository's from
  its root down to the working folder: the most specific instructions are read last. Outside a
  repository only the working folder is read, so mo run from a subfolder of home does not pick up
  files above it. A file is read whole, however long: the user wrote it to be followed; a link to a file
  already read is skipped.

## Modes per tool, per skill and per MCP server

- Every tool, skill and MCP server can have its own mode over the default: full, compact or
  search; a server also group. The search tools are declared as soon as a tool is not in full
  mode: one left to search must be found, and one in compact mode is declared without what it does,
  so tool_search gives that too, and with LoadDeclare a compact tool found is declared whole from
  then on. tool_list names only the tools not declared, so none is listed twice. Skills likewise:
  skill_search comes with any skill in compact or search mode.
- group shows an MCP server as one entry, named with its tool count in tool_search's
  description; tool_search with the server's name gives all its tools, more words filter them. A
  server with many tools costs a line until the model needs it.
- A project overrides each mode on its own, under keys like `tools.modes.git`, so the rest follows
  the global settings.

## Status line and side panel

- The status line is drawn as powerline segments, each on a mid-dark background with an arrow of
  that colour into the next: the mode, shaded from its own colour, provider › model, the context,
  the speeds; the branch and the folder end it from the right. Powerline arrows need a Nerd Font,
  so `render.status = "plain"` joins the parts with │ instead.
- The branch shows always, master too, after a segment of its own counting the changes by kind
  in their colours: added, modified, deleted, renamed, untracked, in conflict, or a check when
  clean. They are Nerd Font's Material file icons (a file with a plus, a pencil, a minus…), or with `render.icons = "text"` the characters
  `+ ~ - » ? ! ✓`, which also leaves the branch icon out. They are read with the branch off the UI goroutine at start, after each tool and turn
  and on focus, one read at a time, since a status can take long in a large repository. A click
  on the branch lists the files. The counts go before the branch and the folder when room is short.
- When room is short the ⇧tab hint goes first, then the provider, first shortened to its letter,
  then the speeds, the tokens, and the bar last, which shrinks to one cell in eighths (▁ to █).
  The branch and the folder go only when nothing else is left, and once they go nothing that went
  before them comes back.
- With the side panel the context and the speeds sit at its bottom whatever is scrolled, without a
  title: a whole bar, the tokens in words and the speeds.
- The panel scrolls with the wheel when its tasks and jobs do not fit, saying how many lines are
  above and below.

## Right clicks and /model

- A right click opens what can be done with what is under it: on the mode, every mode; on a task,
  its states and delete; on a job, its output and how long it ran, refresh and kill. Menus are the
  settings lists, so the keys and the left click work in them too.
- A task changed by the user is told to the model at its next step with the whole list, as ended
  jobs are, without starting a turn.
- `/model`, a click on the model, the Model setting and the provider form list the provider's
  models: an OpenAI-compatible server's `/models`, Anthropic's Models API. The list filters with /,
  and a name it lacks can be typed. A model picked with /model is saved where the model is set: the
  project's file when it sets one, else the user's.

## Gemini's thought signatures

- Gemini 3 models, through the OpenAI-compatible endpoint, send a thought signature with each tool
  call, in `extra_content`, and refuse the next request if it is not sent back with the call. Every
  tool call keeps what its provider sent with it as opaque `Extra`, saved with the conversation,
  and sends it back as it came.

## Errors on narrow terminals

- A line of an error longer than the terminal goes on under itself, indented as far as its text
  starts, or under the value of a JSON `"key":`, so a provider's JSON error stays readable whole.
- An error of more than two lines, or a very long one, is folded to its first line with how many
  lines it has, like a reasoning: ctrl+o or a click opens it whole. A turn that ends with one keeps
  the order: the answer cut short, the error, then mo's notes.

## OAuth sign-in

- A provider can sign in with OAuth 2 instead of a key: the device flow for any terminal, the
  browser with PKCE and a loopback redirect, or client credentials. golang.org/x/oauth2 does the
  protocol; mo keeps the token by provider in `~/.local/share/mo/oauth/`, 0600, and asks for it at
  each request, so a token renewed with its refresh token, or one got with /login while mo runs,
  is used without a restart. A session starts without a token; its first request says to /login.
- A provider with a sign-in counts as one with a key: a project file cannot pick it unless trusted.
- Signing in with another product's OAuth client, as with Codex or Antigravity subscriptions, is
  left for later: it works only by presenting itself as that product.

## The model's context window

- The window is the model's own unless set: mo asks the server each time a session is built, so at
  start and after picking another model. llama-server says it in /props; other OpenAI-compatible
  servers in the model's entry of /models, under the name each uses (context_length,
  context_window, max_model_len, max_context_length); Ollama, the context it loaded the model with,
  else its num_ctx, else the most the model takes, as it cuts at num_ctx; LM Studio, the loaded
  context, else the most; Gemini, its own model API's inputTokenLimit, the key as key= since that
  API takes no bearer key; Anthropic, the Models API's max_input_tokens. When none says, 32768, or
  200000 for Anthropic. Context › Context window overrides it, and the Context section says where
  the window in use came from.

## Vim keys

- Off by default, `render.keys = "vim"` or `/vim` for the session. It starts writing, as a chat
  does; esc goes to normal mode, where letters move and act instead of typing.
- Normal mode acts on a pane: the input, the conversation or the side panel, moved between with
  ctrl+h/j/k/l as split windows are in vim. Writing is only in the input, so i, a and : go back
  there from anywhere.
- The conversation gets a line cursor of its own: j and k move it and scroll to keep it in view,
  l and h open and fold the block it is on, v selects lines and y copies them as they read.
- In the side panel j and k go from task to task and job to job, a job's two rows being one item,
  and l opens what a right click would. In settings lists l is enter and h is esc, so menus go in
  and out as directories do in a file manager. Text fields keep h and l for typing.
- esc in normal mode does what it did: stop the run, or twice to go back to a prompt. ctrl+c,
  ctrl+b, ctrl+o and the others keep their meaning in every mode.
- Copying and pasting go through the system clipboard, as vim's `+` register: y, yy, dd, x, D and a
  visual d put what they take there, whole lines with a line break after; p and P read it, whole
  lines going below or above. Without a clipboard program p pastes what mo kept itself.
- u undoes one normal-mode edit or a whole insert, as vim does, from snapshots of the input;
  ctrl+r redoes. Counts (3j) are left out.

## Settings menu

- The menu is six groups, not a flat list of sixteen sections: Model, Conversation, Tools, Skills,
  Safety and Interface. A group lists its settings directly, without a level of sections in
  between; titles that would read ambiguously there say what they belong to ("Notify by", "Loop:
  within the last replies"). Only the long lists, as long as the tools or skills there are, open
  on their own: the mode of each tool and skill, the descriptions, the providers, the MCP servers.
- In the tool modes an MCP server is one row opening its own list: its mode, and its tools only
  when the server is not a group, since in a group they are found together and have no mode.
- A group's row shows the values of its first section, the ones most looked for. Esc goes back one
  level at a time: setting, list, group, menu.
- A short field, a choice, yes or no, a line, opens in a box over the middle of the list it came
  from, which stays in view; a click outside the box cancels it. Long text keeps the whole screen.
- The project screen keeps its flat list, short as it is, with each section named with its group.

## Context menus

- A right click opens a small menu over the screen by the pointer, below and to the right of it,
  moved to fit; opened from the keys (l in vim's panel) it is centred. The screen stays as it was
  under it, instead of a settings-like screen taking its place for a choice of three.
- Keys and clicks: ↑↓ or j k move, enter, l or a click take an option, the first letter jumps to
  one, esc, h or a click outside close it; a right click outside opens the menu there instead.
- It is drawn as a layer over the screen with lipgloss's compositor, so nothing under it is
  redrawn or moved.

## Interfaces the model draws

- One tool, `ui`, with create, update and remove: the model never chooses how an interface is
  shown. Its call shows in the conversation as any tool's; the interface itself is listed under
  UIs in the side panel and opens as a dialog over the screen, where it is used, so scrolling
  never loses it and the input is never fought over for the keys. A click outside hides it.
- A tree of five nodes, panel, card, label, button and input, all taking the same style fields,
  laid out by a small flex of our own over lipgloss: no flexbox library fitted without its own
  model. Sizes are in cells, so bars of a chart are panels of a width and a colour.
- Events name what to do: notify, submit, close (the dialog only) or remove (the interface, told
  to the model), or a function of the interface's script; the
  model hears them as notes, as it hears of ended jobs, and an idle model gets a turn of its own.
  Nothing blocks a tool call while the user thinks.
- Scripts are Lua in the plugins' sandbox (`internal/luabox`, shared): base, table, string and
  math only, `string.rep` capped, half a second an event, two to load. They change elements through
  `ui.set`, which checks every field as the tree is checked, and tell the model only when they
  call notify, submit or close: validation as the user types costs no turn. Every handler a tree
  names must be a function of its script, checked when it is created.
- What the user typed, what scripts changed and their `ui.state` are kept with the interface, in
  `sessions/<id>/uis.json`, written after each event and change: resuming a conversation runs
  its scripts again with their state, so interfaces come back as they were left.

## /view

- `/view` is for the user alone: nothing it shows reaches the model. Pictures, SVG and Mermaid are
  drawn in the conversation as before; any other text file opens in the viewer jobs use, with
  chroma's lexer for its name, line numbers, search and copy. Files over 8 MB, folders and
  binary files are refused with a note.
- Tab after `/view ` completes paths from the working directory (or `~/`), folders first, hidden
  ones only once a dot is typed; a folder fills in without running, so tab goes down the tree.
- Comments on a file live in mo, by path, until the message they are attached to is sent: the
  file is never written. The model gets them after the message, each with its lines and the first
  of them quoted, so it can find the place even if the file moved on.
- `@` searches an index of the working directory's files, read again after 15 s, hidden folders
  and those of dependencies and builds left out: names that start with what was typed come
  first, then names and paths that hold it, then paths holding its letters in order.
