# mo

A minimal coding agent for the terminal. One Go binary, small context, any provider.

```sh
go build -o mo ./cmd/mo
./mo                          # interactive, picks the first local server it finds
./mo -p ollama -m qwen3       # choose provider and model
./mo -y "run the tests"       # answer once and exit, tools run without asking
git diff | ./mo "review this" # prompt from stdin
./mo -detect                  # list the providers mo can use right now
```

Every setting in the config file can be changed from inside mo, saved to `~/.config/mo/config.toml`
and applied at once, keeping the conversation:

- `/settings`: provider, model, output tokens, tools, context (with what every request costs in
  tokens), prompts, tool descriptions, loop guard, web search, memory and images.
- `/providers`: add a provider, or edit or delete one: type, URL, model and where its key comes from.
- `/mcp`: shows which servers answered, then adds, edits or deletes one; they reconnect on save.

Paths in a prompt, pasted or dropped from a file manager, are attached: images go to the model with
the message (when it has vision; otherwise mo sends it without them), other files are listed for it
to read. An image the model opens with `read` reaches it too. The status line shows the context in
use and the speed of the last reply: `pp` (prompt read) and `tg` (answer written) in tokens per
second, from llama-server's own timings when it sends them.

When mo starts with no provider, or with one that does not answer, it opens a setup screen instead
of quitting: pick a provider mo knows or found, set up a new one, go on anyway, or quit. The choice
is tried first and saved only when it works.

## Providers

Two protocols cover almost everything:

| Preset | Protocol | Default URL | Key |
|---|---|---|---|
| `llama-server` | openai | `http://localhost:8080/v1` | — |
| `ollama` | openai | `http://localhost:11434/v1` | — |
| `lmstudio` | openai | `http://localhost:1234/v1` | — |
| `openai` | openai | `https://api.openai.com/v1` | `OPENAI_API_KEY` |
| `openrouter` | openai | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| `anthropic` | anthropic | `https://api.anthropic.com` | `ANTHROPIC_API_KEY` |
| `gemini` | openai | `https://generativelanguage.googleapis.com/v1beta/openai` | `GEMINI_API_KEY` |
| `groq` | openai | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` |
| `deepseek` | openai | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` |
| `mistral` | openai | `https://api.mistral.ai/v1` | `MISTRAL_API_KEY` |

With no provider set, mo tries llama-server, ollama and lmstudio in that order.
With no model set on an openai-type provider, it uses the first model the server lists.

### Reusing other tools' credentials

`mo -detect` and `/settings` also offer what codex and opencode already have:

| Found | Becomes |
|---|---|
| `OPENAI_API_KEY` in `~/.codex/auth.json` | `openai-codex`, key read from codex when needed |
| `[model_providers.*]` with `wire_api = "chat"` in `~/.codex/config.toml` | `codex-<name>` |
| API logins in opencode's `auth.json` | `<provider>-opencode`, key read from opencode when needed |
| providers with a `baseURL` in `opencode.json` | `opencode-<name>`; `{env:NAME}` keys only |

mo stores a reference (`api_key_from = "codex"` or `"opencode:<provider>"`), never a copy of the key.
Subscription logins (Claude, ChatGPT, Google OAuth) are listed as not usable and never read:
Anthropic forbids using Claude subscription tokens outside its own apps. Claude works with an API key.

## Config

`~/.config/mo/config.toml`, then `.mo.toml` in the project. Each layer overrides only what it sets.
The project file comes with the repository, so it can only pick a provider that needs no key, set
models, `system_prompt`, `max_tokens`, `disabled` and `max_output`, add tools to `confirm`, add
`deny` rules and pick a stricter permission mode. Providers with URLs or keys go in the user file.


```toml
provider = "llama-server"
model = ""                 # empty: first model the server lists
max_tokens = 0             # 0: provider default
system_prompt = ""         # empty: built-in prompt (~80 tokens)

[tools]
mode = "full"              # full | compact (no descriptions) | search (find tools on demand)
load = "context"           # search mode: context (found tools stay in the conversation, called
                           # through tool_call) | declare (found tools are declared from then on)
always_loaded = ["bash", "read", "edit"]   # search mode: tools sent from the start
confirm = ["bash", "rm", "fetch"]   # ask before these; [] never asks. A list of your own
                                    # replaces this one: keep "fetch" in it to be asked
disabled = []              # e.g. ["mkdir", "ls"] to save context
max_output = 10000         # bytes of tool output sent to the model

[tools.descriptions]       # replace what the model reads about a tool; "" sends none
grep = "Regex search in files."

[context]
window = 0                 # 0: ask llama-server, else 32768 (local) / 200000 (anthropic)
compact_at = 0.75          # make room at this share of the window; 0 = never
strategy = "compact"       # compact: summarize older messages · drop: remove the oldest turns
compact_prompt = ""        # empty: built-in summary prompt

[loop]
enabled = true             # watch for a model going round in circles
repeats = 3                # the same tool call with the same result this many times is not run
replies = 10               # ... counted within the last replies of the model in this turn
stream_repeats = 5         # the same text this many times in a row cuts the reply
warnings = 2               # loops the model is told about per turn; the next one stops the turn

[memory]
dir = "~/.local/share/mo/memory"

[render]
background = "#00081a"     # painted behind everything so mo looks the same in any terminal;
                           # "terminal" keeps the terminal's own

[permissions]
mode = "edits"             # ask | edits | auto | all; shift+tab changes it for the session
allow = ["ls *", "git status *", "re:^npm (test|run lint)$", "fetch https://go.dev/*"]
deny = ["git push *"]      # asks in every mode, also in all

[notify]
finish = true              # when a turn ends, with the start of the answer (or its question)
approval = true            # when a call asks for permission
method = "auto"            # auto | terminal | desktop | bell | off
focused = false            # true: also while the terminal has the focus

[permissions.projects."/home/me/work/app"]   # in this folder only, added to the above
mode = "auto"
allow = ["make *"]

[providers.llama-server]
base_url = "http://10.106.122.191:8081/v1"

[providers.my-vllm]
type = "openai"
base_url = "http://gpu-box:8000/v1"
api_key_env = "VLLM_KEY"   # or api_key = "...", or api_key_from = "codex" / "opencode:<provider>"
model = "qwen3-coder"
```

`/settings` rewrites this file with only the values that differ from the defaults; comments are lost.

## Tools

`bash` `read` `write` `edit` `ls` `mkdir` `rm` `grep` `memory` `fetch` `web_search` `todo`, plus `tool_search` in
search mode and the tools of your MCP servers.
Output is cut to `max_output`, keeping the head and the tail.

`fetch` reads a URL as markdown (the article of an HTML page, indented JSON, plain text), 8 KB at a
time. `web_search` uses DuckDuckGo by default, with no key; see `[web]` below for the others.

`todo` is the model's task list for work of several steps. It shows above the input while a task
is left, the one in progress highlighted; it comes back with the conversation on `/resume`.

`memory` keeps notes across sessions, per project or global. Their first lines go into the system
prompt so the model knows what it saved.

Changes outside the working directory, to mo's config, under `.git` or to hidden files in your home
always ask, even with `-y` or after answering "always", in every mode but `all`. So do changes to
global memories.

## Conversations

Every conversation is saved after each turn in `~/.local/share/mo/sessions/`, readable by you only.

- `/resume` lists the conversations of this folder, the latest first; `mo -c` continues the latest.
- `/rewind`, or esc twice with an empty input, lists your prompts: the one you pick comes back into
  the input to edit and send, and the conversation goes on from before it, in a fork. The
  conversation as it was stays in `/resume`.
- `/fork` goes on in a copy; `/clear` starts a new conversation. Neither loses the old one.

Going back does not undo what tools changed on disk since that prompt.

Tool calls show their first lines; ctrl+o, or a click on one, shows the whole call and output.

## Notifications

mo calls you back when a turn ends and when a call asks for permission, only while its terminal is
not focused. `auto` uses the terminal's own notifications where it has them (Ghostty, WezTerm,
foot, kitty, iTerm2; they work over ssh and through tmux), else `notify-send` or macOS
notifications, else the bell. `/notify` sends a test. A terminal that does not report focus
counts as focused once you type: set `focused = true` there.

## Permissions

The status line shows the mode; shift+tab changes it for the session.

| Mode | What runs without asking |
|---|---|
| `ask` | reading; everything that changes files or runs asks |
| `edits` | reading and file changes in the working directory (the default) |
| `auto` | as `edits`, and the commands and fetches the model judges safe for your request |
| `all` | everything, except what a `deny` rule names |

In every mode a `bash` command runs when rules allow each of its parts: the commands joined by
pipes, `&&` or `;`, inside loops, and inside `$(...)`. A part that writes to a file with `>` asks
even when a rule allows it. A fetch is checked as `fetch <url>`.

A rule is a command where `*` stands for anything: alone it is any words or none (`ls *` also
takes `ls`, `ls * /home` takes `ls -la /home`), inside a word any characters of it (`git log*`).
`re:` starts a regular expression. `deny` wins over `allow`. A wildcard on a command that runs
others (`find *`, `xargs *`, `env *`, `sudo *`) lets those through too.

When a command asks, `a` opens the parts no rule allows as rules: put the `*` where you want, then
save them for every project or for this folder only. `/settings` › Permissions edits the lists, one
rule per line, and has a field to try a command against them.

In `auto` the model is asked in a separate request that holds only your request and the commands,
not what tools returned, so a page or a file cannot argue for itself. An unclear answer asks you.

## Web search

```toml
[web]
search = "duckduckgo"      # duckduckgo | searxng | brave | tavily
search_url = ""            # searxng: https://your-searxng
search_key_env = ""        # brave / tavily: the variable holding the key (or search_key = "...")
```

## MCP servers

```toml
[mcp.playwright]
command = "npx"
args = ["@playwright/mcp@latest"]

[mcp.docs]
url = "https://example.com/mcp"
headers = { Authorization = "Bearer ${DOCS_TOKEN}" }   # ${NAME} reads the environment
trust = true               # run its tools without asking
```

Their tools are named `mcp__<server>__<tool>` and ask before running unless the server has
`trust = true`. In `search` tool mode they stay out of the context until the model looks for them.
`/mcp` shows which servers answered and edits them. A server's stderr goes to `~/.local/share/mo/logs/mcp-<name>.log`.
`[web]` and `[mcp]` are read from the user file only, never from a project's `.mo.toml`.

## Images and diagrams

Answers with a ```` ```mermaid ```` or ```` ```svg ```` block, or a line that is only `![](local/file.png)`,
are drawn in the terminal; so are images that `read` opens or an MCP tool returns, and `/view <file>`
shows an image, SVG or Mermaid file.

```toml
[render]
images = "auto"   # auto | kitty | blocks | off
```

`auto` uses kitty graphics in kitty and ghostty (also inside tmux with `set -g allow-passthrough on`),
coloured half blocks in other truecolor terminals, and text otherwise. SVG is drawn by resvg built
into mo; Mermaid flowcharts and sequence diagrams are drawn with box characters, or as an image when
`mmdc` (mermaid-cli) is installed.

## Plugins

Lua files in `~/.config/mo/plugins/` load at start; `/plugins` lists them. A plugin can add tools
for the model, commands for you and hooks:

```lua
mo.tool{
  name = "git_log", description = "Recent commits.",
  params = { count = { type = "integer" } },
  confirm = false,                       -- true: ask before it runs
  run = function(args) return (mo.exec("git log --oneline -n " .. (args.count or 10))) end,
}
mo.command{ name = "standup", run = function(text) return "**markdown** to print" end }
mo.on("prompt", function(text) return text .. " (be brief)" end)   -- rewrite every prompt
mo.on("tool_call", function(call)                                    -- return a reason to block
  if call.name == "bash" and call.args.command:find("push %-%-force") then return "no force pushes" end
end)
```

They get `mo.exec(cmd)` (output, exit code), `mo.read(path)`, `mo.write(path, text)`,
`mo.json_encode`, `mo.json_decode`, `mo.env(name)`, `mo.workdir` and `mo.log` (`print` goes to the
log too, in `~/.local/share/mo/logs/plugin-<name>.log`). The Lua `io`, `os` and `require` are not
there. A full example is in `docs/examples/plugins/git.lua`.

The language is Lua 5.1 as gopher-lua implements it: patterns have no `%f` frontier (it finds
nothing, without an error), and there is no `goto` or integer division.

Plugins are your code and run with your rights; they are never loaded from a project.

## Keys

| Key | Action |
|---|---|
| enter | send |
| ctrl+j, alt+enter | new line |
| esc, ctrl+c | stop the current run |
| ctrl+c (idle) | clear input, then quit |
| ctrl+d | quit |
| shift+tab | change the permission mode |
| ctrl+o, click | open or fold reasoning and tool calls |
| esc esc (empty input) | go back to one of your prompts |
| ctrl+v | paste a copied image or files as attachments |
| pgup, pgdown, shift+up/down, mouse wheel | scroll the conversation |
| shift+drag | select text (the mouse is mo's while it runs) |
| `/clear` `/resume` `/rewind` `/fork` `/compact` `/settings` `/providers` `/mcp` `/plugins` `/view <file>` `/help` `/quit` | commands; type `/` for suggestions, tab or enter to take one |
| `/<name> text` | a plugin command |
