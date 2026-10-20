package plugins

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/tools"
)

const maxComponentDepth = 16

// declareComponent is mo.component{ name, render }: a node type of the
// plugin's own, which render turns into nodes mo knows when a dialog shows.
func (p *Plugin) declareComponent(state *lua.LState) int {
	p.mustBeLoading(state, "mo.component")
	spec := state.CheckTable(1)
	name := luaString(spec.RawGetString("name"))
	render, isFunction := spec.RawGetString("render").(*lua.LFunction)
	if !validName(name) || !isFunction {
		state.RaiseError("mo.component needs a name (a letter or _, then letters, digits, _ or -) and a render function")
	}
	if tools.IsUIType(name) {
		state.RaiseError("mo.component: %s is a node type of mo's, or a name taken as one; pick another", name)
	}
	if _, taken := p.components[name]; taken {
		state.RaiseError("mo.component: %s is declared twice", name)
	}
	p.components[name] = render
	return 0
}

// expandComponents replaces the nodes of the plugin's components in a tree
// by what their render gives for them, their fields and children as props,
// down to nodes mo knows.
func (p *Plugin) expandComponents(state *lua.LState, value any, depth int) (any, error) {
	switch value := value.(type) {
	case []any:
		expanded := make([]any, len(value))
		for i, each := range value {
			node, err := p.expandComponents(state, each, depth)
			if err != nil {
				return nil, err
			}
			expanded[i] = node
		}
		return expanded, nil
	case map[string]any:
		name, _ := value["type"].(string)
		render, isComponent := p.components[name]
		if !isComponent {
			return p.expandChildren(state, value, depth)
		}
		if depth >= maxComponentDepth {
			return nil, fmt.Errorf("component %s: components nested deeper than %d", name, maxComponentDepth)
		}
		if err := state.CallByParam(lua.P{Fn: render, NRet: 1, Protect: true}, toLua(state, value)); err != nil {
			return nil, fmt.Errorf("component %s: %w", name, p.readable(err))
		}
		result := state.Get(-1)
		state.Pop(1)
		rendered, err := toGo(result)
		if err != nil {
			return nil, fmt.Errorf("component %s: %w", name, err)
		}
		return p.expandComponents(state, rendered, depth+1)
	}
	return value, nil
}

func (p *Plugin) expandChildren(state *lua.LState, node map[string]any, depth int) (any, error) {
	children, ok := node["children"]
	if !ok {
		return node, nil
	}
	expanded, err := p.expandComponents(state, children, depth)
	if err != nil {
		return nil, err
	}
	copied := make(map[string]any, len(node))
	for key, each := range node {
		copied[key] = each
	}
	copied["children"] = expanded
	return copied, nil
}
