// Package plugins runs the user's Lua plugins: tools for the model, slash
// commands and hooks on prompts and tool calls.
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	loadTimeout   = 10 * time.Second
	hookTimeout   = 5 * time.Second
	runTimeout    = 2 * time.Minute
	abandonGrace  = 3 * time.Second
	maxNameLength = 48
)

var reservedCommands = []string{"new", "clear", "compact", "settings", "providers", "mcp", "plugins", "agents", "cache", "view", "help", "quit", "exit"}

var hookEvents = []string{hookPrompt, hookToolCall, hookToolResult, hookTurnEnd, hookStream}

var (
	errPluginStuck = errors.New("stopped responding and was turned off")
	toolCallHook   = regexp.MustCompile(`mo\.on\s*\(\s*["']tool_call["']`)
)

type Plugin struct {
	name    string
	workDir string
	libDir  string
	logPath string
	surface *surface
	source  string

	// turn holds one token: whoever has it may use the Lua state.
	turn     chan struct{}
	stuck    atomic.Bool
	loading  bool
	state    *lua.LState
	tools    []toolDefinition
	commands []commandDefinition
	hooks    map[string][]*lua.LFunction
	loadErr  error
	warnings []string
}

type toolDefinition struct {
	name        string
	description string
	schema      json.RawMessage
	confirm     bool
	run         *lua.LFunction
}

type commandDefinition struct {
	name        string
	description string
	run         *lua.LFunction
}

// load runs a plugin file, which declares its tools, commands and hooks
// through the mo table. A file that fails keeps its error for Status and
// offers nothing.
func load(path, workDir, logDir, libDir string, surface *surface) *Plugin {
	name := strings.TrimSuffix(filepath.Base(path), ".lua")
	source, _ := os.ReadFile(path)
	plugin := &Plugin{
		name:    name,
		workDir: workDir,
		libDir:  libDir,
		surface: surface,
		logPath: filepath.Join(logDir, "plugin-"+name+".log"),
		source:  string(source),
		turn:    make(chan struct{}, 1),
		hooks:   map[string][]*lua.LFunction{},
		state:   lua.NewState(),
		loading: true,
	}
	plugin.turn <- struct{}{}
	plugin.openLibraries()
	plugin.openAPI()
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	_, plugin.loadErr = plugin.call(ctx, loadTimeout, func(state *lua.LState) (any, error) {
		return nil, state.DoString(string(source))
	})
	plugin.loading = false
	return plugin
}

// call runs work on the plugin's Lua state within timeout, waiting for its
// turn as long as ctx allows. Lua code is interrupted at the time limit;
// work stuck inside Go, such as a runaway pattern match, is abandoned after
// a grace period and turns the plugin off for the rest of the run.
func (p *Plugin) call(ctx context.Context, timeout time.Duration, work func(*lua.LState) (any, error)) (any, error) {
	if p.stuck.Load() {
		return nil, fmt.Errorf("plugin %s %w", p.name, errPluginStuck)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-p.turn:
	case <-ctx.Done():
		return nil, fmt.Errorf("plugin %s is busy: %w", p.name, ctx.Err())
	}
	type outcome struct {
		value any
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() { p.turn <- struct{}{} }()
		defer func() {
			if panicked := recover(); panicked != nil {
				done <- outcome{err: fmt.Errorf("panic: %v", panicked)}
			}
		}()
		p.state.SetContext(ctx)
		value, err := work(p.state)
		p.state.RemoveContext()
		done <- outcome{value, err}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.name, p.readable(result.err))
		}
		return result.value, nil
	case <-ctx.Done():
	}
	select {
	case result := <-done:
		if result.err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.name, ctx.Err())
		}
		return result.value, nil
	case <-time.After(abandonGrace):
		p.stuck.Store(true)
		p.log("turned off: a call did not stop at its time limit")
		return nil, fmt.Errorf("plugin %s %w", p.name, errPluginStuck)
	}
}

// readable is the message of a Lua error without its stack trace, which goes
// to the plugin's log: whoever reads the error has no use for it.
func (p *Plugin) readable(err error) error {
	var failure *lua.ApiError
	if !errors.As(err, &failure) || failure.StackTrace == "" {
		return err
	}
	p.log("error:", err.Error())
	return errors.New(failure.Object.String())
}

// callFunction calls a Lua function and converts its first result with
// convert, both within the plugin's turn.
func (p *Plugin) callFunction(ctx context.Context, timeout time.Duration, function *lua.LFunction, convert func(lua.LValue) (any, error), args ...func(*lua.LState) lua.LValue) (any, error) {
	return p.call(ctx, timeout, func(state *lua.LState) (any, error) {
		values := make([]lua.LValue, 0, len(args))
		for _, arg := range args {
			values = append(values, arg(state))
		}
		if err := state.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true}, values...); err != nil {
			return nil, err
		}
		result := state.Get(-1)
		state.Pop(1)
		return convert(result)
	})
}

func (p *Plugin) declareTool(state *lua.LState) int {
	p.mustBeLoading(state, "mo.tool")
	spec := state.CheckTable(1)
	name := spec.RawGetString("name").String()
	run, isFunction := spec.RawGetString("run").(*lua.LFunction)
	if !validName(name) || !isFunction {
		state.RaiseError("mo.tool needs a name (a letter or _, then letters, digits, _ or -) and a run function")
	}
	schema, err := toolSchema(spec)
	if err != nil {
		state.RaiseError("mo.tool %s: %v", name, err)
	}
	p.tools = append(p.tools, toolDefinition{
		name:        name,
		description: luaString(spec.RawGetString("description")),
		schema:      schema,
		confirm:     lua.LVAsBool(spec.RawGetString("confirm")),
		run:         run,
	})
	return 0
}

// toolSchema builds the JSON schema from params, a table of properties, and
// required, a list of their names.
func toolSchema(spec *lua.LTable) (json.RawMessage, error) {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	if params, ok := spec.RawGetString("params").(*lua.LTable); ok {
		properties, err := toGo(params)
		if err != nil {
			return nil, err
		}
		if fields, isMap := properties.(map[string]any); isMap {
			schema["properties"] = fields
		}
	}
	if required, ok := spec.RawGetString("required").(*lua.LTable); ok {
		names, err := toGo(required)
		if err != nil {
			return nil, err
		}
		schema["required"] = names
	}
	return json.Marshal(schema)
}

func (p *Plugin) declareCommand(state *lua.LState) int {
	p.mustBeLoading(state, "mo.command")
	spec := state.CheckTable(1)
	name := spec.RawGetString("name").String()
	run, isFunction := spec.RawGetString("run").(*lua.LFunction)
	if !validName(name) || !isFunction {
		state.RaiseError("mo.command needs a name (a letter or _, then letters, digits, _ or -) and a run function")
	}
	if slices.Contains(reservedCommands, name) {
		state.RaiseError("mo.command: /%s is one of mo's own commands", name)
	}
	p.commands = append(p.commands, commandDefinition{name: name, description: luaString(spec.RawGetString("description")), run: run})
	return 0
}

func (p *Plugin) declareHook(state *lua.LState) int {
	p.mustBeLoading(state, "mo.on")
	event := state.CheckString(1)
	if !slices.Contains(hookEvents, event) {
		state.RaiseError("mo.on: unknown event %q (use %s)", event, strings.Join(hookEvents, ", "))
	}
	p.hooks[event] = append(p.hooks[event], state.CheckFunction(2))
	return 0
}

// mustBeLoading allows declarations only while the file loads: afterwards
// other goroutines read what was declared.
func (p *Plugin) mustBeLoading(state *lua.LState, function string) {
	if !p.loading {
		state.RaiseError("%s can only be called while the plugin loads", function)
	}
}

// guardsToolCalls tells whether the plugin declares, or meant to declare, a
// tool_call hook.
func (p *Plugin) guardsToolCalls() bool {
	return len(p.hooks[hookToolCall]) > 0 || toolCallHook.MatchString(p.source)
}

func (p *Plugin) log(parts ...string) {
	if err := os.MkdirAll(filepath.Dir(p.logPath), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), strings.Join(parts, " "))
}

// validName accepts what every provider takes as a tool name: a letter or
// underscore, then letters, digits, underscores or hyphens.
func validName(name string) bool {
	if name == "" || len(name) > maxNameLength {
		return false
	}
	for i, char := range name {
		isLetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '_'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && (i == 0 || (!isDigit && char != '-')) {
			return false
		}
	}
	return true
}

func luaString(value lua.LValue) string {
	if value == lua.LNil {
		return ""
	}
	return value.String()
}
