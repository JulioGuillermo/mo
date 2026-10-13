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
  (names and schemas usually say enough to a capable model), `search` sends only `tool_search`,
  the `always_loaded` tools and those loaded so far. `tool_search` names the tools not loaded yet in
  its own description: names are cheap and let the model `select:` them without guessing.
- In search mode a newly loaded tool changes the tool list, which ends the Anthropic prompt cache
  prefix for that request. Accepted: loading happens a few times per session.
- A call to a tool that exists but is not loaded still runs, with the usual approvals.

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

## Memory

- One `memory` tool (list/read/write/delete) instead of four, to keep the tool list small.
- Files live in `memory.dir` (`~/.local/share/mo/memory`): `global/` and `projects/<dir>-<hash>/`.
  The hash of the full path keeps `/x/a-b` and `/x/a/b` apart.
- The first line of each memory goes into the system prompt, up to 50, labelled as notes rather
  than instructions; `memory list` shows them all.
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
- Only `y` approves. Enter does not, so typing into the prompt cannot approve a command by accident.

## TUI

- Inline, not alternate screen: finished output goes to the terminal's own scrollback, where
  selection, search and copy work as usual. Only input, streaming text and status are redrawn.
- The agent goroutine prints with `Program.Println`, which queues in the program's message channel.
  Printing through `tea.Println` commands from `Update` would run each command in its own
  goroutine and could reorder lines.
- Quitting while a run is active cancels it and waits up to 3 s so running commands get killed.
  Prints from the agent stop once the program has exited, because `Program.Println` blocks forever
  when nobody reads the channel.
