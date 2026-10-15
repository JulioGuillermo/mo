package plugins

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/luabox"
)

func toGo(value lua.LValue) (any, error) { return luabox.ToGo(value) }

func toLua(state *lua.LState, value any) lua.LValue { return luabox.ToLua(state, value) }
