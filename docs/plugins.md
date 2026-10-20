# Plugins

A plugin is a Lua file in `~/.config/mo/plugins/`. mo runs every one when it starts. A plugin can
add tools for the model, commands for you, hooks on what happens in a turn, items in the side panel
and dialogs, and send the model a message of its own. `/plugins` lists what each one added, or why it failed to load.

```lua
-- ~/.config/mo/plugins/hello.lua
mo.command{ name = "hello", description = "Say hello", run = function(text)
  return "**Hello** " .. (text ~= "" and text or "there")
end }
```

Restart mo, then type `/hello ana`.

## Contents

- [Where the files go](#where-the-files-go)
- [Tools for the model](#tools-for-the-model)
- [Commands for you](#commands-for-you)
- [Hooks](#hooks)
- [MCP servers](#mcp-servers)
- [Settings](#settings)
- [The side panel](#the-side-panel)
- [Dialogs](#dialogs)
- [Keys](#keys)
- [Sending to the model](#sending-to-the-model)
- [What a plugin can use](#what-a-plugin-can-use)
- [Limits and errors](#limits-and-errors)
- [The Lua version](#the-lua-version)
- [Examples](#examples)

## Where the files go

| Path | What |
|---|---|
| `~/.config/mo/plugins/*.lua` | plugins, run in name order when mo starts |
| `~/.config/mo/lib/name.lua`, `~/.config/mo/lib/name/init.lua` | modules plugins load with `require("name")`, to share code |
| `~/.local/share/mo/logs/plugin-<name>.log` | what a plugin prints, and its errors with their stack trace |

A plugin is your own code and runs with your rights. mo never loads one from a project folder.
Changing a file takes a restart of mo. Turning a plugin off or on, or changing its settings, in
**/settings › Plugins** loads the plugins again once saved, and so does `/reload` after you edit
`[plugins.<name>]` by hand.

## Tools for the model

```lua
mo.tool{
  name = "git_log",
  description = "Recent commits of the repository, one per line.",
  params = { count = { type = "integer", description = "how many, default 10" } },
  required = {},
  confirm = false,
  run = function(args)
    local out, code = mo.exec("git log --oneline -n " .. (tonumber(args.count) or 10))
    if code ~= 0 then error(out, 0) end
    return out
  end,
}
```

| Field | Meaning |
|---|---|
| `name` | a letter or `_`, then letters, digits, `_` or `-`; 48 characters at most |
| `description` | what the model reads to decide when to call it |
| `params` | the JSON schema properties of the arguments |
| `required` | the names of the arguments it cannot do without |
| `confirm` | `true` asks you before each call, as an `edit` does |
| `run(args)` | gets the arguments as a table; what it returns is the result. A table is sent as JSON, nothing as no output, and `error(...)` as a failed call |

A tool whose name another tool already has is left out, with a warning in `/plugins`.

## Commands for you

```lua
mo.command{ name = "standup", description = "My commits since yesterday", run = function(text)
  return "**Since yesterday**\n\n" .. mo.exec("git log --since=yesterday --format='- %s'")
end }
```

`/standup more words` calls `run("more words")`. What it returns is shown as Markdown, cut to
64 KB; the model does not see it. A command runs off the screen, like a turn: the spinner turns and esc stops it.
A name of mo's own commands (`new`, `settings`, `mcp`...) is refused, and of two plugins with the
same command only the first keeps it.

## Hooks

`mo.on(event, function)` runs the function at that point of every turn of the main conversation.
Subagents do not run hooks.

| Event | When | Gets | Returns |
|---|---|---|---|
| `prompt` | before your message goes to the model | the text | text to send instead; `nil` keeps it |
| `tool_call` | before a call runs, before anyone is asked | `{ name, args }` | a reason blocks the call; `nil` lets it go |
| `tool_result` | after a call ran, before the model reads it | `{ name, args, output, is_error }` | text the model reads and you see instead; `nil` keeps it |
| `stream` | while the reply comes, at most every 300 ms | `{ text, reasoning }` | nothing |
| `turn_end` | when a turn ends, well, with an error or stopped | `{ prompt, answer, error, stopped }` | nothing |

```lua
mo.on("tool_call", function(call)
  if call.name == "bash" and call.args.command:find("push %-%-force") then
    return "no force pushes"
  end
end)

mo.on("tool_result", function(result)
  return (result.output:gsub("TOKEN=%S+", "TOKEN=***"))
end)

mo.on("turn_end", function(turn)
  if not turn.stopped then
    mo.run{ argv = { "notify-send", "mo", turn.error or "done" } }
  end
end)
```

How each one behaves when something goes wrong:

- `tool_call` is a guard. One that fails blocks the call, and a plugin with a `tool_call` hook that
  failed to load, or was turned off, blocks every call: a broken guard must not let everything
  through.
- `prompt` and `tool_result` that fail, return nothing or return more than 256 KB leave the text as
  it was. The error goes to the log.
- `stream` gets the pieces of the reply gathered, never one call per token. A piece that comes
  after 300 ms of quiet goes at once; the pieces that follow it within 300 ms wait, then go
  together in one call. `text` and `reasoning` are only what came since the last call. The hook
  runs on its own, so a slow one never slows the reply down, and it can still be running when the
  turn ends.
- `turn_end` runs once per turn, also when esc stopped it (`stopped = true`). `error` is `nil` when
  the turn ended well.

## MCP servers

`mo.mcp{...}` adds an MCP server, connected with those of your settings. It takes what
`[mcp.name]` does, and `headers` may be a function: mo asks it for the headers of every request,
so a plugin can sign each one, or refresh a token it keeps.

```lua
mo.setting{ name = "token", secret = true }

mo.mcp{
  name = "board",
  url = "https://board.example/mcp",
  headers = function(request)            -- { method, url }
    return { Authorization = "Bearer " .. mo.settings.get("token") }
  end,
}
mo.mcp{ name = "local", command = "board-mcp", args = { "--stdio" }, env = { LEVEL = "debug" } }
```

| Field | What |
|---|---|
| `name` | its name, as in `[mcp.name]`; its tools are `mcp__name__tool` |
| `url` or `command` | where it is: one of them |
| `headers` | a table, or a function of `{ method, url }` giving a table, for a `url` |
| `args`, `env` | for a `command` |
| `ca`, `insecure`, `trust` | as in `[mcp.name]` |

A server of your settings with the same name wins, and the plugin's is left out with a warning in
`/plugins`. `tools.mcp_disabled` turns it off, and its tools take modes in **Settings › Tools** like
any server's. A header function has 5 seconds, and an error in it fails the request. When the
plugins load again, their servers connect again.

## Settings

A plugin declares the values you set for it, and reads them with `mo.settings.get`:

```lua
mo.setting{ name = "site", default = "example.atlassian.net", description = "Your Jira site" }
mo.setting{ name = "token", secret = true, description = "API token" }
mo.setting{ name = "limit", type = "number", default = 20 }

mo.tool{ name = "jira_search", run = function(args)
  local site, token = mo.settings.get("site"), mo.settings.get("token")
  -- ...
end }
```

| Field | What |
|---|---|
| `name` | how the plugin reads it; any name but `enabled` |
| `type` | `string` (the default), `number` or `boolean` |
| `default` | what `mo.settings.get` gives while you set nothing; `nil` if left out |
| `description` | shown when you edit it |
| `secret` | `true` shows it as `••••••` and hides it as you type |

You set them in **/settings › Plugins › name**, where each plugin also has its on/off switch. They
are saved in your `config.toml`; a value equal to the default is not written:

```toml
[plugins.jira]
site = "acme.atlassian.net"
token = "..."

[plugins.old_thing]
enabled = false
```

A plugin with `enabled = false` does not run at all. `mo.settings.get` of a name the plugin did not
declare is an error, and a value of the wrong type in the file gives the default.

## The side panel

Items go under **Plugins** in the side panel.

```lua
mo.sidebar.set{
  id = "jira", text = "Jira: logged in", detail = "12 open issues",
  color = "success", icon = "●",
  on_click = function() return "text shown as a note" end,
}
mo.sidebar.remove("jira")
```

| Field | Meaning |
|---|---|
| `id` | names the item within the plugin; `set` with the same id changes it |
| `text` | the line shown, cut to the panel's width |
| `detail` | a second, dimmer line |
| `color` | of the icon: `text`, `dim`, `accent`, `success`, `warning` or `error` |
| `icon` | one character; `•` when not set |
| `on_click` | runs on a click; the text it returns shows as a note. Without it the item is not clickable |

`set` and `remove` work at any time: while the plugin loads, from a tool, a command, a hook or a
dialog's event.

## Dialogs

`mo.dialog` opens a dialog over the screen at once. It takes the same nodes as the model's `ui`
tool: `panel`, `card`, `grid`, `label`, `button`, `input`, `textarea`, `select`, `slider`,
`checkbox`, `switch`, `radio` and `svg`; and, for plugins only, `canvas`, which the plugin draws
itself, and its own [components](#components).
`{"action":"help","topic":"nodes"}` of the `ui` tool lists every field.

```lua
local dialog
dialog = mo.dialog{
  title = "New issue",
  tree = { type = "card", title = "New issue", children = {
    { type = "input", id = "summary", placeholder = "what happened" },
    { type = "panel", direction = "row", gap = 2, children = {
      { type = "button", id = "save", label = "Save" },
      { type = "button", id = "cancel", label = "Cancel", on_click = "close" },
    } },
  } },
  on_event = function(event)
    if event.element == "save" then
      mo.sidebar.set{ id = "last", text = "Saved: " .. event.values.summary }
      dialog.close()
    end
  end,
}
```

| Field | What |
|---|---|
| `title` | in the dialog's top edge |
| `tree` | its nodes |
| `on_event` | gets what you do in it |
| `size` | `"full"` fills the screen; left out, it is as big as what it holds |
| `listed` | `true` keeps it under **Plugins** in the side panel, to open again once hidden |
| `frame` | `false` draws it without its frame: no edges, title, × or keys line; with `size = "full"`, the interface takes every cell of the screen |

`mo.dialog` gives a handle:

| | |
|---|---|
| `dialog.id` | its number, from 1001 on |
| `dialog.update{ title, tree }` | changes the title, the tree or both; what you typed stays |
| `dialog.bounds()` | `{ x, y, width, height }` where it is drawn now, `nil` while it is not on screen |
| `dialog.close()` | takes it away |

What you do in the dialog goes to `on_event`, never to the model. The event is
`{ dialog, element, event, action, value, values }`: the element's id, `click`, `input` or
`change`, the action it names, its value, and the value of every input of the dialog.

| A node's `on_click`, `on_input` or `on_change` | What happens |
|---|---|
| not set, on a button | `on_event` gets the click |
| `close` or `submit` | `on_event` gets it and the dialog hides |
| `remove` | `on_event` gets it and the dialog goes |
| any other name | `on_event` gets it with that name as `action` |

Esc, the × and a click outside close a dialog for good, and `on_event` gets
`{ event = "close", action = "close", values }`, so the plugin can keep what was typed. So does a
node's `close` or `submit`. A dialog opened with `listed = true` only hides instead: it stays under
**Plugins** in the side panel, to open again with a click, until the plugin calls `close`. Dialogs
live while mo runs and are never saved. An error in `on_event` shows under the dialog and goes to
the log.

### Sizes

Every node takes `width` and `height`, as cells or `"full"`, the least and most it takes, and
`grow`, as CSS flex-grow. Sizes count with its padding and border.

| Field | What |
|---|---|
| `width`, `height` | cells, or `"full"`: all the room its parent gives it; beside others in a row or a column, what they leave; at the root, the whole dialog |
| `min_width`, `max_width`, `min_height`, `max_height` | the least and most it takes |
| `grow` | a share of the room left along its parent: width in a row, height in a column that has a height to give |

```lua
mo.dialog{ title = "Mail", size = "full", tree = { type = "panel", direction = "row", height = "full", children = {
  { type = "panel", width = 24, children = { --[[ the folders ]] } },
  { type = "panel", width = "full", children = {
    { type = "input", id = "subject", width = "full" },
    { type = "textarea", id = "body", grow = 1 },
    { type = "button", id = "send", label = "Send" },
  } },
} } }
```

`mo.screen()` gives `{ width, height }` of the terminal, or `nil` when there is none, as when mo
answers once.

### Text of many lines

A `textarea` takes text of many lines: enter starts a new line, tab moves on. `language` colours
it as code; it is 6 rows high unless `height` or `grow` say otherwise. With `size = "full"` it makes
a small editor:

```lua
mo.command{ name = "note", run = function()
  local dialog
  dialog = mo.dialog{ title = "Note", size = "full", tree = { type = "panel", children = {
    { type = "textarea", id = "body", language = "markdown", height = "full", value = mo.read("NOTES.md") or "" },
    { type = "button", id = "save", label = "Save" },
  } }, on_event = function(event)
    if event.element == "save" or event.event == "close" then
      mo.write("NOTES.md", event.values.body)
      dialog.close()
    end
  end }
end }
```

### Components

`mo.component{ name, render }` adds a node type of the plugin's own. Where a tree has a node of
that type, `render` gets its fields, `children` included, and gives the nodes to draw in its place,
which may hold other components. The name can be none of mo's node types, nor a name taken as one
such as `table` or `field`.

```lua
mo.component{ name = "labelled", render = function(props)
  return { type = "grid", columns = 2, gap = 2, children = {
    { type = "label", text = props.label },
    { type = "input", id = props.id, value = props.value },
  } }
end }

mo.dialog{ tree = { type = "card", title = "Issue", children = {
  { type = "labelled", id = "summary", label = "Summary" },
  { type = "labelled", id = "owner", label = "Owner", value = "ana" },
} } }
```

### Canvas

A `canvas` is drawn by the plugin, cell by cell: `draw(w, h)` gets its size and gives its lines,
as a string or a list, with any colours. It takes `width`, `height`, their least and most and
`grow` like any node, and holds the keys and the mouse while it has the focus.

```lua
local rows, at = { "hello" }, 1
local dialog
dialog = mo.dialog{ title = "pad", size = "full", frame = false, tree = {
  type = "canvas", id = "pad", width = "full", height = "full", keep_esc = true,
  draw = function(w, h)
    local lines = {}
    for i = 1, h do
      local text = rows[i] or "~"
      if i == at then text = mo.style(text, { bg = "surface" }) end
      lines[i] = text
    end
    return lines
  end,
}, on_event = function(e)
  if e.event == "key" and e.value == "j" then at = math.min(at + 1, #rows) end
  if e.event == "key" and e.value == "k" then at = math.max(at - 1, 1) end
  if e.event == "key" and e.value == "q" then dialog.close() end
end }
```

| | |
|---|---|
| `draw(w, h)` | gives the lines at that size; mo cuts what is wider or longer and fills what is short |
| `redraw` | `true` draws it again on its own as often as `fps` lets; a function is asked each frame and draws it again when it gives `true`; left out, it is drawn again only when it changes size, after one of its events, or on `dialog.redraw()` |
| `fps` | the most it is drawn again a second on its own, at most **Settings › Interface › Plugin UIs** (`render.ui_fps`, 20 unless set) |
| `keep_esc` | `true` gives it esc too; ctrl+c closes the dialog then |
| `focusable` | `false` for one that only shows: tab skips it and keys never reach it |

Its events reach `on_event` with `element` its id:

| `event` | With |
|---|---|
| `key` | `value` the key as mo names it (`j`, `ctrl+x`, `enter`, `tab`, `space`, `pgdown`), `text` what it types |
| `click` | `x`, `y`: the cell, from 0 |
| `wheel` | `value` `up` or `down`, `x`, `y` |
| `focus`, `blur` | the focus reached or left it |
| `resize` | `value` the new size, as `80x24`, before it is drawn at it |

Draw is called off mo's screen, which shows the last lines drawn meanwhile: a slow draw slows the
canvas, never mo. `mo.style(text, { fg, bg, bold, italic, underline, reverse })` colours text;
`mo.width(text)` is the cells it takes, wide characters counted as two.

`dialog.redraw(id)` draws a canvas again, or all of the dialog's with no id.

A component can draw itself too: its `render` gives `{ size, draw, redraw }` instead of nodes.
`size(most_w, most_h)`, with the screen's size as the most, gives `{ w, h }`, its preferred size,
and if it likes `min_w`, `max_w`, `min_h` and `max_h`; the node's own sizes win. It becomes a
canvas, with the node's `id` or one of its own, that takes no focus unless its render gives
`focusable = true`.

```lua
mo.component{ name = "meter", render = function(props)
  return {
    size = function() return { w = 20, h = 1 } end,
    draw = function(w) local full = math.floor(w * props.value) return string.rep("█", full) .. string.rep("░", w - full) end,
  }
end }
```

### Showing a file

`mo.view(path)` opens a file or folder in mo's viewer, as `/view` does, over what is on screen; a
relative path is in the working directory. It gives `false` when there is no screen to show it on,
as when mo answers once.

## Keys

`mo.on("key", [keys], function)` hears the keys you press while one of the plugin's dialogs is
open, before the dialog gets them. `mo.global.on("key", [keys], function)` hears them anywhere in
mo, the input included, but only once you allow it.

```lua
mo.on("key", { "ctrl+s", "ctrl+q" }, function(e) ... end)   -- these keys
mo.global.on("key", "ctrl+*", function(e)                    -- every ctrl+ key
  if e.key == "ctrl+e" then open_editor() return true end
end)
mo.global.on("key", function(e) ... end)                     -- every key
```

The function gets `{ key, text }`: the key as mo names it (`j`, `ctrl+x`, `alt+enter`, `f5`,
`pgdown`) and what it types. Returning `true` takes the key: it goes no further, to no other
plugin, the dialog or the input. A key ends in `+*` for every key with those modifiers. The plugin
whose dialog is open hears first, then those allowed to hear globally, in name order.

- A plugin with `mo.global.on` gets **Global events** in **/settings › Plugins › name**, off until
  you turn it on, saying which keys it asks for. Plugins that ask for none never show it.
- The status line ends in a dot, after the folder, while any plugin asks: yellow when none is allowed,
  red when one hears. A click on it opens **Settings › Plugins**.
- Esc and ctrl+c reach no plugin, nor anything typed in mo's own settings, menus and questions.
- A function has 50 ms: a key it holds longer goes on as if it took nothing.

## Sending to the model

```lua
mo.send{ title = "BSSP-1234 moved to review", message = "Ana moved it at 17:02 and asked for a check." }
```

`mo.send` gives the model a message with a title, as a note that says which plugin sent it and that
it is not you. It shows in the conversation with a violet bar, as a peer's message does. It returns
`true` when there was a conversation to take it (not in one-shot `mo "prompt"` runs), not whether it
got in.

| Field | Meaning |
|---|---|
| `title` | one line saying what it is |
| `message` | the text, 64 KB at most |
| `wake` | `true` (default): an idle model gets a turn to read it. `false`: it waits for your next message, or the model's next step |

It goes through the permission mode, as anything that reaches the model from outside:

| Mode | What happens |
|---|---|
| `all` | it goes in |
| `auto` | the model judges it with your request; when not sure it is safe, you are asked |
| `ask`, `edits` and the rest | you are asked: let it in, let in everything from that plugin this session, or reject it |

After 10 messages from plugins went in without you, the next one asks again until you write: a
`turn_end` hook that sends at every turn's end cannot keep the model running on its own. Esc puts a
question off; `/plugins` asks it again.

## What a plugin can use

Every library of Lua is there: `io`, `os`, `string`, `table`, `math`, `package`, `coroutine` and
`debug`. The `mo` table adds what Lua lacks to work with the system.

### Commands

| | |
|---|---|
| `mo.exec(cmd)` | runs `cmd` with bash in the working directory; gives its output, errors included, and its exit code |
| `mo.run{ command, argv, cwd, env, stdin, timeout }` | runs `command` with bash, or `argv` with no shell; `env` adds to the environment, `timeout` is in seconds (120). Gives `{ stdout, stderr, code, ok }` |

A command's output keeps its first and last 512 KB. A process it leaves in the background goes on.

### HTTP

| | |
|---|---|
| `mo.http.get(url, opts)` | a GET |
| `mo.http.post(url, body, opts)` | a POST of `body` |
| `mo.http.request{ url, method, headers, body, json, timeout, ca, insecure, redirects }` | any request |
| `mo.http.listen{ port, path, reply }` | waits for one request on this computer |

A request gives `{ status, ok, headers, body }`, header names in lower case, or `nil` and the reason
when there is no answer. A 404 is an answer, with `ok = false`, not an error.

- `json` is a table, sent as JSON with its content type.
- `timeout` is in seconds (30).
- `ca` is a PEM file of authorities trusted besides the system's, for a server signed by a private
  one. `insecure = true` takes any certificate.
- `redirects = false` stops at the first answer instead of following it.

`mo.http.listen` listens on `127.0.0.1` only, on `port` or a free one, for one request to `path`
(any path when not set). It gives `{ port, wait, close }`. `wait(seconds)` gives
`{ method, path, query, body }`, or `nil` and the reason, and closes the listener. The browser gets
`reply`. It is what an OAuth login needs to get its code back.

### Files

| | |
|---|---|
| `mo.read(path)`, `mo.write(path, text)` | a whole file; `write` makes the folders it needs |
| `mo.fs.list(dir)` | `{ name, is_dir, size }` per entry, sorted |
| `mo.fs.stat(path)` | `{ size, is_dir, mode, mtime }`, or `nil` and the reason |
| `mo.fs.mkdir(path)` | with its parents |
| `mo.fs.remove(path, recursive)` | a file or an empty folder; `true` removes a folder and all in it |
| `mo.fs.rename(from, to)`, `mo.fs.glob(pattern)` | |

Relative paths start in the working directory. Lua's `io.open` takes paths from where mo was
started, which is usually the same folder.

### The rest

| | |
|---|---|
| `mo.json_encode(value)`, `mo.json_decode(text)` | JSON; `decode` gives `nil` and the reason on bad JSON |
| `mo.base64_encode`, `mo.base64_decode` | base64 |
| `mo.sha256(text)`, `mo.hmac_sha256(key, text)` | hashes, in hex |
| `mo.url_encode(text)` | for a query string |
| `mo.sleep(seconds)` | stops when the call is stopped |
| `mo.env(name)` | an environment variable |
| `mo.workdir` | the working directory |
| `mo.log(...)`, `print(...)` | a line in the plugin's log |

## Limits and errors

| | |
|---|---|
| a tool or a command | 2 minutes |
| a hook | 5 seconds |
| a dialog's `on_event`, an item's `on_click` | 2 minutes |
| loading the file, the modules it requires included | 10 seconds |

A call stops at its limit, also in the middle of a request, a command or `mo.sleep`. A plugin stuck
where Lua cannot stop it, as in a runaway pattern match, is turned off for the rest of the run.

An error a plugin raises reaches the model, or you, as its message: `plugin jira: not logged in`.
The stack trace goes to the plugin's log. Use `error(message, 0)` for a message without the file
and line in front of it.

What would break mo's screen is turned aside:

- `print`, `io.write`, `io.stdout` and `io.stderr` write to the plugin's log.
- `io.read` and `io.stdin` read nothing.
- `os.exit` raises an error instead of quitting mo.
- `os.execute` runs with no terminal; its output goes to the log.

## The Lua version

Plugins run Lua 5.1 as [gopher-lua](https://github.com/yuin/gopher-lua) implements it, inside mo.
Lua 5.2 and later added things it lacks:

- no `goto`, integer division `//`, bit operators (`&`, `|`, `<<`), `bit32` or `utf8`;
- `load` takes a function; `loadstring` takes text;
- `unpack` instead of `table.unpack`;
- the pattern `%f` (frontier) finds nothing, without an error;
- no C modules: `require` loads Lua files only.

One bug to know: `(text:gsub(a, b) .. c):gmatch(p)` in one expression crashes the interpreter. Put
the `gsub` result in a variable first.

## Examples

- [`examples/plugins/git.lua`](examples/plugins/git.lua): a tool, a command and a guard against
  force pushes.
- A Jira plugin that logs in to Atlassian's MCP server with OAuth and offers its tools to the model
  is a longer example of `mo.http`, `mo.http.listen`, `require` and the side panel; it lives with
  the user's own plugins, not in mo.
