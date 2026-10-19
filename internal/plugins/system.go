package plugins

import (
	"os"
	"path/filepath"
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/luabox"
)

// openLibraries gives the plugin every standard Lua library: it is the
// user's own code, so io, os, require and the rest are there. What would
// break mo's terminal is turned aside: what a plugin writes goes to its log,
// what it reads is empty, and os.exit and os.execute do not take mo down or
// draw over it.
func (p *Plugin) openLibraries() {
	state := p.state
	luabox.CapRepeat(state)
	p.setRequirePath()
	p.keepTerminalApart()
	os := state.GetGlobal("os").(*lua.LTable)
	os.RawSetString("exit", state.NewFunction(func(state *lua.LState) int {
		state.RaiseError("os.exit would stop mo: finish the function instead")
		return 0
	}))
	os.RawSetString("execute", state.NewFunction(p.osExecute))
}

// setRequirePath lets require find Lua modules in the lib folder beside the
// plugins, as lib/name.lua or lib/name/init.lua.
func (p *Plugin) setRequirePath() {
	pkg, ok := p.state.GetGlobal("package").(*lua.LTable)
	if !ok {
		return
	}
	pkg.RawSetString("path", lua.LString(filepath.Join(p.libDir, "?.lua")+";"+filepath.Join(p.libDir, "?", "init.lua")))
}

// keepTerminalApart makes io.write, io.stdout and io.stderr append to the
// plugin's log, and io.read and io.stdin read nothing.
func (p *Plugin) keepTerminalApart() {
	if err := os.MkdirAll(filepath.Dir(p.logPath), 0o700); err != nil {
		return
	}
	err := p.state.DoString(`
		local null = io.open("/dev/null", "r")
		io.input(null)
		io.stdin = null
		local log = io.open(` + luaQuote(p.logPath) + `, "a")
		io.output(log)
		io.stdout, io.stderr = log, log
	`)
	if err != nil {
		p.log("could not redirect io:", err.Error())
	}
}

func luaQuote(text string) string {
	return strconv.Quote(text)
}

// osExecute runs a command with no terminal, its output in the log, and
// gives its exit status, as os.execute does.
func (p *Plugin) osExecute(state *lua.LState) int {
	result := runProcess(state.Context(), commandSpec{command: state.CheckString(1), dir: p.workDir, timeout: execTimeout})
	p.log("os.execute:", state.CheckString(1), "→", result.stdout+result.stderr)
	state.Push(lua.LNumber(result.code))
	return 1
}
