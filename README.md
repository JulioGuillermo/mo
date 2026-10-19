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

Two protocols cover almost everything (three more serve the subscription logins: Google's Cloud Code for Antigravity, the Responses API for Codex, and Anthropic's own with a Claude account):

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
| `antigravity` | cloudcode | `https://daily-cloudcode-pa.sandbox.googleapis.com` | `/login antigravity` |
| `copilot` | openai | `https://api.githubcopilot.com` | `/login copilot` (GitHub device code) |
| `github-models` | openai | `https://models.github.ai/inference` | `GITHUB_TOKEN` |
| `opencode` | openai | `https://opencode.ai/zen/v1` | `OPENCODE_API_KEY` or opencode login |
| `opencode-zen` | openai | `https://opencode.ai/zen/v1` | `OPENCODE_API_KEY` |
| `xai` | openai | `https://api.x.ai/v1` | `XAI_API_KEY` |
| `together` | openai | `https://api.together.xyz/v1` | `TOGETHER_API_KEY` |
| `fireworks` | openai | `https://api.fireworks.ai/inference/v1` | `FIREWORKS_API_KEY` |
| `cerebras` | openai | `https://api.cerebras.ai/v1` | `CEREBRAS_API_KEY` |
| `moonshot` | openai | `https://api.moonshot.ai/v1` | `MOONSHOT_API_KEY` |
| `zai` | openai | `https://api.z.ai/api/paas/v4` | `ZAI_API_KEY` |
| `qwen` | openai | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `DASHSCOPE_API_KEY` |
| `nvidia` | openai | `https://integrate.api.nvidia.com/v1` | `NVIDIA_API_KEY` |
| `huggingface` | openai | `https://router.huggingface.co/v1` | `HF_TOKEN` |
| `codex` | responses | `https://chatgpt.com/backend-api/codex` | `/login codex` |
| `claude-code` | claude-code | `https://api.anthropic.com` | `/login claude-code` |

With no provider set, mo tries llama-server, ollama and lmstudio in that order.
With no model set on an openai-type provider, it uses the first model the server lists.

`/effort` sets how hard the model thinks, saved as `/model` saves a model; `/effort` alone opens
a menu, and tab after `/effort ` offers `default` (the model's own) and the levels
the model takes. The status line shows the one set, beside the model. Which levels: llama-server
says through its chat template (gpt-oss's low to high, or `none` for a model whose thinking only
turns off, sent as `enable_thinking: false`); Anthropic, Codex, Gemini and OpenAI's reasoning
models are known; for any other every level is offered, saying the provider does not tell. A
server that refuses the effort gets the request again without it, and none from then on.
To Anthropic the effort goes as Claude Code sends it, in `output_config.effort`: with `default`
the model's own level is sent (medium for Opus, Sonnet and Haiku 5.5, xhigh for Opus 4.7, high
for the rest), a level the model lacks goes as `high`, and a model without effort (Haiku 4.5,
Sonnet 4.5 and older) gets none.
Anthropic's requests also carry Claude Code's `max_tokens` for the model (32000 to 128000) when
`max_tokens` is 0, and its thinking: `adaptive` where the model has it, else a budget of tokens.
Thinking blocks go back to the model with their signature, as the API asks; when it refuses
them (a history changed by compaction), the request goes again without them.
When a provider's safety filter stops an answer (Anthropic's `refusal`, OpenAI's `content_filter`,
Gemini's `SAFETY` and the like), the answer ends with a note saying so, red on a dark red band
after ⊘; a turn that ends with no text and no call, the provider giving no reason, gets such a note
instead of ending in silence. The model reads the note too.
`antigravity` and `claude-code` start the system prompt as Antigravity's and Claude Code's own
requests do, then say the model runs inside mo; `preamble = false` under the provider, or its form
in `/providers`, sends mo's prompt alone (Claude accounts' tokens may be refused without it).

### Reusing other tools' credentials

`mo -detect` and `/settings` also offer what codex and opencode already have:

| Found | Becomes |
|---|---|
| `OPENAI_API_KEY` in `~/.codex/auth.json` | `openai-codex`, key read from codex when needed |
| `[model_providers.*]` with `wire_api = "chat"` in `~/.codex/config.toml` | `codex-<name>` |
| API logins in opencode's `auth.json` | `<provider>-opencode`, key read from opencode when needed |
| providers with a `baseURL` in `opencode.json` | `opencode-<name>`; `{env:NAME}` keys only |
| Antigravity's sign-in (its `state.vscdb`, read with the `sqlite3` command) | `antigravity-detected` |
| Claude Code's sign-in (`~/.claude/.credentials.json`) | `claude-code-detected` |
| Codex's ChatGPT sign-in (`~/.codex/auth.json`) | `codex-detected` |

mo stores a reference (`api_key_from = "codex"`, `"opencode"` or `"opencode:<provider>"`), never a copy of the key.

`/providers` lists everything found under **Detected**; picking one uses it with no second sign-in. A borrowed sign-in is
`token_from = "claude-code"`, `"codex"` or `"antigravity"`: mo reads the other app's token each time and keeps none. By default
it only reads. When the token expires, Claude Code's and Codex's refresh tokens change at each renewal, so mo asks you to open
the app to renew it, unless the provider has `token_renew = true` (**Renew its login** in its form): then mo renews it and writes
the new token back to the app's file. Antigravity's refresh token does not change, so mo renews it in memory and writes nothing.

A provider can sign in with OAuth 2 instead of a key, set in its form in `/providers` or by hand:

```toml
[providers.corp]
type = "openai"
base_url = "https://llm.corp.example/v1"
[providers.corp.oauth]
flow = "device"            # device (a code to enter on a page) | browser (PKCE, local port) | client_credentials
client_id = "mo"
token_url = "https://sso.corp.example/oauth/token"
device_url = "https://sso.corp.example/oauth/device"   # browser takes auth_url instead
scopes = ["models"]
client_secret_env = ""     # when the client has a secret
```

`/login corp` signs in; the token is kept in `~/.local/share/mo/oauth/corp.json`, for you only, and
renewed with its refresh token when it expires. `/logout corp` forgets it.
Subscription logins have their own providers.

### Notes

- `copilot` needs a GitHub account with Copilot; it sends the headers in `[providers.copilot.headers]`.
- Any provider may set `headers = { Name = "value" }`.
- For Azure OpenAI, add a new OpenAI-compatible provider with `https://<resource>.openai.azure.com/openai/v1`.

## Config

`~/.config/mo/config.toml`, then `.mo/settings.toml` in the project (the old `.mo.toml` is still
read when that does not exist). Each layer overrides only what it sets.

`/settings` groups what it sets under six headings, each listing its settings directly: **Model**,
**Conversation** (context window, instructions files, memory), **Tools** (with web search),
**Skills**, **Safety** (permissions, loop guard) and **Interface** (keys, look, notifications).
Only the long lists open on their own, marked ›: the mode of each tool and of each skill, the
tool descriptions, the providers and the MCP servers. In the tool modes an MCP server is one row:
it opens its mode and, unless that is `group`, its tools one by one. Esc goes back a level.

It has two screens, switched with tab: **Global**, for every project, and **This project**
(`/settings project`), which shows only what a project can override (provider and model, tools
and MCP servers on or off, skills, context, loop guard) and marks each value `● this project` or `· global`. What the project does not
set follows Global, also when Global changes later. Providers, MCP servers, web search, memory,
notifications and display are only global. The project's permission rules are edited there too,
but kept in the user file for that folder, so a repository never brings its own `allow` rules.

A project file you save from mo is trusted: mo remembers its hash in
`~/.local/share/mo/trusted-projects.json`. One that came with the repository, or changed since, can
only pick a provider that needs no key, set models, `system_prompt`, `max_tokens`, `disabled` and
`max_output`, add tools to `confirm`, add `deny` rules and pick a stricter permission mode; mo says
so at start when that holds something back, and saving it from `/settings project` trusts it.
Providers with URLs or keys always go in the user file.

mo follows both files while it runs, edited by hand or saved from another mo:

| What | When it applies |
|---|---|
| look, keys, notifications (`[render]`, `[notify]`) | at once |
| tools, MCP servers, skills, instructions, context, permission rules, the rest | at the next turn; a turn under way ends with what it began with |
| provider, model, effort, `max_tokens`, permission mode | at `/reload`, or when mo starts again |

A file that does not parse, or holds a wrong value, opens a red box: accept and go on with the
settings in force, or put back the last file that worked or the defaults, keeping the broken one as
`<file>.broken`. Once any mo fixes it, the box closes in all of them. A project file changed outside
mo that sets more than its harmless part asks first: trust it and load it all, load the harmless
part, or ignore it.


```toml
provider = "llama-server"
model = ""                 # empty: first model the server lists
max_tokens = 0             # 0: provider default (Anthropic: Claude Code's for the model)
effort = ""                # empty: the model's default · low | medium | high | xhigh | max (Anthropic)
                           # · none | minimal | low | medium | high | xhigh (OpenAI's reasoning_effort)
system_prompt = ""         # empty: built-in prompt (~80 tokens)

[tools]
mode = "full"              # full | compact (no descriptions) | search (find tools on demand)
load = "context"           # search mode: context (found tools stay in the conversation, called
                           # through tool_call) | declare (found tools are declared from then on)
always_loaded = ["read", "edit", "write", "bash", "ask", "memory"]   # search mode: declared from the start
confirm = ["bash", "rm", "web_fetch"]   # ask before these; [] never asks. A list of your own
                                        # replaces this one: keep "web_fetch" in it to be asked
disabled = []              # e.g. ["mkdir", "ls"] to save context
mcp_disabled = []          # MCP servers set up but not started, e.g. ["docs"]
max_output = 10000         # bytes of tool output sent to the model
read_max_size = "10MB"     # larger files are read only in parts (offset, limit, match); "0" for no limit
outputs = "disk"           # whole command output kept: disk, memory or none
output_turns = 0           # for how many of your turns; 0 for the whole conversation
calls = "streamed"         # streamed: each call runs as soon as it is written; reply: all once the reply ends

[tools.descriptions]       # replace what the model reads about a tool; "" sends none
grep = "Regex search in files."

[tools.modes]              # the mode of single tools, over tools.mode
git = "search"
grep = "compact"

[tools.mcp_modes]          # the mode of all of an MCP server's tools, or group: one entry
playwright = "group"       # in tool_search under the server's name, which gives all its tools

[skills]
mode = "full"              # full: names and what each is for · compact: names · search: skill_list/skill_search
disabled = []              # skills the model does not get
dirs = []                  # more folders of skills, e.g. ["~/.claude/skills"]
modes = { pdf = "search" } # the mode of single skills, over skills.mode

[instructions]
enabled = true             # read instructions files into the system prompt
files = ["AGENTS.md", "AGENT.md"]   # looked for in ~/.config/mo, then from the repository's root down

[context]
window = 0                 # 0: the model's own, as its server says (llama-server, the model list,
                           # Ollama, LM Studio, Gemini, Anthropic); else 32768 / 200000
compact_at = 0.75          # make room at this share of the window; 0 = never
strategy = "compact"       # compact: summarize older messages · drop: remove the oldest turns
compact_prompt = ""        # empty: built-in summary prompt; /compact <instruction> adds yours under it, that time only.
                           # /compact between turns sums everything up; compacting on its own, mid-turn, keeps the
                           # turn in progress whole when it is small. Sizes are estimated with the bytes a token
                           # took in the provider's last count

[loop]
enabled = true             # watch for a model going round in circles
repeats = 3                # the same tool call with the same result this many times is not run
replies = 10               # ... counted within the last replies of the model in this turn
stream_repeats = 5         # the same text this many times in a row cuts the reply
warnings = 2               # loops the model is told about per turn; the next one stops the turn

[agents]                   # user file only; /settings › Subagents
enabled = false            # let the model start subagents with the agent tool
depth = 1                  # levels: 1, only the main agent starts them; 2, theirs may too; ...
max_parallel = 3           # working at once; one waiting for its own subagents does not count
max_agents = 20            # started in one of your turns
max_steps = 50             # replies of the model before a subagent is stopped
model = ""                 # "provider/model" for them; empty for the main agent's

[peers]                    # user file only; /settings › Peers
enabled = false            # let the main agent see the other mo windows and message them
name = ""                  # what the others call this one; "auto": from the title; empty: folder@branch
incoming = "ask"           # before a peer's message reaches the model: tools | ask | auto | allow
outgoing = "tools"         # before the model sends one: the same choices
wake = true                # a message let in while the model is idle starts a turn
network = "off"            # off: this computer only | visible: found on the local network | hidden: by address only
port = 0                   # where it takes peers from the network; 0 for any free one
addresses = []             # peers asked directly, "host:port": hidden ones, or across a VPN
secret = ""                # the team's; 16 characters or more; /peers secret new makes one
secret_env = ""            # or the variable that holds it
# [peers.projects."/path/to/project"]  # another team's secret for one folder; /settings project › Peers
# secret = "..."

[memory]
dir = "~/.local/share/mo/memory"

[render]
background = "#00081a"     # painted behind everything so mo looks the same in any terminal;
                           # "terminal" keeps the terminal's own
sidebar = "auto"           # side panel of tasks and jobs when wide enough; "off" never
keys = "default"           # "vim" for vim keys: modes, and ctrl+h/j/k/l between the panes
status = "powerline"       # segments on backgrounds with arrows (needs a Nerd Font); "plain" for any font
bar = "shapes"             # context bar: shapes ▰▱, braille, circles, half-circles, squares, half-squares, dots, rings, dashes, shade, blocks, halves, line
reply_refresh = "live"     # how often what the model writes is drawn: live (every piece), time or tokens;
reply_every_ms = 100       # ... with time; drawing less leaves a local model's GPU to it
reply_every_tokens = 10    # ... with tokens, a second apart at most. What asks, starts or ends is drawn at once;
                           # the live pp and tg speeds are taken again at the same pace
spinner_ms = 80            # ms between frames: the status line's spinner; /settings › Interface › Animations
running_ms = 150           # ... the arc of a call, job or subagent at work
writing_ms = 220           # ... the bubble of a call being written
reasoning_ms = 160         # ... the spark of the reasoning as it comes
asking_ms = 500            # ... the beat of what waits for your answer
icons = "nerd"             # Nerd Font icons for the branch and its changes and for the files of the /view tree; "emoji": emoji for the tree (the status line has plain marks); "text": + ~ - » ? ! ✓ and ▸ ▾

[permissions]
mode = "edits"             # ask | edits | auto | all; shift+tab changes it and it is kept
allow = ["ls *", "git status *", "re:^npm (test|run lint)$", "web_fetch https://go.dev/*"]
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

`bash` `read` `write` `edit` `ls` `mkdir` `rm` `grep` `memory` `web_fetch` `web_search` `todo` `ask` `git` `title`, plus `tool_search` in
search mode and the tools of your MCP servers.

Every tool that returns text takes the same view fields, so the model never loses what is beyond a
limit: `offset` lines to skip (from the end when negative), `limit` lines to show (2000; 200 for
`web_fetch`), `col` and `width` the bytes of each line (0 and 2000, for minified files), `match` only
the lines matching a regexp, and `numbered` for line numbers. What is left out is said, with the
`offset` or `col` that shows it. Other output, as an MCP tool's, is cut to `max_output`, keeping the
head and the tail. `bash`, `git` and `memory` take an `action`, and `help` explains each.

`read` looks at the first 8 KB of a file before reading the rest. Binary data (a NUL byte, or over a
tenth control characters or broken UTF-8) is refused saying what it looks like (PDF, zip…) and
pointing to `file`, `xxd` or `strings`; UTF-16 text is read as text, and images are seen as always.
A file over `tools.read_max_size` (10 MB) is refused whole, with its size; asked for a part with
`offset`, `limit` or `match`, it is read a line at a time without holding it, so the last lines of a
log of gigabytes take a second or two.

`grep` gives matches under their file, or with `show` only the files or a count, and `context`
lines around them. It reads each file a line at a time, whatever its size, skips binary ones by
their first 8 KB, stops a file at its first match when only names are asked, and esc stops it. `edit` takes several replacements at once in `edits`, each in the call's `path` or its own, so one call changes several files, all or none.

`web_fetch` reads a URL as markdown (the article of an HTML page, indented JSON, plain text).
`web_search` uses DuckDuckGo by default, with no key; see `[web]` below for the others.

`todo` is the model's task list for work of several steps. On a terminal 110 columns or wider it
shows in a side panel beside the conversation, whole, with the background jobs; on a narrower one,
above the input while a task is left. `/sidebar` hides or shows the panel.

Every `bash` command is a job of the conversation, numbered, with its whole output kept in
`~/.local/share/mo/sessions/<conversation>/jobs/`: the first and the last 25 MB of each, and up to
500 MB a conversation, the oldest going first. A long output reaches the model cut, with the job
number; `bash` with `action: output` and its `id` shows any part of it, and without an `id` lists
the jobs, or with `match` searches all of them, also after mo restarts. The output lasts as long
as the conversation. Reading output never asks; running and `kill` do. `tools.outputs`, also in
`/settings › Tools`, keeps it on `disk` (the default), in `memory` until mo exits, or `none`: only
the start and end the command returned. `tools.output_turns = N` lets go of the output of
commands older than your last N turns; 0 keeps it for the whole conversation.

`bash` with `background: true` runs a command as a job, for servers, watchers and long builds, and
returns at once; with `until: "listening|ready"` or `wait: 5` it first waits for a line that
matches, the seconds, or the command's end, and returns what it printed, so a server's start is
seen in the same call. Every command shows in the side panel while it runs. When one in the foreground
blocks the model, ctrl+b, or a right click on it or on its call, sends it to the background: the
model goes on with what it printed so far. "Watch its output live" in that menu shows a job's
output over the whole screen as a terminal would, following the end while it runs: scroll with
the arrows, the wheel, pgup and pgdn, search with `/` and `n`/`N`, copy lines by dragging over
them or all with `y`, `b` to the background, `x` to kill, esc to close. When a background job ends the
model is told: at its next step if it is working, or in a turn of its own if it is idle, so it can
react, also after you stop it with esc. `/jobs tell` lets it hear one that is waiting, as when a
settings screen was open. `/jobs` lists them, `/kill <n>` stops one, and mo kills what is left when it exits.

Each tool call starts with a dot coloured by what it does: red a command that blocks the model,
yellow one in the background, green reading a command's output, pink a change, blue a read. The
side panel colours running jobs the same way.

`ui` lets the model draw an interface for you: panels laying out their children in a row or a
column (`justify` start, center, end, space-between, space-around; `align`; `gap`; `grow`),
cards with a border and a title, labels, buttons and inputs, each with `bg`, `fg`, `padding`,
`margin`, `width`, `height`, `visible` and, for buttons, `disabled`; grids, laying their
children row by row in `columns`, each as wide as its widest cell or as `widths` fixes it, so
labels line up beside their controls and data reads as a table; svg nodes, a picture of `width` ×
`height` cells, a kitty image where the terminal takes them and half blocks elsewhere, that a
script draws anew with `ui.set(id, {svg = ...})`, as a chart a slider moves, and whose `on_click`
gets `e.x` and `e.y` in the svg's own units; and controls holding a value
you pick: `select` (`◂ M ▸`: ◂ and ▸ step through its `options`, a click on the value or enter lists them all), `slider` (`min`, `max`, `step`),
`checkbox` (`[x]`), `switch` and `radio` (its options in a column, or a row with `direction`),
each with a `label`. ←→ change them, space ticks a checkbox or switch, and a click picks an
option, an arrow of a select or a place on a slider; their `on_change` is told at each change,
and their values go with `submit` as the inputs' do. It is listed under **UIs**
in the side panel, new until you open it; a click opens it as a dialog over the screen, and a
click outside hides it again. In it, tab moves, enter presses, and what you type fills the input
in focus. `on_click`, `on_input` and `on_change` take `notify` (the model hears the event),
`submit` (it hears every input's value), `close` (the dialog closes, the interface stays listed)
or `remove` (the interface goes, and it hears so); a
button without one notifies, and any other name calls that function of the interface's `script`:
Lua run on your side, without the model, in the same sandbox as plugins (base, table, string,
math), each event given half a second. `ui.set(id, {text=, fg=, bg=, visible=, disabled=, ...})`
changes an element, `ui.get` and `ui.value` read, `ui.state` keeps a table between events, and
`ui.notify(data)` and `ui.submit(data)` tell the model, `ui.close()` closes the dialog and
`ui.remove()` takes the interface away, telling it: so a form validates as you
type and submits only once valid. A script's error shows under the interface and is told to the
model. The model hears at its next step, or in a turn of its own when idle. A right click in the
panel opens or removes one. Interfaces are kept in the conversation's folder, with what was typed
and the scripts' state, and come back with it on resume. `{"action":"help"}` explains it all to the model with
an example, and a tree a small model gets slightly wrong (no `type`, other names for fields, sent
as a string) is mended when what it meant is plain.

`ask` lets the model ask you questions, with options or free text. What the calls of one reply
need from you, approvals and questions, is asked at once in a dialog above the input: tabs show
how many there are and which one you are on, tab moves between them, and the agent goes on once
all are answered. Enter presses the selected button, "No" at first; `y` approves. A command
shows whole and coloured, ↑↓ scroll it, and a bash one lists each of its parts with what the
rules say of it.

`git` works without the git command: status, diff, log, show, blame, branches and check read and never
ask; add, commit, branch, switch, merge, cherry_pick, rebase and pull change the repository, and push
always asks, never forces. pull takes a remote and a branch (by default the one the current branch
tracks) and merges, or rebases with `rebase: true`; unset, it follows git's `pull.rebase` and
`branch.<name>.rebase`. fetch, pull and push take an SSH agent or key, or GIT_TOKEN/GITHUB_TOKEN for HTTPS.
stash, stash_apply, stash_pop and stash_drop are kept as git keeps them; tag and reset (soft, mixed,
hard, which asks) are there too. Untracked files are never touched. A merge or cherry-pick
with conflicts leaves markers in the files for the model to resolve, then `continue`; `commit` and
`continue` refuse while a marker is left, and `check` says where. Hooks do not run.
Every action takes `repo`, the repository's folder, relative or absolute; without it, the working
directory's. A repository outside the working directory asks, in every mode but all. Outside a
repository the error lists those in the 2 levels below; `repos` lists them, and every one opened so
far however deep, each with its branches. That list lives while mo runs and is never saved.
`init` makes a repository in `repo` (the folder too) on git's `init.defaultBranch`, else `main`.

`memory` keeps notes across sessions, per project or global. They are not put in the prompt:
its `search` looks through them and the saved conversations (BM25, by words), and `read` gives one
whole, a note, a conversation's turns, or one turn, when the model decides it needs it. It keeps
to this folder unless asked for all.

Changes outside the working directory, to mo's config, under `.git` or to hidden files in your home
always ask, even with `-y` or after answering "always", in every mode but `all`. So do changes to
global memories.

## Subagents

With `agents.enabled`, the model hands tasks to subagents with the `agent` tool. Each is an agent of
its own: the same tools and permissions (no title, interfaces or background jobs), an empty
conversation that starts from its task, and a system prompt that tells it to end with a report. Its
parent reads only that report, so long searches or test runs stay out of its context.

- `run` takes a `task` and a `name`, or several in `tasks`, which work at once; it waits for their
  reports, or with `background: true` gives their ids at once and the model hears each report when
  it ends, as it does of background jobs.
- `log id` shows everything one did, with the view fields; `send id message` tells one more while it
  works, or lets it go on from where it was once it ended or was stopped; `wait`, `status`, `stop`.
  An agent sees its own subagents and theirs, never another branch's.
- A subagent tells its parent something before its report with `tell_parent`, meant only for what
  matters to it or what you ask it to pass on. A parent that works reads it at its next step; the
  main agent, idle, starts a turn to read it, and a subagent that ended, failed or was stopped goes
  on to read it, and tells its own parent in turn only if it matters to it.
  `{"action":"help"}` explains the agent tool's actions.
- A subagent at `depth` has no `agent` tool. Waiting for its own subagents, one gives up its place
  among the `max_parallel` working, so a tree never waits on itself.
- What a subagent asks, an approval or a question, comes to you in the usual dialog, saying which one
  asks. Esc stops the main agent and every subagent; with the main agent idle, it stops the subagents
  still working.
- Subagents are saved with the conversation, all they did, and come back with `/resume`: those that
  were working, stopped, ready to go on. `/continue` goes on as if nothing had stopped, also after
  mo exited: an `agent` call cut short is kept, unanswered, and run again as resumed, so the
  subagents it started go on from where they were (and theirs, in turn) instead of starting anew;
  those started in the background go on too, unless you stopped them yourself. A new message
  instead of `/continue` answers the call as stopped.
- The side panel lists them under **Agents** as a tree, each with a sign of how it does: `◌` grey
  waiting for a place, `◌` purple waiting for its own subagents (yellow, with `· N asks`, when one
  under it waits for you), `◜◝◞◟` blue turning while it works, `⊙ ◎` yellow beating while it asks
  you, `●` green done, `✗` red when it failed, `⊘` red when stopped, and on the right the context it used of its window, as `4.5k/1M`. A click opens its view;
  a right click stops it, removes it from the list or opens it.
  `/agents` lists them to pick one, `/agents N` opens one.
- Its view is the main screen drawn from the subagent's: its conversation live, the calls it runs as
  they go, its spinner, an input of its own, its status line with its model and context bar, and the
  side panel with its tasks, the subagents under it and its context; over them a title bar with its
  sign, where it is (`main › lead › scout`, each name opens that one), its state and `ctrl+x ✕`. Enter sends it a message: queued while it works, else it goes
  on, its parent not told. Esc stops it and those under it, not the main agent; ctrl+x or `✕` closes.
- Before it reports, a subagent reads what it was sent during its last step and waits for those it
  left working in the background, to hear them. Answering once (`mo "…"`), subagents work too: what
  they do shows on stderr under their name, and what needs confirmation is denied unless `-y`.

## Peers

With `[peers] enabled = true`, each mo window you open is a peer of the others: the main agent gets
the `peers` tool, to `list` them (name, conversation title, folder, working or idle) and `send` one a
message. A send returns once the other mo received it, without waiting for an answer; the other agent
answers, if it wants, with the same tool. The model hears clearly why one failed: `not sent:
api@dev is no longer running`, `… is not reachable: it stopped, or rejects the secret` for a peer
of the network, `no peer named "x"; peers: …`, `no answer from … in 5s`.

On one computer they meet through a Unix socket each in `$XDG_RUNTIME_DIR/mo/peers/`, a folder only
you can open. Each window has a random id and a name, `folder@branch` (`mo@master`, `-2` when another
of this computer has it, the later of the two taking it, also when they meet over the network), set with `name` or `/peers name`, or `auto` for a few words of its title. A project file
never turns peers on, opens the network or loosens what asks.

With `network` on, peers on other computers of the team are reached too, named with their computer:
`api@dev (jg-laptop)`.

- Every connection is TLS 1.3, and both ends prove the team's `secret` by signing the keys of that
  very session with it: the secret never travels, and a proof seen or passed on is worth nothing on
  another connection. A peer with another secret is refused both ways and the sender is told
  `authentication failed, the secrets differ`.
- `visible` tells the local network where it is, by multicast, every 30 seconds, and answers who
  asks; a `list` asks and hears the answers at once, and `/peers scan` asks and waits longer. The packets carry a tag made with the secret, so only
  the team's are heard, and the TLS certificate's fingerprint, which the connection then has to show.
- `hidden` tells no one; the others reach it at its `port` through their `addresses`. It still finds
  the visible ones.
- The secret goes in your own settings, never in the project file: globally, or for one folder in
  `/settings project › Peers`, which overrides the global there; or for the session alone, kept
  nowhere, over both. Use a long random one: the network sees what it signs.
- `/peers secret new|set|clear global|project|session` makes, sets or takes one away there;
  `new` shows the one it made, once, to set on the others; `set` never shows it, and the command is
  not kept in the input's history. `/peers secret` says where the one in force comes from.
- Outside the local network, across a VPN, use `addresses`.

What asks before a message passes, each way:

| Setting | |
|---|---|
| `tools` | as the permission mode says: ask and edits ask, auto judges, all lets through |
| `ask` | asks you every time; default for incoming |
| `auto` | the model judges it, and asks you when unsure |
| `allow` | never asks |

A message in shows in a block of its own, a violet bar with `⇠`, who sent it, from which
conversation and when; one out has a cyan bar with `⇢ to …`. Asked about one, you let it in, let in
all from that peer for the session, or reject it, the sender not told; closing the question leaves it
waiting for `/peers`. A message let in reaches the model at its next step, or starts a turn when it is
idle (`wake`). After 10 messages in a row let in without you, the next asks, in any setting; your
next prompt starts the count again. Subagents have no `peers` tool.

`/peers` lists them with how this one is on the network, `/peers scan` asks the network again;
`/peers name <name>|auto`, `/peers incoming|outgoing <setting>`, `/peers visible|hidden|local` and
`/peers on|off` change the session only.

## Instructions

mo reads `AGENTS.md` (and `AGENT.md`) into the system prompt: yours in `~/.config/mo/`, then each one
from the repository's root down to the working folder, so the closest come last. `/settings ›
Conversation` turns them off or names other files, such as `CLAUDE.md`, globally or per project.

## Skills

A skill is a folder with a `SKILL.md`: front matter with `name` and `description`, then the
instructions, and any other files they refer to. mo looks in `~/.config/mo/skills/`, in the folders
of `skills.dirs` (`~/.claude/skills` works as is), and in the project's `.mo/skills/`, which wins
for a name found twice.

```markdown
---
name: release
description: Cut a release: changelog, tag and the GitHub release
---
1. ...
```

With at least one skill on, the model gets a `skill` tool that loads one: its instructions, its
folder and the list of its other files. `skills.mode` says how it learns what there is: `full` puts
every name with its description in that tool's description, `compact` only the names, `search`
adds `skill_list` and `skill_search` to find them. Without skills, or with every one off, no skill
tool is sent. `/settings › Skills` picks the default mode; `Each skill ›` lists every skill to turn
it off or give it its own mode, globally or per project. Compact mode comes with `skill_list` and
`skill_search` too.

You load one yourself by starting a message with `$name` (`$pdf fill form.pdf`) or with
`/skill name text`; both complete the name as you type. The skill goes with that message only.
`/skill` alone lists them. A `$` that names no skill, like `$HOME`, is sent as it is.

## Conversations

Every conversation is saved in `~/.local/share/mo/sessions/`, readable by you only: after each step
of a turn (a second apart at most), at its end, and whenever mo exits, also mid-turn, so a crash
loses a step at most.

- `/resume` lists the conversations of this folder, the latest first; `mo -r` (or `mo --resume`)
  opens on that list, and `mo -c` continues the latest.
- `/rewind`, or esc twice with an empty input, lists your prompts: the one you pick comes back into
  the input to edit and send, and the conversation goes on from before it, in a fork. The
  conversation as it was stays in `/resume`.
- `/fork` goes on in a copy; `/new` starts a new conversation on a clear screen. Neither loses
  the old one.
- `/delete` deletes the conversation in progress, with the output of its commands, after asking. In
  `/resume`, ctrl+x, delete or a click on ✕ deletes one, after asking too. `/clean <date or age>`
  deletes those of this folder last used before it: `/clean 2026-08-01`, `/clean 2 months`,
  `/clean 3 weeks`. Nothing is deleted on its own.
- A conversation is named after its first prompt. `/title <name>` renames it, `/title auto` asks
  the model for a short title in the language you write in (a spinner says where it is, with the
  last two lines it writes under it, and esc stops it; the title is what it puts in <title></title>), and `/title` alone says the current
  one. It works while the model is busy, and the name shows in `/resume` and the side panel.
  The agent has a `title` tool too, so "rename this conversation" in a prompt does it.

Going back does not undo what tools changed on disk since that prompt.

Tool calls show their first lines; ctrl+o, or a click on one, shows the whole call and output.

## Notifications

mo calls you back when a turn ends and when a call asks for permission, only while its terminal is
not focused. `auto` uses the terminal's own notifications where it has them (Ghostty, WezTerm,
foot, kitty, iTerm2; they work over ssh and through tmux), else `notify-send` or macOS
notifications, else the bell. `/notify` sends a test. A terminal that does not report focus
counts as focused once you type: set `focused = true` there.

## Permissions

The status line shows the mode; shift+tab, a click on it or its menu changes it, and the mode is kept until you pick another: where the user settings had it, for this project or for all.

| Mode | What runs without asking |
|---|---|
| `ask` | reading; everything that changes files or runs asks |
| `edits` | reading and file changes in the working directory (the default) |
| `auto` | as `edits`, and the commands and fetches the model judges safe for your request |
| `all` | everything, except what a `deny` rule names |

In every mode a `bash` command runs when rules allow each of its parts: the commands joined by
pipes, `&&` or `;`, inside loops, and inside `$(...)`. A part that writes to a file with `>` asks
even when a rule allows it. A fetch is checked as `web_fetch <url>`; a rule written as `fetch <url>`, the tool's name before it was renamed, still applies, a `deny` one too, and so does `fetch` in `confirm`, `disabled`, `always_loaded` and `modes`.

A rule is a command where `*` stands for anything: alone it is any words or none (`ls *` also
takes `ls`, `ls * /home` takes `ls -la /home`), inside a word any characters of it (`git log*`).
`re:` starts a regular expression. `deny` wins over `allow`. A wildcard on a command that runs
others (`find *`, `xargs *`, `env *`, `sudo *`) lets those through too.

When a command asks, `a` opens the parts no rule allows as rules: put the `*` where you want, then
save them for every project or for this folder only. `/settings` › Safety edits the lists, one
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
ca = "~/corp-ca.crt"       # a PEM authority trusted besides the system's, for a private one
# insecure = true          # any certificate, self-signed too; anyone between could read it
```

Their tools are named `mcp__<server>__<tool>` and ask before running unless the server has
`trust = true`. In `search` tool mode they stay out of the context until the model looks for them.
`/mcp` lists the servers with how each is doing (`● N tools · mode`, `○ off`, `✗ why`) and edits them.
There a server's **On** and **Tool mode** are fields too, saved to `tools.mcp_disabled` and
`tools.mcp_modes` (and to the project's file when it sets those): only that server connects or
stops, the others keep their connection. **Test connection** in a server's editor
connects to it as it is filled in, saved or not, and says how many tools it offers or why it failed
(a command server is started and stopped again). `/settings › Tools › MCP servers off` stops
one without deleting it, everywhere or only in a project. A server's stderr goes to `~/.local/share/mo/logs/mcp-<name>.log`.
`[web]` and `[mcp]` are read from the user file only, never from a project's `.mo/settings.toml`.

## Images and diagrams

Answers with a ```` ```mermaid ```` or ```` ```svg ```` block, or a line that is only `![](local/file.png)`,
are drawn in the terminal, an image line with its name and path under it (`name · path`); so are
images that `read` opens or an MCP tool returns, and `/view <file>`
shows an image, SVG or Mermaid file. Any other text file `/view` opens over the whole screen,
highlighted as its name says and numbered, to scroll, search with `/` and copy, as a job's output
is watched; only you see it, the model never does. Tab after `/view ` offers the files and folders
there, folders first; tab on a folder goes into it. A file is also found by what its name holds:
`/view @mifile` offers the project's files by the same search as `@` (names that start with it
first, then those that hold it, then those with its letters in order), a word that is no path
here does the same, and a pattern names files: `*.go` any `.go` at any depth, `**/*mifile.*` or
`src/**/*.md` by path (`**` crosses folders, `*` stays in one, `?` is one character, capitals do
not matter). One match opens, several are listed to pick from. `/view` alone, or on a folder, opens
the viewer with a tree of the folder on its left, as an editor has: ↑↓ move, → or enter open a
folder, ← closes it or goes to the one over a file, → shows a file and enter opens it and moves to
it, `/` narrows the tree to the files that hold what you type (a pattern with `*` too), tab goes
between the tree and the file, esc clears the search and then closes, a click on a row does what
enter does. In a file `/view` opened, `t` opens the tree on that file, and again hides it. A
terminal narrower than 60 columns has no room for both and lists the folder to pick from instead.

In a file `/view` opened, ↑↓ move a cursor, and `c` comments its line, or the lines you dragged
over; the file is not touched. `c` on a comment edits it, `d` deletes it. `a` closes the viewer and
attaches the file to your message: as `[comments name]` with comments, which the model gets with
the path, the lines and their first line quoted; as `[file name]` without. In the input, `@` and a
few letters offer the files of the project whose name or path holds them, or their letters in
order; tab or enter attaches the one picked.

`/git show` opens the same viewer on the changes not committed, staged or not, and
`/git compare <branch>` on what the branch checked out adds over that one, as a pull request shows
it: the commits since the two parted, so what only the other branch has is left out. The
tree lists only the files that changed: new in green, changed in amber, deleted in red. Beside it,
the diff of the file picked: three lines around each change, the lines taken out on red and those
put in on green, numbered and highlighted. The unchanged lines between are folded into a `⋯` row:
on it `e` shows 20 more, `E` all of them, and `f` anywhere shows the whole file, or the changes
alone again. ↑↓ move a cursor, and `c`, `d` and `a` comment lines and attach them as in a file
`/view` opened: the model gets each comment with its line in the new file, or in the old one for a
line taken out. Tab after `/git compare ` offers the branches.

`/git show <revision>` opens the changes of that commit the same way.

Every action of the `git` tool is there for you too, typed as git takes it and run without the git
command: `/git status`, `diff [--staged] [paths]`, `log [-n N] [path]`, `blame <path>`, `check`,
`switch [-c] <branch>`, `branch <name>`, `add [paths]`, `commit <message>`, `fetch`, `pull [remote]
[branch] [--rebase|--merge]`, `push`, `merge`, `rebase`, `cherry-pick`, `continue`, `abort`,
`stash [message]`, `stash list|apply|pop|drop [n]`, `tags`, `tag <name> [revision] [-m message]`,
`reset [revision] [--soft|--mixed|--hard]` and `init [folder]`. Each shows as a git call with what it
gave. While the model works, those that change the repository wait for it to end. `/git help`
lists them.

Each takes a repository folder after the action, relative or absolute, for when mo runs above the
repositories, as in a workspace: `/git show ./mo`, `/git log ../api`, `/git compare ./mo master`. The
word is the repository when it is one, or a folder outside this folder's repository; `compare` with
two words always takes the first. Tab offers the repositories first, then the other folders to go
into, and the branches after `compare`, `switch`, `merge`, `rebase` and `cherry-pick`. Outside a
repository with none named, the one in the 2 levels below is taken, or a list offers them.
`/git branch [repo]` lists the branches, the current one marked, to filter (`/`) and switch to one.
`/git repos` lists the repositories found and every one opened, each with its branches; enter lists
the branches of the one picked.

`!command` runs a command in the shell, in the working directory, without a terminal (an editor or a
pager will not work there): `!make test`. It shows as a bash call with its output, and esc or ctrl+c
stops it. The model reads the command and what it printed (its start and end, 8 KB) with your next
message.

```toml
[render]
images = "auto"   # auto | kitty | blocks | off
```

`auto` uses kitty graphics in kitty and ghostty (also inside tmux with `set -g allow-passthrough on`),
coloured half blocks in other truecolor terminals, and text otherwise. SVG is drawn by resvg built
into mo; Mermaid flowcharts, sequence, ER and class diagrams are drawn with box characters, each
box tight around its text, its edge and text in a colour of its own, or as an image when `mmdc`
(mermaid-cli) is installed, which also draws the other kinds. A diagram wider than the conversation says so under it; ctrl+← and
ctrl+→, with nothing typed, move every such diagram across, each to its own edge; a click
opens it over the whole screen, panned by dragging, the arrows, hjkl or the wheel (shift for
across), `s` showing its code. A right click on any diagram shows its code in its place or
the drawing again, all of them so, opens it whole, or copies its code; `/diagrams` switches
all of them. A right click on an SVG, written in an answer or a `.svg` file shown, or on a
diagram mermaid-cli drew as an image, shows its code in its place or the picture again, or
copies its code; a `.mmd` file shown is a diagram like any other. ctrl+↑↓ scroll the conversation; with nothing typed and without vim keys, ctrl+h j
k l are ctrl+← ↓ ↑ →. Node shapes mermaid-ascii does not draw (stadiums, subroutines, cylinders,
circles, hexagons, parallelograms) are drawn as boxes.

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
| enter | send; while the model works, queue it: it waits over the status line, shaded, each message after an icon (💬 or Nerd Font's bubble, » in text), with an amber edge on the left (`⧗ queued · N · ↑ edit` under them) and goes into the conversation when the model reads it, at its next step; a long one wraps over several lines, up to eight; several messages queued together are one entry and the model reads them as one message; ↑ with nothing typed brings them all back into the input to edit, without stopping the model; ctrl+s sends them (and what is typed) now: the model stops where it is and answers them, instead of reading them at its next step; esc gives them back in the input too, but stops the model |
| shift+enter, ctrl+j, alt+enter | new line, in the input and in the boxes of forms |
| `!command` | run it in the shell, in this folder; the model reads it with your next message |
| esc, ctrl+c | stop the current run, or the commands run with `!`; esc with suggestions open (commands, paths, @) closes them first and leaves the run going |
| ctrl+b | send the command that blocks the model to the background |
| `/vim`, or `keys = "vim"` under `[render]` | vim keys: esc to normal mode, i a A I o to write; ctrl+h/j/k/l move between the conversation, the input and the side panel; in the conversation j k gg G ctrl+d ctrl+u move a cursor, l and h open and fold what it is on, v selects lines and y copies them; in the panel j k pick a task or job and l opens its menu; in the input h l w b 0 $ x D dd edit, v selects, u and ctrl+r undo and redo; y yy dd x D copy to the clipboard and p P paste from it; `:` starts a command; in settings lists l goes in and h back. The mode shows before the status line |
| ctrl+x | clear what is typed, also while the model works, which goes on |
| ctrl+c (idle) | clear input, then quit |
| ctrl+d | quit |
| shift+tab | change the permission mode |
| ctrl+o, click | open or fold reasoning, tool calls and long errors |
| esc esc (empty input) | go back to one of your prompts |
| up, down (empty input) | go through what you sent before, prompts and commands, kept between sessions in `~/.local/share/mo/history` |
| ctrl+v | paste a copied image or files as attachments |
| backspace, delete next to `[image #1]` | take the attachment out whole; the chips over the input drop one with a click |
| pgup, pgdown, shift+up/down, mouse wheel | scroll the conversation |
| drag | select text and copy it; the shared indent is taken off. Held at the top or bottom of the conversation, it scrolls on and keeps selecting |
| click | buttons, options and tabs of a dialog, a suggested command, the mode (next), the provider (its settings), the model (a list to pick from), a job (its last lines), a settings option |
| click on the branch | the files changed, each with its kind |
| right click | a small menu by the pointer, over the screen (↑↓ or j k, enter or a click, esc or a click outside closes it): the mode: all modes to pick · a task: change its state or delete it · a job in the panel, or a bash call still running: watch its output live, its last lines, send it to the background, kill it |
| wheel over the side panel | scroll its tasks and jobs |
| `/new` `/resume` `/rewind` `/fork` `/delete` `/clean <age>` `/title` `/model` `/login` `/logout` `/compact [instruction]` `/cache clear` `/settings` `/reload` `/providers` `/mcp` `/plugins` `/agents` `/view [file]` `/git <action> [repo]` `/peers` `/help` `/quit` | commands; type `/` for suggestions, tab or enter to take one |
| `/<name> text` | a plugin command |
