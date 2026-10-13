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

Inside mo, `/settings` edits provider, model, tools, context and memory, shows what every request
costs in tokens, saves to `~/.config/mo/config.toml` and applies at once, keeping the conversation.

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
models, `system_prompt`, `max_tokens`, `disabled` and `max_output`, and add tools to `confirm`.
Providers with URLs or keys go in the user file.

```toml
provider = "llama-server"
model = ""                 # empty: first model the server lists
max_tokens = 0             # 0: provider default
system_prompt = ""         # empty: built-in prompt (~80 tokens)

[tools]
mode = "full"              # full | compact (no descriptions) | search (load tools on demand)
always_loaded = ["bash", "read", "edit"]   # search mode: tools sent from the start
confirm = ["bash", "rm"]   # ask before these; [] never asks
disabled = []              # e.g. ["mkdir", "ls"] to save context
max_output = 10000         # bytes of tool output sent to the model

[tools.descriptions]       # replace what the model reads about a tool; "" sends none
grep = "Regex search in files."

[context]
window = 0                 # 0: ask llama-server, else 32768 (local) / 200000 (anthropic)
compact_at = 0.75          # summarize at this share of the window; 0 = never
compact_prompt = ""        # empty: built-in summary prompt

[memory]
dir = "~/.local/share/mo/memory"

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

`bash` `read` `write` `edit` `ls` `mkdir` `rm` `grep` `memory`, plus `tool_search` in search mode.
Output is cut to `max_output`, keeping the head and the tail.

`memory` keeps notes across sessions, per project or global. Their first lines go into the system
prompt so the model knows what it saved.

Changes outside the working directory, to mo's config, under `.git` or to hidden files in your home
always ask, even with `-y` or after answering "always". So do changes to global memories.

## Keys

| Key | Action |
|---|---|
| enter | send |
| ctrl+j, alt+enter | new line |
| esc, ctrl+c | stop the current run |
| ctrl+c (idle) | clear input, then quit |
| ctrl+d | quit |
| `/clear` `/compact` `/settings` `/help` `/quit` | commands |
