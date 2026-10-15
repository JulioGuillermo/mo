// Package luabox opens Lua states that reach nothing outside them, and
// passes plain values in and out of them.
package luabox

import (
	"fmt"
	"math"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const (
	maxConvertDepth = 64
	maxConvertNodes = 100_000
)

// ToGo turns a Lua value into plain Go values: tables with keys 1..n become
// slices, other tables maps with string keys. It visits at most
// maxConvertNodes values, so a table sharing its subtables over and over
// cannot make it run for ever.
func ToGo(value lua.LValue) (any, error) {
	walk := &converter{}
	return walk.value(value, 0)
}

type converter struct {
	nodes int
}

func (c *converter) value(value lua.LValue, depth int) (any, error) {
	c.nodes++
	if c.nodes > maxConvertNodes {
		return nil, fmt.Errorf("value has more than %d parts", maxConvertNodes)
	}
	if depth > maxConvertDepth {
		return nil, fmt.Errorf("table nested deeper than %d", maxConvertDepth)
	}
	switch value := value.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(value), nil
	case lua.LNumber:
		number := float64(value)
		if number == math.Trunc(number) && math.Abs(number) < 1<<53 {
			return int64(number), nil
		}
		return number, nil
	case lua.LString:
		return string(value), nil
	case *lua.LTable:
		return c.table(value, depth)
	}
	return nil, fmt.Errorf("cannot pass a Lua %s", value.Type())
}

func (c *converter) table(table *lua.LTable, depth int) (any, error) {
	if length := table.Len(); length > 0 {
		list := make([]any, 0, min(length, maxConvertNodes))
		for i := 1; i <= length; i++ {
			item, err := c.value(table.RawGetInt(i), depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, item)
		}
		return list, nil
	}
	fields := map[string]any{}
	var err error
	table.ForEach(func(key, value lua.LValue) {
		if err != nil {
			return
		}
		fields[key.String()], err = c.value(value, depth+1)
	})
	return fields, err
}

// ToLua turns plain Go values, as JSON decoding gives them, into Lua values.
func ToLua(state *lua.LState, value any) lua.LValue {
	switch value := value.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(value)
	case float64:
		return lua.LNumber(value)
	case int64:
		return lua.LNumber(value)
	case int:
		return lua.LNumber(value)
	case string:
		return lua.LString(value)
	case []any:
		table := state.NewTable()
		for _, item := range value {
			table.Append(ToLua(state, item))
		}
		return table
	case map[string]any:
		table := state.NewTable()
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			table.RawSetString(key, ToLua(state, value[key]))
		}
		return table
	}
	return lua.LString(fmt.Sprint(value))
}

// maxRepeatResult caps string.rep, which would otherwise exhaust memory in
// one call.
const maxRepeatResult = 16 << 20

// Sandbox opens only base, table, string and math in a state made with
// SkipOpenLibs, takes away what loads code from files, and caps string.rep.
func Sandbox(state *lua.LState) {
	for _, library := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		state.Push(state.NewFunction(library.open))
		state.Push(lua.LString(library.name))
		state.Call(1, 0)
	}
	for _, unsafe := range []string{"dofile", "loadfile", "require", "module", "_printregs"} {
		state.SetGlobal(unsafe, lua.LNil)
	}
	stringTable, ok := state.GetGlobal("string").(*lua.LTable)
	if !ok {
		return
	}
	stringTable.RawSetString("rep", state.NewFunction(func(state *lua.LState) int {
		text, count, separator := state.CheckString(1), state.CheckInt(2), state.OptString(3, "")
		if count <= 0 {
			state.Push(lua.LString(""))
			return 1
		}
		if (len(text)+len(separator))*count > maxRepeatResult {
			state.RaiseError("string.rep: result over %d MB", maxRepeatResult>>20)
		}
		state.Push(lua.LString(strings.Repeat(text+separator, count-1) + text))
		return 1
	}))
}
