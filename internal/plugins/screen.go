package plugins

import (
	lua "github.com/yuin/gopher-lua"
)

// SetScreen tells the plugins the size of the terminal, as it changes.
func (s *Set) SetScreen(width, height int) {
	if s == nil {
		return
	}
	s.surface.mu.Lock()
	defer s.surface.mu.Unlock()
	s.surface.screen = [2]int{width, height}
}

// screenSize is mo.screen(): { width, height } of the terminal, or nil
// when mo has no screen, as when it answers once.
func (p *Plugin) screenSize(state *lua.LState) int {
	p.surface.mu.Lock()
	size := p.surface.screen
	p.surface.mu.Unlock()
	if size[0] == 0 {
		state.Push(lua.LNil)
		return 1
	}
	table := state.NewTable()
	table.RawSetString("width", lua.LNumber(size[0]))
	table.RawSetString("height", lua.LNumber(size[1]))
	state.Push(table)
	return 1
}

// dialogBounds is a dialog's bounds(): { x, y, width, height } where it is
// drawn now, or nil while it is not on screen.
func (p *Plugin) dialogBounds(id int) lua.LGFunction {
	return func(state *lua.LState) int {
		bounds, shown := p.surface.dialogs.Bounds(id)
		if !shown {
			state.Push(lua.LNil)
			return 1
		}
		table := state.NewTable()
		table.RawSetString("x", lua.LNumber(bounds.X))
		table.RawSetString("y", lua.LNumber(bounds.Y))
		table.RawSetString("width", lua.LNumber(bounds.Width))
		table.RawSetString("height", lua.LNumber(bounds.Height))
		state.Push(table)
		return 1
	}
}
