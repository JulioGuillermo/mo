package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/tools"
)

const (
	execTimeout    = 2 * time.Minute
	execKeptOutput = 1 << 20
)

// openAPI installs the mo table: what a plugin declares (tool, command, on)
// and what it may use (exec, read, write, json, env, log, workdir).
func (p *Plugin) openAPI() {
	state := p.state
	api := state.NewTable()
	functions := map[string]lua.LGFunction{
		"tool":        p.declareTool,
		"command":     p.declareCommand,
		"on":          p.declareHook,
		"exec":        p.exec,
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
	api.RawSetString("workdir", lua.LString(p.workDir))
	state.SetGlobal("mo", api)
	state.SetGlobal("print", state.NewFunction(p.logLine))
}

// exec runs a bash command in the working directory and returns its output,
// cut to its first and last bytes when long, and its exit code. Background
// processes it starts may keep running, as with the bash tool.
func (p *Plugin) exec(state *lua.LState) int {
	ctx, cancel := context.WithTimeout(state.Context(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", state.CheckString(1))
	cmd.Dir = p.workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	output := tools.NewHeadTailBuffer(execKeptOutput)
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	text := output.String()
	code := 0
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrWaitDelay):
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		code = -1
		text += err.Error()
	}
	state.Push(lua.LString(text))
	state.Push(lua.LNumber(code))
	return 2
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
