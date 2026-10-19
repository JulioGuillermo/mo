package plugins

import (
	"os"
	"path/filepath"
	"sort"

	lua "github.com/yuin/gopher-lua"
)

// fsTable is mo.fs: what Lua's io lacks of the file system. Relative paths
// are taken from the working directory.
func (p *Plugin) fsTable() *lua.LTable {
	functions := map[string]lua.LGFunction{
		"list":   p.fsList,
		"stat":   p.fsStat,
		"mkdir":  p.fsMkdir,
		"remove": p.fsRemove,
		"rename": p.fsRename,
		"glob":   p.fsGlob,
	}
	table := p.state.NewTable()
	for name, function := range functions {
		table.RawSetString(name, p.state.NewFunction(function))
	}
	return table
}

// failed answers a call that did not work: nil and the reason.
func failed(state *lua.LState, err error) int {
	state.Push(lua.LNil)
	state.Push(lua.LString(err.Error()))
	return 2
}

// succeeded answers a call that did: true.
func succeeded(state *lua.LState) int {
	state.Push(lua.LTrue)
	return 1
}

// fsList gives the entries of a folder, sorted, each {name, is_dir, size}.
func (p *Plugin) fsList(state *lua.LState) int {
	entries, err := os.ReadDir(p.resolve(state.CheckString(1)))
	if err != nil {
		return failed(state, err)
	}
	list := state.NewTable()
	for _, entry := range entries {
		item := state.NewTable()
		item.RawSetString("name", lua.LString(entry.Name()))
		item.RawSetString("is_dir", lua.LBool(entry.IsDir()))
		if info, err := entry.Info(); err == nil {
			item.RawSetString("size", lua.LNumber(info.Size()))
		}
		list.Append(item)
	}
	state.Push(list)
	return 1
}

// fsStat gives {size, is_dir, mode, mtime} of a path, mtime in Unix seconds.
func (p *Plugin) fsStat(state *lua.LState) int {
	info, err := os.Stat(p.resolve(state.CheckString(1)))
	if err != nil {
		return failed(state, err)
	}
	table := state.NewTable()
	table.RawSetString("size", lua.LNumber(info.Size()))
	table.RawSetString("is_dir", lua.LBool(info.IsDir()))
	table.RawSetString("mode", lua.LNumber(info.Mode().Perm()))
	table.RawSetString("mtime", lua.LNumber(info.ModTime().Unix()))
	state.Push(table)
	return 1
}

func (p *Plugin) fsMkdir(state *lua.LState) int {
	if err := os.MkdirAll(p.resolve(state.CheckString(1)), 0o755); err != nil {
		return failed(state, err)
	}
	return succeeded(state)
}

// fsRemove deletes a file or an empty folder; with true as the second
// argument, a folder and all in it.
func (p *Plugin) fsRemove(state *lua.LState) int {
	path := p.resolve(state.CheckString(1))
	remove := os.Remove
	if state.OptBool(2, false) {
		remove = os.RemoveAll
	}
	if err := remove(path); err != nil {
		return failed(state, err)
	}
	return succeeded(state)
}

func (p *Plugin) fsRename(state *lua.LState) int {
	if err := os.Rename(p.resolve(state.CheckString(1)), p.resolve(state.CheckString(2))); err != nil {
		return failed(state, err)
	}
	return succeeded(state)
}

// fsGlob lists the paths a pattern matches, sorted.
func (p *Plugin) fsGlob(state *lua.LState) int {
	matches, err := filepath.Glob(p.resolve(state.CheckString(1)))
	if err != nil {
		return failed(state, err)
	}
	sort.Strings(matches)
	list := state.NewTable()
	for _, match := range matches {
		list.Append(lua.LString(match))
	}
	state.Push(list)
	return 1
}
