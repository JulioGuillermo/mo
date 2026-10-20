package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/proctree"
	"github.com/jg/mo/internal/tools"
)

const (
	execTimeout    = 2 * time.Minute
	execKeptOutput = 1 << 20
)

// commandSpec is a process to run: a shell command, or argv without a shell.
type commandSpec struct {
	command string
	argv    []string
	dir     string
	env     map[string]string
	stdin   string
	timeout time.Duration
	// merged sends the error output to the same place as the output.
	merged bool
}

type commandResult struct {
	stdout, stderr string
	code           int
}

// runProcess runs a command, in its own session so a timeout takes its
// children with it, keeping the start and end of what it prints. Background
// processes it starts may keep running, as with the bash tool.
func runProcess(ctx context.Context, spec commandSpec) commandResult {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, spec.timeout)
	defer cancel()
	var cmd *exec.Cmd
	if len(spec.argv) > 0 {
		cmd = exec.CommandContext(ctx, spec.argv[0], spec.argv[1:]...)
	} else {
		cmd = tools.CurrentShell().Command(ctx, spec.command)
	}
	cmd.Dir = spec.dir
	if len(spec.env) > 0 {
		cmd.Env = os.Environ()
		for name, value := range spec.env {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	if spec.stdin != "" {
		cmd.Stdin = strings.NewReader(spec.stdin)
	}
	proctree.Detach(cmd)
	proctree.KillOnCancel(cmd)
	cmd.WaitDelay = time.Second
	stdout := tools.NewHeadTailBuffer(execKeptOutput)
	stderr := stdout
	if !spec.merged {
		stderr = tools.NewHeadTailBuffer(execKeptOutput)
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	result := commandResult{stdout: stdout.String()}
	if !spec.merged {
		result.stderr = stderr.String()
	}
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrWaitDelay):
	case errors.As(err, &exitErr):
		result.code = exitErr.ExitCode()
	case err != nil:
		result.code = -1
		result.stderr += err.Error()
		if spec.merged {
			result.stdout += err.Error()
			result.stderr = ""
		}
	}
	return result
}

// exec runs a bash command in the working directory and returns its output,
// error output included, and its exit code.
func (p *Plugin) exec(state *lua.LState) int {
	result := runProcess(state.Context(), commandSpec{command: state.CheckString(1), dir: p.workDir, timeout: execTimeout, merged: true})
	state.Push(lua.LString(result.stdout))
	state.Push(lua.LNumber(result.code))
	return 2
}

// run is exec with what a command may need: mo.run{ command = "..." } for a
// shell, or mo.run{ argv = {"git", "log"} } for none; cwd, env (a table
// added to the environment), stdin and timeout (seconds). It returns a table
// with stdout, stderr, code and ok.
func (p *Plugin) run(state *lua.LState) int {
	options := state.CheckTable(1)
	spec := commandSpec{dir: p.workDir, timeout: execTimeout}
	spec.command = luaString(options.RawGetString("command"))
	if argv, ok := options.RawGetString("argv").(*lua.LTable); ok {
		argv.ForEach(func(_, value lua.LValue) { spec.argv = append(spec.argv, value.String()) })
	}
	if spec.command == "" && len(spec.argv) == 0 {
		state.RaiseError("mo.run needs a command or an argv list")
	}
	if cwd := luaString(options.RawGetString("cwd")); cwd != "" {
		spec.dir = p.resolve(cwd)
	}
	spec.stdin = luaString(options.RawGetString("stdin"))
	if seconds, ok := options.RawGetString("timeout").(lua.LNumber); ok && seconds > 0 {
		spec.timeout = time.Duration(float64(seconds) * float64(time.Second))
	}
	if env, ok := options.RawGetString("env").(*lua.LTable); ok {
		spec.env = map[string]string{}
		env.ForEach(func(name, value lua.LValue) { spec.env[name.String()] = value.String() })
	}
	result := runProcess(state.Context(), spec)
	table := state.NewTable()
	table.RawSetString("stdout", lua.LString(result.stdout))
	table.RawSetString("stderr", lua.LString(result.stderr))
	table.RawSetString("code", lua.LNumber(result.code))
	table.RawSetString("ok", lua.LBool(result.code == 0))
	state.Push(table)
	return 1
}

// sleep waits for some seconds, or until the call is stopped.
func sleep(state *lua.LState) int {
	wait := time.Duration(float64(state.CheckNumber(1)) * float64(time.Second))
	ctx := state.Context()
	if ctx == nil {
		time.Sleep(wait)
		return 0
	}
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		state.RaiseError("sleep stopped: %v", ctx.Err())
	}
	return 0
}
