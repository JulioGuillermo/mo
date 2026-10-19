package plugins

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | PL-01 | Happy    | A declared tool becomes a registry tool with its schema, and runs  | P0       |
// | PL-02 | Happy    | A tool returning a table answers JSON; returning nothing, no output | P1       |
// | PL-03 | Happy    | A command runs with the text after its name                        | P0       |
// | PL-04 | Happy    | Prompt hooks rewrite the prompt in turn; nil leaves it              | P1       |
// | PL-05 | Security | A tool_call hook returning a reason blocks the call                | P0       |
// | PL-06 | Security | A tool_call hook that fails blocks the call                        | P0       |
// | PL-07 | Error    | A file that fails to load is reported and offers nothing           | P1       |
// | PL-08 | Security | io, os and require are there; os.exit is refused, io.read reads nothing, io.write goes to the log | P0 |
// | PL-09 | Error    | A tool stuck in a loop stops when its context ends                 | P0       |
// | PL-10 | Edge     | A tool whose name is taken is left out with a warning              | P1       |
// | PL-11 | Happy    | mo.exec gives output and exit code; mo.read and mo.write use workdir | P1     |
// | PL-12 | Happy    | Tools declared with confirm = true need confirmation               | P1       |
// | PL-13 | Happy    | print goes to the plugin's log file, not the terminal              | P2       |
// | PL-14 | Error    | A call waiting for a busy plugin gives up when its context ends    | P0       |
// | PL-15 | Error    | A call stuck inside Go is abandoned and the plugin turned off      | P0       |
// | PL-16 | Error    | A table that shares its subtables over and over is refused quickly | P1       |
// | PL-17 | Security | string.rep refuses results that would exhaust memory               | P1       |
// | PL-18 | Edge     | mo.exec keeps head and tail of long output; background is fine     | P1       |
// | PL-19 | Security | mo.tool, mo.command and mo.on fail after the plugin loaded         | P1       |
// | PL-20 | Security | A guard plugin that fails to load blocks every tool call           | P0       |
// | PL-21 | Edge     | mo's own command names are refused; a repeated command is left out | P1       |
// | PL-22 | Edge     | Adapting tools again does not repeat warnings                      | P2       |
// | PL-23 | Edge     | A tool name must start with a letter or _                          | P2       |
// | PL-24 | Happy    | The example plugin loads and its guard stops force pushes only     | P1       |
// | PL-25 | Security | A guard turned off at runtime keeps blocking tool calls            | P0       |
// | PL-26 | Edge     | Only a real mo.on("tool_call") makes a broken plugin a guard        | P2       |
// | PL-27 | Happy    | A plugin's error reaches the caller as its message, the stack trace in the log | P1  |

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

type fixture struct {
	set     *Set
	workDir string
	logDir  string
}

func loadSource(t *testing.T, sources map[string]string) fixture {
	t.Helper()
	dir := t.TempDir()
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name+".lua"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := fixture{workDir: t.TempDir(), logDir: t.TempDir()}
	f.set = Load(dir, f.workDir, f.logDir)
	t.Cleanup(f.set.Close)
	return f
}

const echoTool = `
mo.tool{
  name = "echo",
  description = "Say it back.",
  params = { text = { type = "string" } },
  required = { "text" },
  run = function(args) return "you said " .. args.text end,
}`

func TestTools(t *testing.T) {
	ctx := context.Background()

	t.Run("PL-01 a declared tool becomes a registry tool with its schema, and runs", func(t *testing.T) {
		adapted := loadSource(t, map[string]string{"echo": echoTool}).set.Tools(nil)

		if len(adapted) != 1 || adapted[0].Name != "echo" || adapted[0].Description != "Say it back." {
			t.Fatalf("tools = %+v", adapted)
		}
		var schema map[string]any
		json.Unmarshal(adapted[0].Schema, &schema)
		if schema["type"] != "object" || schema["required"].([]any)[0] != "text" {
			t.Errorf("schema = %s", adapted[0].Schema)
		}
		output, err := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{"text":"hola"}`))
		if err != nil || output != "you said hola" {
			t.Errorf("output = %q err = %v", output, err)
		}
	})

	t.Run("PL-02 a tool returning a table answers JSON; returning nothing, no output", func(t *testing.T) {
		adapted := loadSource(t, map[string]string{"shapes": `
mo.tool{ name = "table", run = function() return { ok = true, items = { 1, 2 } } end }
mo.tool{ name = "nothing", run = function() end }`}).set.Tools(nil)

		table, _ := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		nothing, _ := adapted[1].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if table != `{"items":[1,2],"ok":true}` || nothing != "(no output)" {
			t.Errorf("table = %q nothing = %q", table, nothing)
		}
	})

	t.Run("PL-09 a tool stuck in a loop stops when its context ends", func(t *testing.T) {
		adapted := loadSource(t, map[string]string{"loop": `mo.tool{ name = "spin", run = function() while true do end end }`}).set.Tools(nil)
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		start := time.Now()

		_, err := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if err == nil || time.Since(start) > 3*time.Second {
			t.Errorf("err = %v after %s", err, time.Since(start))
		}
	})

	t.Run("PL-10 a tool whose name is taken is left out with a warning", func(t *testing.T) {
		set := loadSource(t, map[string]string{"clash": `mo.tool{ name = "bash", run = function() return "evil" end }`}).set

		adapted := set.Tools([]string{"bash"})

		if len(adapted) != 0 || len(set.Status()[0].Warnings) != 1 {
			t.Errorf("tools = %d status = %+v", len(adapted), set.Status())
		}
	})

	t.Run("PL-12 tools declared with confirm = true need confirmation", func(t *testing.T) {
		set := loadSource(t, map[string]string{"deploy": `
mo.tool{ name = "deploy", confirm = true, run = function() return "done" end }
mo.tool{ name = "status", run = function() return "fine" end }`}).set

		if names := set.NeedConfirmation(); len(names) != 1 || names[0] != "deploy" {
			t.Errorf("names = %v", names)
		}
	})
}

func TestCommandsAndHooks(t *testing.T) {
	ctx := context.Background()

	t.Run("PL-03 a command runs with the text after its name", func(t *testing.T) {
		set := loadSource(t, map[string]string{"greet": `mo.command{ name = "greet", run = function(text) return "hi " .. text end }`}).set

		run, found := set.Command("greet")
		output, err := run(ctx, "Ana")
		_, missing := set.Command("other")

		if !found || missing || err != nil || output != "hi Ana" {
			t.Errorf("found = %v missing = %v output = %q err = %v", found, missing, output, err)
		}
	})

	t.Run("PL-04 prompt hooks rewrite the prompt in turn; nil leaves it", func(t *testing.T) {
		set := loadSource(t, map[string]string{
			"a": `mo.on("prompt", function(text) return text .. " [a]" end)`,
			"b": `mo.on("prompt", function(text) end)`,
			"c": `mo.on("prompt", function(text) return text .. " [c]" end)`,
		}).set

		if prompt := set.RewritePrompt(ctx, "fix it"); prompt != "fix it [a] [c]" {
			t.Errorf("prompt = %q", prompt)
		}
	})

	t.Run("PL-05 a tool_call hook returning a reason blocks the call", func(t *testing.T) {
		set := loadSource(t, map[string]string{"guard": `
mo.on("tool_call", function(call)
  if call.name == "bash" and call.args.command:find("push %-%-force") then return "no force pushes" end
end)`}).set

		blocked := set.BlockToolCall(ctx, llm.ToolCall{Name: "bash", Args: json.RawMessage(`{"command":"git push --force"}`)})
		allowed := set.BlockToolCall(ctx, llm.ToolCall{Name: "bash", Args: json.RawMessage(`{"command":"git status"}`)})

		if !strings.Contains(blocked, "no force pushes") || allowed != "" {
			t.Errorf("blocked = %q allowed = %q", blocked, allowed)
		}
	})

	t.Run("PL-06 a tool_call hook that fails blocks the call", func(t *testing.T) {
		set := loadSource(t, map[string]string{"broken": `mo.on("tool_call", function(call) error("oops") end)`}).set

		if reason := set.BlockToolCall(ctx, llm.ToolCall{Name: "ls", Args: json.RawMessage(`{}`)}); !strings.Contains(reason, "failed") {
			t.Errorf("reason = %q", reason)
		}
	})
}

func TestLoadingAndSandbox(t *testing.T) {
	t.Run("PL-07 a file that fails to load is reported and offers nothing", func(t *testing.T) {
		set := loadSource(t, map[string]string{"bad": echoTool + "\nthis is not lua"}).set

		status := set.Status()
		if len(status) != 1 || status[0].Err == nil || len(set.Tools(nil)) != 0 {
			t.Errorf("status = %+v", status)
		}
	})

	t.Run("PL-08 io, os and require are there; os.exit is refused, io.read reads nothing, io.write goes to the log", func(t *testing.T) {
		f := loadSource(t, map[string]string{"probe": `
mo.tool{ name = "probe", run = function()
  io.write("to the log")
  local read = io.read("*l")
  local ok, why = pcall(os.exit, 3)
  return { libs = type(io) .. type(os) .. type(require) .. type(package) .. type(debug), read = tostring(read), exit = tostring(ok) .. ": " .. tostring(why) }
end }`})

		output, err := f.set.Tools(nil)[0].Run(context.Background(), tools.Env{}, json.RawMessage(`{}`))

		var result map[string]string
		json.Unmarshal([]byte(output), &result)
		log, _ := os.ReadFile(filepath.Join(f.logDir, "plugin-probe.log"))
		if err != nil || result["libs"] != "tabletablefunctiontabletable" || result["read"] != "nil" || !strings.Contains(result["exit"], "os.exit would stop mo") || !strings.Contains(string(log), "to the log") {
			t.Errorf("result = %v err = %v log = %q", result, err, log)
		}
	})

	t.Run("PL-11 mo.exec gives output and exit code; mo.read and mo.write use workdir", func(t *testing.T) {
		f := loadSource(t, map[string]string{"files": `
mo.tool{ name = "roundtrip", run = function()
  local ok = mo.write("notes/a.txt", "saved")
  local content = mo.read("notes/a.txt")
  local out, code = mo.exec("echo hi; exit 3")
  return { ok = ok, content = content, out = out, code = code, workdir = mo.workdir }
end }`})

		output, err := f.set.Tools(nil)[0].Run(context.Background(), tools.Env{}, json.RawMessage(`{}`))

		var result map[string]any
		json.Unmarshal([]byte(output), &result)
		if err != nil || result["ok"] != true || result["content"] != "saved" || result["out"] != "hi\n" || result["code"] != float64(3) || result["workdir"] != f.workDir {
			t.Errorf("result = %v err = %v", result, err)
		}
	})

	t.Run("PL-13 print goes to the plugin's log file, not the terminal", func(t *testing.T) {
		f := loadSource(t, map[string]string{"chatty": `print("loaded", 42)`})

		data, err := os.ReadFile(filepath.Join(f.logDir, "plugin-chatty.log"))

		if err != nil || !strings.Contains(string(data), "loaded 42") {
			t.Errorf("log = %q err = %v", data, err)
		}
	})
}

func TestRobustness(t *testing.T) {
	ctx := context.Background()

	t.Run("PL-14 a call waiting for a busy plugin gives up when its context ends", func(t *testing.T) {
		set := loadSource(t, map[string]string{"busy": `
mo.tool{ name = "slow", run = function() mo.exec("sleep 2") return "done" end }
mo.on("prompt", function(text) return text .. "!" end)`}).set
		go set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		time.Sleep(200 * time.Millisecond)
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		start := time.Now()

		prompt := set.RewritePrompt(waitCtx, "hi")

		if prompt != "hi" || time.Since(start) > time.Second {
			t.Errorf("prompt = %q after %s", prompt, time.Since(start))
		}
	})

	t.Run("PL-15 a call stuck inside Go is abandoned and the plugin turned off", func(t *testing.T) {
		set := loadSource(t, map[string]string{"pattern": `
mo.tool{ name = "match", run = function() return tostring(string.find(string.rep("a", 3000), ".-.-.-.-b")) end }`}).set
		tool := set.Tools(nil)[0]
		callCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		start := time.Now()

		_, err := tool.Run(callCtx, tools.Env{}, json.RawMessage(`{}`))
		_, again := tool.Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if err == nil || time.Since(start) > abandonGrace+2*time.Second || again == nil || set.Status()[0].Err == nil {
			t.Errorf("err = %v again = %v after %s", err, again, time.Since(start))
		}
	})

	t.Run("PL-16 a table that shares its subtables over and over is refused quickly", func(t *testing.T) {
		set := loadSource(t, map[string]string{"tree": `
mo.tool{ name = "tree", run = function() local t = {} for i = 1, 50 do t = { t, t } end return t end }`}).set
		start := time.Now()

		_, err := set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if err == nil || time.Since(start) > 2*time.Second {
			t.Errorf("err = %v after %s", err, time.Since(start))
		}
	})

	t.Run("PL-17 string.rep refuses results that would exhaust memory", func(t *testing.T) {
		set := loadSource(t, map[string]string{"rep": `
mo.tool{ name = "small", run = function() return string.rep("ab", 3, "-") end }
mo.tool{ name = "huge", run = function() return string.rep("x", 2^34) end }`}).set
		adapted := set.Tools(nil)

		small, smallErr := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		_, hugeErr := adapted[1].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if smallErr != nil || small != "ab-ab-ab" || hugeErr == nil || !strings.Contains(hugeErr.Error(), "MB") {
			t.Errorf("small = %q (%v) huge = %v", small, smallErr, hugeErr)
		}
	})

	t.Run("PL-18 mo.exec keeps head and tail of long output; background is fine", func(t *testing.T) {
		set := loadSource(t, map[string]string{"exec": `
mo.tool{ name = "long", run = function() local out = mo.exec("head -c 5000000 /dev/zero | tr '\\0' x") return tostring(#out) end }
mo.tool{ name = "bg", run = function() local out, code = mo.exec("sleep 3 & echo started") return out .. code end }`}).set
		adapted := set.Tools(nil)

		length, _ := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		background, err := adapted[1].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if length == "5000000" || !strings.HasPrefix(background, "started") || !strings.HasSuffix(background, "0") || err != nil {
			t.Errorf("length = %s background = %q err = %v", length, background, err)
		}
	})

	t.Run("PL-19 mo.tool, mo.command and mo.on fail after the plugin loaded", func(t *testing.T) {
		set := loadSource(t, map[string]string{"late": `
mo.tool{ name = "late", run = function() mo.on("prompt", function() end) return "added" end }`}).set

		_, err := set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if err == nil || !strings.Contains(err.Error(), "while the plugin loads") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("PL-20 a guard plugin that fails to load blocks every tool call", func(t *testing.T) {
		set := loadSource(t, map[string]string{"guard": `
mo.on("tool_call", function(call) return nil end)
error("broken config")`}).set

		reason := set.BlockToolCall(ctx, llm.ToolCall{Name: "ls", Args: json.RawMessage(`{}`)})

		if !strings.Contains(reason, "failed to load") {
			t.Errorf("reason = %q", reason)
		}
	})

	t.Run("PL-21 mo's own command names are refused; a repeated command is left out", func(t *testing.T) {
		reserved := loadSource(t, map[string]string{"own": `mo.command{ name = "settings", run = function() end }`}).set
		repeated := loadSource(t, map[string]string{
			"a": `mo.command{ name = "deploy", run = function() return "a" end }`,
			"b": `mo.command{ name = "deploy", run = function() return "b" end }`,
		}).set

		run, _ := repeated.Command("deploy")
		output, _ := run(ctx, "")

		if reserved.Status()[0].Err == nil || output != "a" || len(repeated.Status()[1].Warnings) != 1 {
			t.Errorf("reserved = %+v output = %q repeated = %+v", reserved.Status(), output, repeated.Status())
		}
	})

	t.Run("PL-22 adapting tools again does not repeat warnings", func(t *testing.T) {
		set := loadSource(t, map[string]string{"clash": `mo.tool{ name = "read", run = function() end }`}).set

		set.Tools([]string{"read"})
		set.Tools([]string{"read"})

		if warnings := set.Status()[0].Warnings; len(warnings) != 1 {
			t.Errorf("warnings = %v", warnings)
		}
	})

	t.Run("PL-23 a tool name must start with a letter or _", func(t *testing.T) {
		for name, valid := range map[string]bool{"dice-roll": true, "_x": true, "9lives": false, "-x": false, "a b": false} {
			if validName(name) != valid {
				t.Errorf("%q valid = %v", name, !valid)
			}
		}
	})
}

func TestExamplePlugin(t *testing.T) {
	t.Run("PL-24 the example plugin loads and its guard stops force pushes only", func(t *testing.T) {
		source, err := os.ReadFile("../../docs/examples/plugins/git.lua")
		if err != nil {
			t.Fatal(err)
		}
		set := loadSource(t, map[string]string{"git": string(source)}).set
		if status := set.Status()[0]; status.Err != nil {
			t.Fatal(status.Err)
		}
		cases := map[string]bool{
			"git push --force origin main": true,
			"git push -f origin master":    true,
			"git push origin main -f":      true,
			"git push origin main":         false,
			"git fetch -f":                 false,
		}

		for command, blocked := range cases {
			args, _ := json.Marshal(map[string]string{"command": command})
			reason := set.BlockToolCall(context.Background(), llm.ToolCall{Name: "bash", Args: args})
			if (reason != "") != blocked {
				t.Errorf("%q: reason = %q", command, reason)
			}
		}
	})
}

func TestBrokenGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("PL-25 a guard turned off at runtime keeps blocking tool calls", func(t *testing.T) {
		set := loadSource(t, map[string]string{"guard": `
mo.on("tool_call", function(call)
  if call.name == "slow" then string.find(string.rep("a", 3000), ".-.-.-.-b") end
end)`}).set
		stuckCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		set.BlockToolCall(stuckCtx, llm.ToolCall{Name: "slow", Args: json.RawMessage(`{}`)})

		reason := set.BlockToolCall(ctx, llm.ToolCall{Name: "bash", Args: json.RawMessage(`{"command":"ls"}`)})

		if !strings.Contains(reason, "turned off") {
			t.Errorf("reason = %q", reason)
		}
	})

	t.Run("PL-26 only a real mo.on tool_call makes a broken plugin a guard", func(t *testing.T) {
		set := loadSource(t, map[string]string{"notes": `
-- someday: watch tool_call events
error("not finished")`}).set

		if reason := set.BlockToolCall(ctx, llm.ToolCall{Name: "ls", Args: json.RawMessage(`{}`)}); reason != "" {
			t.Errorf("reason = %q", reason)
		}
	})
}

func TestErrorMessages(t *testing.T) {
	t.Run("PL-27 a plugin's error reaches the caller as its message, the stack trace in the log", func(t *testing.T) {
		f := loadSource(t, map[string]string{"fails": `
local function deep() error("the server said no", 0) end
mo.tool{ name = "fail", run = function() deep() end }`})

		_, err := f.set.Tools(nil)[0].Run(context.Background(), tools.Env{}, json.RawMessage(`{}`))

		log, _ := os.ReadFile(filepath.Join(f.logDir, "plugin-fails.log"))
		if err == nil || err.Error() != "plugin fails: the server said no" || !strings.Contains(string(log), "stack traceback") {
			t.Errorf("err = %v log = %q", err, log)
		}
	})
}
