package plugins

import (
	"fmt"
	"math"
	"sort"

	lua "github.com/yuin/gopher-lua"
)

const (
	maxConvertDepth = 64
	maxConvertNodes = 100_000
)

// toGo turns a Lua value into plain Go values: tables with keys 1..n become
// slices, other tables maps with string keys. It visits at most
// maxConvertNodes values, so a table sharing its subtables over and over
// cannot make it run for ever.
func toGo(value lua.LValue) (any, error) {
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

// toLua turns plain Go values, as JSON decoding gives them, into Lua values.
func toLua(state *lua.LState, value any) lua.LValue {
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
			table.Append(toLua(state, item))
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
			table.RawSetString(key, toLua(state, value[key]))
		}
		return table
	}
	return lua.LString(fmt.Sprint(value))
}
