package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"

	lua "github.com/yuin/gopher-lua"
)

// openAPI installs the mo table: what a plugin declares (tool, command, on,
// setting, component), its settings, its dialogs and view, and what it may
// use besides Lua's own libraries: exec and run for commands, http, fs, read
// and write for files, json, encodings, sleep, env, log and workdir.
func (p *Plugin) openAPI() {
	state := p.state
	api := state.NewTable()
	functions := map[string]lua.LGFunction{
		"tool":        p.declareTool,
		"command":     p.declareCommand,
		"on":          p.declareHook,
		"setting":     p.declareSetting,
		"component":   p.declareComponent,
		"view":        p.viewFile,
		"screen":      p.screenSize,
		"style":       styleText,
		"width":       textWidth,
		"exec":        p.exec,
		"run":         p.run,
		"sleep":       sleep,
		"read":        p.read,
		"write":       p.write,
		"json_encode": jsonEncode,
		"json_decode": jsonDecode,
		"env":         env,
		"log":         p.logLine,
	}
	for name, function := range functions {
		api.RawSetString(name, state.NewFunction(function))
	}
	api.RawSetString("http", p.httpTable())
	api.RawSetString("fs", p.fsTable())
	api.RawSetString("sidebar", p.sidebarTable())
	api.RawSetString("dialog", state.NewFunction(p.openDialog))
	api.RawSetString("send", state.NewFunction(p.sendToModel))
	api.RawSetString("settings", p.settingsTable())
	openEncodings(state, api)
	api.RawSetString("workdir", lua.LString(p.workDir))
	state.SetGlobal("mo", api)
	state.SetGlobal("print", state.NewFunction(p.logLine))
}

func (p *Plugin) read(state *lua.LState) int {
	data, err := os.ReadFile(p.resolve(state.CheckString(1)))
	if err != nil {
		state.Push(lua.LNil)
		state.Push(lua.LString(err.Error()))
		return 2
	}
	state.Push(lua.LString(data))
	return 1
}

func (p *Plugin) write(state *lua.LState) int {
	path := p.resolve(state.CheckString(1))
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err == nil {
		err = os.WriteFile(path, []byte(state.CheckString(2)), 0o644)
	}
	if err != nil {
		state.Push(lua.LFalse)
		state.Push(lua.LString(err.Error()))
		return 2
	}
	state.Push(lua.LTrue)
	return 1
}

func (p *Plugin) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(p.workDir, path)
}

func jsonEncode(state *lua.LState) int {
	value, err := toGo(state.CheckAny(1))
	if err != nil {
		state.RaiseError("json_encode: %v", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		state.RaiseError("json_encode: %v", err)
	}
	state.Push(lua.LString(encoded))
	return 1
}

func jsonDecode(state *lua.LState) int {
	var value any
	if err := json.Unmarshal([]byte(state.CheckString(1)), &value); err != nil {
		state.Push(lua.LNil)
		state.Push(lua.LString(err.Error()))
		return 2
	}
	state.Push(toLua(state, value))
	return 1
}

func env(state *lua.LState) int {
	state.Push(lua.LString(os.Getenv(state.CheckString(1))))
	return 1
}

func (p *Plugin) logLine(state *lua.LState) int {
	var parts []string
	for i := 1; i <= state.GetTop(); i++ {
		parts = append(parts, state.Get(i).String())
	}
	p.log(parts...)
	return 0
}
