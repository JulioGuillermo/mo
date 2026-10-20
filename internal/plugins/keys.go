package plugins

import (
	"context"
	"fmt"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	eventKey = "key"
	// GlobalEventsKey, true in a plugin's settings, lets its mo.global.on
	// listeners hear what happens anywhere in mo.
	GlobalEventsKey = "global_events"
	// keyWait is how long a key waits for the plugins that listen to it
	// before it goes on as if none took it.
	keyWait = 50 * time.Millisecond
)

// neverHeard are the keys no plugin gets: those that close and stop.
var neverHeard = []string{"esc", "ctrl+c"}

// keyListener is a function listening to keys, those its patterns match,
// or every key without patterns.
type keyListener struct {
	patterns []string
	run      *lua.LFunction
}

// matches tells whether a listener hears a key: one of its patterns names
// it, or ends in +* after its modifiers, as ctrl+* for every ctrl+ key.
func (l keyListener) matches(key string) bool {
	if len(l.patterns) == 0 {
		return true
	}
	for _, pattern := range l.patterns {
		prefix, any := strings.CutSuffix(pattern, "*")
		if pattern == key || any && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// described is what a listener listens to, for the user to see.
func (l keyListener) described() string {
	if len(l.patterns) == 0 {
		return "every key"
	}
	return strings.Join(l.patterns, ", ")
}

// declareKey reads mo.on("key", [patterns], fn) or mo.global.on of the
// same: patterns are a key, as mo names them, or a list of keys; a key
// may end in +* for every key with those modifiers.
func declareKey(state *lua.LState, function string) keyListener {
	var listener keyListener
	at := 2
	switch patterns := state.Get(2).(type) {
	case lua.LString:
		listener.patterns, at = []string{string(patterns)}, 3
	case *lua.LTable:
		for i := 1; i <= patterns.Len(); i++ {
			listener.patterns = append(listener.patterns, lua.LVAsString(patterns.RawGetInt(i)))
		}
		at = 3
	}
	for _, pattern := range listener.patterns {
		if err := validKeyPattern(pattern); err != nil {
			state.RaiseError("%s(\"key\"): %v", function, err)
		}
	}
	listener.run = state.CheckFunction(at)
	return listener
}

func validKeyPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("an empty key")
	}
	before, any := strings.CutSuffix(pattern, "+*")
	if strings.Contains(before, "*") || !any && strings.Contains(pattern, "*") || any && before == "" {
		return fmt.Errorf("key %q: * only ends a key after its modifiers, as ctrl+*", pattern)
	}
	return nil
}

// globalTable is mo.global: on, which listens anywhere in mo once the user
// allows the plugin global events.
func (p *Plugin) globalTable() *lua.LTable {
	table := p.state.NewTable()
	table.RawSetString("on", p.state.NewFunction(p.declareGlobal))
	return table
}

func (p *Plugin) declareGlobal(state *lua.LState) int {
	p.mustBeLoading(state, "mo.global.on")
	if event := state.CheckString(1); event != eventKey {
		state.RaiseError("mo.global.on: unknown event %q (use key)", event)
	}
	p.globalKeys = append(p.globalKeys, declareKey(state, "mo.global.on"))
	return 0
}

// hearsGlobally tells whether the user lets the plugin hear what happens
// anywhere in mo.
func (p *Plugin) hearsGlobally() bool {
	allowed, _ := p.values[GlobalEventsKey].(bool)
	return allowed
}

// GlobalEvents tells whether some plugin asks to hear keys anywhere in mo
// and whether some hears them: it asked and the user allowed it.
func (s *Set) GlobalEvents() (asked, heard bool) {
	for _, plugin := range s.loaded() {
		if len(plugin.globalKeys) == 0 {
			continue
		}
		if plugin.hearsGlobally() {
			heard = true
		} else {
			asked = true
		}
	}
	return asked, heard
}

// Key tells the plugins listening of a key: first those of focused, the
// plugin the user works in now, then those allowed to hear keys anywhere.
// It tells whether one took the key, true from its function, within
// keyWait; a key none took in time goes on, as esc and ctrl+c always do.
func (s *Set) Key(focused, key, text string) bool {
	for _, never := range neverHeard {
		if key == never {
			return false
		}
	}
	type heard struct {
		plugin   *Plugin
		listener keyListener
	}
	var listening []heard
	for _, plugin := range s.loaded() {
		if plugin.name != focused {
			continue
		}
		for _, listener := range plugin.keys {
			listening = append(listening, heard{plugin, listener})
		}
	}
	for _, plugin := range s.loaded() {
		if !plugin.hearsGlobally() {
			continue
		}
		for _, listener := range plugin.globalKeys {
			listening = append(listening, heard{plugin, listener})
		}
	}
	var matching []heard
	for _, each := range listening {
		if each.listener.matches(key) {
			matching = append(matching, each)
		}
	}
	if len(matching) == 0 {
		return false
	}
	taken := make(chan bool, 1)
	go func() {
		for _, each := range matching {
			result, err := each.plugin.callFunction(context.Background(), keyWait, each.listener.run, func(value lua.LValue) (any, error) {
				return lua.LVAsBool(value), nil
			}, func(state *lua.LState) lua.LValue {
				event := state.NewTable()
				event.RawSetString("key", lua.LString(key))
				event.RawSetString("text", lua.LString(text))
				return event
			})
			if err != nil {
				each.plugin.log("key listener:", err.Error())
			}
			if took, _ := result.(bool); took {
				taken <- true
				return
			}
		}
		taken <- false
	}()
	select {
	case took := <-taken:
		return took
	case <-time.After(keyWait):
		return false
	}
}
