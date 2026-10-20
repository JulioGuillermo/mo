package plugins

import (
	lua "github.com/yuin/gopher-lua"
)

// viewFile is mo.view(path): it shows a file, or a folder, in mo's viewer,
// as /view does; a relative path is in the working directory.
func (p *Plugin) viewFile(state *lua.LState) int {
	path := p.resolve(state.CheckString(1))
	p.surface.mu.Lock()
	view := p.surface.view
	p.surface.mu.Unlock()
	if view == nil {
		state.Push(lua.LFalse)
		return 1
	}
	view(path)
	state.Push(lua.LTrue)
	return 1
}

// OnView sets who shows the files plugins ask to view.
func (s *Set) OnView(show func(path string)) {
	if s == nil {
		return
	}
	s.surface.mu.Lock()
	defer s.surface.mu.Unlock()
	s.surface.view = show
}
