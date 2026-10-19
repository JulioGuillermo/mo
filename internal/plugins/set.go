package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const (
	hookPrompt     = "prompt"
	hookToolCall   = "tool_call"
	hookToolResult = "tool_result"
	hookTurnEnd    = "turn_end"
	hookStream     = "stream"

	maxCommandOutput = 64 << 10
	maxPromptBytes   = 256 << 10
	closeWait        = 2 * time.Second
)

type Set struct {
	plugins []*Plugin
	surface *surface
	feed    *streamFeed

	toolsOnce sync.Once
	tools     []tools.Tool
}

type Status struct {
	Name     string
	Tools    []string
	Commands []string
	Hooks    []string
	Err      error
	Warnings []string
}

// Load runs every .lua file in dir, in name order, with the Lua modules of
// the lib folder beside it to require. A missing dir is no plugins. A
// command name already taken by an earlier plugin is left out with a
// warning.
func Load(dir, workDir, logDir string) *Set {
	set := &Set{surface: newSurface()}
	libDir := filepath.Join(filepath.Dir(dir), "lib")
	paths, _ := filepath.Glob(filepath.Join(dir, "*.lua"))
	slices.Sort(paths)
	commands := map[string]bool{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		plugin := load(path, workDir, logDir, libDir, set.surface)
		plugin.commands = slices.DeleteFunc(plugin.commands, func(command commandDefinition) bool {
			if commands[command.name] {
				plugin.warnings = append(plugin.warnings, "command /"+command.name+" left out: the name is taken")
				return true
			}
			commands[command.name] = true
			return false
		})
		set.plugins = append(set.plugins, plugin)
	}
	set.streamHooks()
	return set
}

// Tools adapts the plugins' tools, once per run. One whose name is in taken,
// or another plugin already has, is left out with a warning.
func (s *Set) Tools(taken []string) []tools.Tool {
	if s == nil {
		return nil
	}
	s.toolsOnce.Do(func() {
		taken = slices.Clone(taken)
		for _, plugin := range s.loaded() {
			for _, definition := range plugin.tools {
				if slices.Contains(taken, definition.name) {
					plugin.warnings = append(plugin.warnings, "tool "+definition.name+" left out: the name is taken")
					continue
				}
				taken = append(taken, definition.name)
				s.tools = append(s.tools, plugin.adapt(definition))
			}
		}
	})
	return s.tools
}

// ToolNames lists the plugin tools the session offers.
func (s *Set) ToolNames() []string {
	var names []string
	for _, tool := range s.tools {
		names = append(names, tool.Name)
	}
	return names
}

func (p *Plugin) adapt(definition toolDefinition) tools.Tool {
	return tools.Tool{
		Name:        definition.name,
		Description: definition.description,
		Schema:      definition.schema,
		Run: func(ctx context.Context, _ tools.Env, raw json.RawMessage) (string, error) {
			var args any
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			result, err := p.callFunction(ctx, runTimeout, definition.run, resultText, func(state *lua.LState) lua.LValue {
				return toLua(state, args)
			})
			if err != nil {
				return "", err
			}
			return result.(string), nil
		},
		Summary: func(raw json.RawMessage) string { return string(raw) },
	}
}

// resultText takes a string as it is, a table as JSON, and nothing as no
// output.
func resultText(result lua.LValue) (any, error) {
	switch result := result.(type) {
	case lua.LString:
		return string(result), nil
	case *lua.LTable:
		value, err := toGo(result)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(value)
		return string(encoded), err
	case *lua.LNilType:
		return "(no output)", nil
	}
	return result.String(), nil
}

// NeedConfirmation names the plugin tools declared with confirm = true.
func (s *Set) NeedConfirmation() []string {
	var names []string
	for _, plugin := range s.loaded() {
		for _, definition := range plugin.tools {
			if definition.confirm {
				names = append(names, definition.name)
			}
		}
	}
	return names
}

type CommandInfo struct {
	Name        string
	Description string
}

// Commands lists the plugin commands, in the order the plugins declared them.
func (s *Set) Commands() []CommandInfo {
	var commands []CommandInfo
	for _, plugin := range s.loaded() {
		for _, definition := range plugin.commands {
			commands = append(commands, CommandInfo{Name: definition.name, Description: definition.description})
		}
	}
	return commands
}

// Command finds a plugin command by name. Its output is cut to
// maxCommandOutput bytes.
func (s *Set) Command(name string) (func(ctx context.Context, text string) (string, error), bool) {
	for _, plugin := range s.loaded() {
		for _, definition := range plugin.commands {
			if definition.name != name {
				continue
			}
			return func(ctx context.Context, text string) (string, error) {
				result, err := plugin.callFunction(ctx, runTimeout, definition.run, resultText, func(*lua.LState) lua.LValue {
					return lua.LString(text)
				})
				if err != nil {
					return "", err
				}
				return tools.Truncate(result.(string), maxCommandOutput), nil
			}, true
		}
	}
	return nil, false
}

// RewritePrompt passes the prompt through every prompt hook in turn. A hook
// that returns nothing, fails, or returns more than maxPromptBytes leaves it
// as it was.
func (s *Set) RewritePrompt(ctx context.Context, prompt string) string {
	for _, plugin := range s.loaded() {
		for _, hook := range plugin.hooks[hookPrompt] {
			result, err := plugin.callFunction(ctx, hookTimeout, hook, optionalText, func(*lua.LState) lua.LValue {
				return lua.LString(prompt)
			})
			text, _ := result.(string)
			switch {
			case err != nil:
				plugin.log("prompt hook:", err.Error())
			case len(text) > maxPromptBytes:
				plugin.log(fmt.Sprintf("prompt hook: result of %d bytes ignored", len(text)))
			case text != "":
				prompt = text
			}
		}
	}
	return prompt
}

// BlockToolCall asks every tool_call hook about a call and returns the
// reason the first one gives to block it. A hook that fails blocks the call,
// and so does a plugin with a tool_call hook that failed to load or was
// turned off: a guard that broke should not let everything through.
func (s *Set) BlockToolCall(ctx context.Context, call llm.ToolCall) string {
	if s == nil {
		return ""
	}
	for _, plugin := range s.plugins {
		if !plugin.guardsToolCalls() {
			continue
		}
		if plugin.loadErr != nil {
			return fmt.Sprintf("blocked: plugin %s guards tool calls but failed to load: %v", plugin.name, plugin.loadErr)
		}
		if plugin.stuck.Load() {
			return fmt.Sprintf("blocked: plugin %s guards tool calls but was turned off", plugin.name)
		}
	}
	var args any
	json.Unmarshal(call.Args, &args)
	for _, plugin := range s.loaded() {
		for _, hook := range plugin.hooks[hookToolCall] {
			result, err := plugin.callFunction(ctx, hookTimeout, hook, optionalText, func(state *lua.LState) lua.LValue {
				event := state.NewTable()
				event.RawSetString("name", lua.LString(call.Name))
				event.RawSetString("args", toLua(state, args))
				return event
			})
			if err != nil {
				return fmt.Sprintf("blocked: the tool_call hook of plugin %s failed: %v", plugin.name, err)
			}
			if reason, _ := result.(string); reason != "" {
				return "blocked by plugin " + plugin.name + ": " + reason
			}
		}
	}
	return ""
}

func optionalText(result lua.LValue) (any, error) {
	if text, ok := result.(lua.LString); ok {
		return string(text), nil
	}
	return "", nil
}

func (s *Set) Status() []Status {
	if s == nil {
		return nil
	}
	var statuses []Status
	for _, plugin := range s.plugins {
		status := Status{Name: plugin.name, Err: plugin.loadErr, Warnings: slices.Clone(plugin.warnings)}
		if plugin.stuck.Load() {
			status.Err = errPluginStuck
		}
		for _, definition := range plugin.tools {
			status.Tools = append(status.Tools, definition.name)
		}
		for _, definition := range plugin.commands {
			status.Commands = append(status.Commands, "/"+definition.name)
		}
		for event, hooks := range plugin.hooks {
			status.Hooks = append(status.Hooks, fmt.Sprintf("%s×%d", event, len(hooks)))
		}
		slices.Sort(status.Hooks)
		statuses = append(statuses, status)
	}
	return statuses
}

// Close frees the Lua states that are free within closeWait; a plugin still
// busy is left to end with the process.
func (s *Set) Close() {
	if s == nil {
		return
	}
	for _, plugin := range s.plugins {
		select {
		case <-plugin.turn:
			plugin.state.Close()
		case <-time.After(closeWait):
		}
	}
}

// loaded is the plugins whose file ran without error and that still answer.
func (s *Set) loaded() []*Plugin {
	if s == nil {
		return nil
	}
	var loaded []*Plugin
	for _, plugin := range s.plugins {
		if plugin.loadErr == nil && !plugin.stuck.Load() {
			loaded = append(loaded, plugin)
		}
	}
	return loaded
}
