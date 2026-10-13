# Decisions

## Context budget

- The built-in system prompt is four lines plus working directory, OS and date (~90 tokens). The nine
  tool definitions are ~2.2 KB (~550 tokens) in full mode. Tool descriptions stay one line; parameters carry no
  description unless the name is not enough.
- Requests that fail at the network level are sent once more; llama-server closes keep-alive
  connections after streaming, and a repeated model request has no side effect.
- Tool output is cut to `tools.max_output` (10 KB) keeping 60 % head and 40 % tail: commands put
  their summary and errors at the end, files and listings put the useful part first.
- `read` has no line numbers: `edit` matches text, not lines, so numbers would only cost tokens.

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
- `always_loaded` is `read`, `edit`, `write`, `bash`, `ask` and `recall` by default: the core of
  editing code; `write` because `edit` cannot create a file; `bash` covers `ls`, `grep`, `mkdir`
  and `rm`, and the default allow rules let its reading commands through; `ask` and `recall`
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
  given request and every request would carry them. The model finds them with `recall`, or
  `memory list`, when it needs them.
- Writing or deleting a global memory always asks: a prompt-injected model could otherwise plant a
  note that reaches every future session in every project. Project memories do not ask.
- The memory folder is a protected path for `write`/`edit`, and the project file cannot move it.

## Providers

- Two protocols only: OpenAI chat completions (every local server speaks it) and Anthropic Messages.
  Gemini, Bedrock and Vertex can come later as more adapters behind `llm.Provider`.
- Anthropic: API key only. Reusing Claude Code's subscription login breaks Anthropic's terms and is
  blocked by them since 2026.
- Anthropic requests mark the system prompt, the last tool and the last message as cache
  breakpoints, so every turn rereads the shared prefix from the prompt cache.
- Tool arguments cut off by the output token limit become `{}` plus `ToolCall.ArgsError`. Sending
  the broken JSON back would make every later request fail; the error tells the model to split the
  work.
- A stream fragment without `index` continues the last tool call unless it carries an id or a name.

## Safety

- The project file `.mo.toml` comes with the repository, so it cannot change provider URLs or keys,
  define providers, pick a provider that needs a key (that would send the code to a paid cloud),
  move the memory folder, remove tools from `confirm` or `disabled`. It can pick a keyless
  provider, set models, tool mode and context settings, and add confirmations or disabled tools.
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
  repository: a cloned `.mo.toml` with `allow = ["*"]` would run anything. The project file may add
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
  opens an edit form for one entry. Only `.mo.toml` stays a file, as it belongs to the project.
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
  with `-p` or by a project's `.mo.toml` is never copied into the global config. Provider and model
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
  absolute links, and serves it 8 KB at a time with an offset, so a long page costs what the model
  actually reads. The last 16 pages are cached per session.
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
  ends, and names the conversation it came from. Nothing is saved until then, so going back and
  changing one's mind loses nothing.
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

- `bash` with `background: true` starts a job in its own session and returns its number at once;
  its output is kept like a foreground command's, 1 MB of head and tail. A job outlives the turn
  that started it. Jobs belong to the run of mo, not to a session: they survive a settings change,
  and mo kills what is left (SIGTERM, then SIGKILL after 3 s) when it exits. mo run once has none.
- When a job ends the model is told with a note holding its last 15 lines: before its next step if
  it is working, appended to the last user-side message so two user messages never follow; and if
  it is idle, in a turn of its own started from the note, so it can react without waiting for the
  user. Also after esc: the user started the job to hear when it ends, and a stop of the model is
  not a stop of the job. A note says why the model goes on; esc again stops it, and nothing is left
  to start it once more. A job killed on purpose is not told as an end.
- The side panel follows what is being done: the conversation, the tasks whole and wrapped, and
  the jobs with their state and time. It shows from 110 columns, 36 wide; the conversation is drawn
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

- `recall` searches memories and saved conversations with BM25 and returns ids with a piece of text
  around the match; `read` gives a memory whole, a conversation's turns, or one turn whole. The
  model decides what to look for and what to read, instead of carrying an index it rarely needs.
- A conversation is split at the user's prompts: a turn is the prompt, the answers and the tool
  calls with the start of their results (300 characters when searching, 1500 when read). mo's own
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
  both keeps ours; both are reported as conflicts. Renames are not followed yet.
- Hooks do not run, and the result of a commit says so.
- Not yet: rebase, push, pull and fetch.

## Tool calls that change files

- An opened `write` shows what it wrote and an opened `edit` the diff from old to new, both
  coloured by the file's language; folded they say how many lines were written, added and
  removed. Each diff line is coloured on its own, so a string spanning lines may colour wrong.
