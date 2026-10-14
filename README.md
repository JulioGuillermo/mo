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
Subscription logins (Claude, ChatGPT, Google OAuth) are listed as not usable and never read:
Anthropic forbids using Claude subscription tokens outside its own apps. Claude works with an API key.

## Config

`~/.config/mo/config.toml`, then `.mo/settings.toml` in the project (the old `.mo.toml` is still
read when that does not exist). Each layer overrides only what it sets.

`/settings` has two screens, switched with tab: **Global**, for every project, and **This project**
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


```toml
provider = "llama-server"
model = ""                 # empty: first model the server lists
max_tokens = 0             # 0: provider default
effort = ""                # empty: the model's default · low | medium | high | xhigh | max (Anthropic)
                           # · none | minimal | low | medium | high | xhigh (OpenAI's reasoning_effort)
system_prompt = ""         # empty: built-in prompt (~80 tokens)

[tools]
mode = "full"              # full | compact (no descriptions) | search (find tools on demand)
load = "context"           # search mode: context (found tools stay in the conversation, called
                           # through tool_call) | declare (found tools are declared from then on)
always_loaded = ["read", "edit", "write", "bash", "ask", "recall"]   # search mode: declared from the start
confirm = ["bash", "rm", "fetch"]   # ask before these; [] never asks. A list of your own
                                    # replaces this one: keep "fetch" in it to be asked
disabled = []              # e.g. ["mkdir", "ls"] to save context
mcp_disabled = []          # MCP servers set up but not started, e.g. ["docs"]
max_output = 10000         # bytes of tool output sent to the model

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
sidebar = "auto"           # side panel of tasks and jobs when wide enough; "off" never
status = "powerline"       # segments on backgrounds with arrows (needs a Nerd Font); "plain" for any font
icons = "nerd"             # Nerd Font icons for the branch and its changes; "text": + ~ - » ? ! ✓

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

`bash` `read` `write` `edit` `ls` `mkdir` `rm` `grep` `memory` `fetch` `web_search` `todo` `ask` `jobs` `recall` `git`, plus `tool_search` in
search mode and the tools of your MCP servers.
Output is cut to `max_output`, keeping the head and the tail.

`fetch` reads a URL as markdown (the article of an HTML page, indented JSON, plain text), 8 KB at a
time. `web_search` uses DuckDuckGo by default, with no key; see `[web]` below for the others.

`todo` is the model's task list for work of several steps. On a terminal 110 columns or wider it
shows in a side panel beside the conversation, whole, with the background jobs; on a narrower one,
above the input while a task is left. `/sidebar` hides or shows the panel.

`bash` with `background: true` runs a command as a job, for servers, watchers and long builds, and
returns at once; the `jobs` tool lists them, reads their output and kills them. When a job ends the
model is told: at its next step if it is working, or in a turn of its own if it is idle, so it can
react, also after you stop it with esc. `/jobs tell` lets it hear one that is waiting, as when a
settings screen was open. `/jobs` lists them, `/kill <n>` stops one, and mo kills what is left when it exits.

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

`memory` keeps notes across sessions, per project or global. They are not put in the prompt:
`recall` searches them and the saved conversations (BM25, by words) and reads one whole, a memory,
a conversation's turns, or one turn, when the model decides it needs it. It keeps to this folder
unless asked for all.

Changes outside the working directory, to mo's config, under `.git` or to hidden files in your home
always ask, even with `-y` or after answering "always", in every mode but `all`. So do changes to
global memories.

## Instructions

mo reads `AGENTS.md` (and `AGENT.md`) into the system prompt: yours in `~/.config/mo/`, then each one
from the repository's root down to the working folder, so the closest come last. `/settings ›
Instructions` turns them off or names other files, such as `CLAUDE.md`, globally or per project.

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
tool is sent. `/settings › Skills` turns skills off and picks the mode, globally or per project.

## Conversations

Every conversation is saved after each turn in `~/.local/share/mo/sessions/`, readable by you only.

- `/resume` lists the conversations of this folder, the latest first; `mo -c` continues the latest.
- `/rewind`, or esc twice with an empty input, lists your prompts: the one you pick comes back into
  the input to edit and send, and the conversation goes on from before it, in a fork. The
  conversation as it was stays in `/resume`.
- `/fork` goes on in a copy; `/clear` starts a new conversation. Neither loses the old one.
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
`/mcp` shows which servers answered and edits them. `/settings › Tools › MCP servers off` stops
one without deleting it, everywhere or only in a project. A server's stderr goes to `~/.local/share/mo/logs/mcp-<name>.log`.
`[web]` and `[mcp]` are read from the user file only, never from a project's `.mo/settings.toml`.

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
| ctrl+o, click | open or fold reasoning, tool calls and long errors |
| esc esc (empty input) | go back to one of your prompts |
| up, down (empty input) | go through what you sent before, prompts and commands, kept between sessions in `~/.local/share/mo/history` |
| ctrl+v | paste a copied image or files as attachments |
| backspace, delete next to `[image #1]` | take the attachment out whole; the chips over the input drop one with a click |
| pgup, pgdown, shift+up/down, mouse wheel | scroll the conversation |
| drag | select text and copy it; the shared indent is taken off |
| click | buttons, options and tabs of a dialog, a suggested command, the mode (next), the provider (its settings), the model (a list to pick from), a job (its last lines), a settings option |
| click on the branch | the files changed, each with its kind |
| right click | the mode: all modes to pick · a task: change its state or delete it · a job: its output and how it stands, or kill it |
| wheel over the side panel | scroll its tasks and jobs |
| `/clear` `/resume` `/rewind` `/fork` `/title` `/model` `/login` `/logout` `/compact` `/settings` `/providers` `/mcp` `/plugins` `/view <file>` `/help` `/quit` | commands; type `/` for suggestions, tab or enter to take one |
| `/<name> text` | a plugin command |
