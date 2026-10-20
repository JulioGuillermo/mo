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
// down to nodes mo knows. A render that gives { draw, size, redraw } makes
// a canvas, whose functions go to canvases.
func (p *Plugin) expandComponents(state *lua.LState, value any, depth int, canvases *[]canvasSpec) (any, error) {
	switch value := value.(type) {
	case []any:
		expanded := make([]any, len(value))
		for i, each := range value {
			node, err := p.expandComponents(state, each, depth, canvases)
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
			return p.expandChildren(state, value, depth, canvases)
		}
		if depth >= maxComponentDepth {
			return nil, fmt.Errorf("component %s: components nested deeper than %d", name, maxComponentDepth)
		}
		if err := state.CallByParam(lua.P{Fn: render, NRet: 1, Protect: true}, toLua(state, value)); err != nil {
			return nil, fmt.Errorf("component %s: %w", name, p.readable(err))
		}
		result := state.Get(-1)
		state.Pop(1)
		if drawn, ok := result.(*lua.LTable); ok {
			if draw, isFunction := drawn.RawGetString("draw").(*lua.LFunction); isFunction {
				return p.drawnComponent(state, name, value, drawn, draw, canvases)
			}
		}
		rendered, err := toGo(takeCanvases(state, result, canvases))
		if err != nil {
			return nil, fmt.Errorf("component %s: %w", name, err)
		}
		return p.expandComponents(state, rendered, depth+1, canvases)
	}
	return value, nil
}

func (p *Plugin) expandChildren(state *lua.LState, node map[string]any, depth int, canvases *[]canvasSpec) (any, error) {
	children, ok := node["children"]
	if !ok {
		return node, nil
	}
	expanded, err := p.expandComponents(state, children, depth, canvases)
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

// placedFields are the fields of a component's node its canvas keeps: where
// and how big it is, and how it takes keys.
var placedFields = []string{"id", "width", "height", "min_width", "max_width", "min_height", "max_height", "grow",
	"margin", "padding", "border", "bg", "fg", "visible", "fps", "keep_esc", "focusable"}

// sizeFields are what a component's size gives, as the node's fields.
var sizeFields = map[string]string{"w": "width", "h": "height", "min_w": "min_width", "max_w": "max_width", "min_h": "min_height", "max_h": "max_height"}

// drawnComponent is the canvas a component draws itself as: its node's
// placing fields, and the sizes its size(most_w, most_h) gives for those
// the node leaves unset, with the screen's size as the most. It takes the
// focus only when its render or its node says focusable = true.
func (p *Plugin) drawnComponent(state *lua.LState, name string, props map[string]any, drawn *lua.LTable, draw *lua.LFunction, canvases *[]canvasSpec) (any, error) {
	node := map[string]any{"type": tools.UICanvas}
	for _, field := range placedFields {
		if each, ok := props[field]; ok {
			node[field] = each
		}
	}
	if node["id"] == nil {
		node["id"] = fmt.Sprintf("%s-%d", name, len(*canvases)+1)
	}
	if node["focusable"] == nil {
		node["focusable"] = lua.LVAsBool(drawn.RawGetString("focusable"))
	}
	if size, isFunction := drawn.RawGetString("size").(*lua.LFunction); isFunction {
		width, height := p.screenOrDefault()
		if err := state.CallByParam(lua.P{Fn: size, NRet: 1, Protect: true}, lua.LNumber(width), lua.LNumber(height)); err != nil {
			return nil, fmt.Errorf("component %s: size: %w", name, p.readable(err))
		}
		sizes, _ := state.Get(-1).(*lua.LTable)
		state.Pop(1)
		if sizes == nil || sizes.RawGetString("w") == lua.LNil || sizes.RawGetString("h") == lua.LNil {
			return nil, fmt.Errorf("component %s: size gives { w, h }, and min_w, max_w, min_h, max_h if it likes", name)
		}
		for from, field := range sizeFields {
			if number, ok := sizes.RawGetString(from).(lua.LNumber); ok && node[field] == nil {
				node[field] = int64(number)
			}
		}
	}
	id := fmt.Sprint(node["id"])
	*canvases = append(*canvases, canvasSpec{id: id, draw: draw, redraw: drawn.RawGetString("redraw")})
	return node, nil
}

// screenOrDefault is the terminal's size, or 80 by 24 before mo tells it.
func (p *Plugin) screenOrDefault() (int, int) {
	p.surface.mu.Lock()
	defer p.surface.mu.Unlock()
	if p.surface.screen[0] == 0 {
		return 80, 24
	}
	return p.surface.screen[0], p.surface.screen[1]
}
