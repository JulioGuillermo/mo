package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/luabox"
)

const (
	uiScriptLoadTimeout  = 2 * time.Second
	uiScriptEventTimeout = 500 * time.Millisecond
)

// uiScript is an interface's Lua: a sandboxed state of its own, its ui
// table, and the event it is handling.
type uiScript struct {
	state *lua.LState
	table *lua.LTable
	// While a handler runs: its interface, the manager, the event, and
	// what it led to.
	ui      *UI
	uis     *UIs
	event   UIEvent
	outcome UIOutcome
}

// loadScript runs an interface's script in a new sandbox, its state as the
// interface kept it.
func loadScript(ui *UI) (*uiScript, error) {
	state := lua.NewState(lua.Options{SkipOpenLibs: true})
	luabox.Sandbox(state)
	script := &uiScript{state: state, ui: ui}
	script.table = state.NewTable()
	for name, function := range map[string]lua.LGFunction{
		"set": script.set, "get": script.get, "value": script.value,
		"notify": script.tell(UINotify), "submit": script.tell(UISubmit), "close": script.tell(UIClose), "remove": script.tell(UIRemove),
	} {
		script.table.RawSetString(name, state.NewFunction(function))
	}
	kept := ui.State
	if kept == nil {
		kept = map[string]any{}
	}
	if table, ok := luabox.ToLua(state, kept).(*lua.LTable); ok {
		script.table.RawSetString("state", table)
	}
	state.SetGlobal("ui", script.table)
	ctx, cancel := context.WithTimeout(context.Background(), uiScriptLoadTimeout)
	defer cancel()
	state.SetContext(ctx)
	defer state.RemoveContext()
	if err := state.DoString(ui.Script); err != nil {
		state.Close()
		return nil, fmt.Errorf("script: %s", shortLuaError(err))
	}
	return script, nil
}

// runHandler calls a function of the interface's script for an event; the
// lock is held. An error is kept to show and told to the model.
func (u *UIs) runHandler(ui *UI, base UIEvent, handler string) UIOutcome {
	script := ui.script
	fail := func(message string) UIOutcome {
		ui.Error = message
		event := base
		event.Action, event.Value = UIError, message
		u.events = append(u.events, event)
		return UIOutcome{Told: true}
	}
	if script == nil {
		return fail("there is no script")
	}
	function, ok := script.state.GetGlobal(handler).(*lua.LFunction)
	if !ok {
		return fail(fmt.Sprintf("the script defines no function %q", handler))
	}
	script.ui, script.uis, script.event, script.outcome = ui, u, base, UIOutcome{}
	event := script.state.NewTable()
	event.RawSetString("id", lua.LString(base.Element))
	event.RawSetString("type", lua.LString(base.Event))
	event.RawSetString("value", lua.LString(base.Value))
	if base.At {
		event.RawSetString("x", lua.LNumber(base.X))
		event.RawSetString("y", lua.LNumber(base.Y))
	}
	ctx, cancel := context.WithTimeout(context.Background(), uiScriptEventTimeout)
	defer cancel()
	script.state.SetContext(ctx)
	err := script.state.CallByParam(lua.P{Fn: function, NRet: 0, Protect: true}, event)
	script.state.RemoveContext()
	if kept, ok := script.table.RawGetString("state").(*lua.LTable); ok {
		if value, convErr := luabox.ToGo(kept); convErr == nil {
			if state, isMap := value.(map[string]any); isMap {
				ui.State = state
			}
		}
	}
	if err != nil {
		outcome := script.outcome
		failed := fail(shortLuaError(err))
		failed.Submitted, failed.Hidden, failed.Removed = outcome.Submitted, outcome.Hidden, outcome.Removed
		return failed
	}
	ui.Error = ""
	return script.outcome
}

func shortLuaError(err error) string {
	text := err.Error()
	if line, _, found := strings.Cut(text, "\nstack traceback:"); found {
		text = line
	}
	return strings.TrimSpace(text)
}

// element is the id a function was given, refused when the tree has none.
func (s *uiScript) element(state *lua.LState) (string, UINode) {
	id := state.CheckString(1)
	node, ok := findUINode(s.ui.Shown(), id)
	if !ok {
		state.RaiseError("no element %q", id)
	}
	return id, node
}

// set changes an element as ui.set(id, {text=..., visible=false}) asks.
func (s *uiScript) set(state *lua.LState) int {
	id, node := s.element(state)
	value, err := luabox.ToGo(state.CheckTable(2))
	props, ok := value.(map[string]any)
	if err != nil || !ok {
		state.RaiseError("ui.set(%q, props): props is a table of names, as {text = \"...\"}", id)
	}
	if typed, given := props["value"]; given {
		if !node.HasValue() {
			state.RaiseError("%q has no value: inputs, selects, sliders, checkboxes, switches and radios do", id)
		}
		text := valueText(typed)
		node.Value = text
		if err := checkValue(node, id); err != nil {
			state.RaiseError("ui.set: %s", err.Error())
		}
		if s.ui.Values == nil {
			s.ui.Values = map[string]string{}
		}
		s.ui.Values[id] = text
		delete(props, "value")
	}
	if err := applyProps(&node, props); err != nil {
		state.RaiseError("ui.set(%q): %s", id, err.Error())
	}
	if s.ui.Changes == nil {
		s.ui.Changes = map[string]map[string]any{}
	}
	if s.ui.Changes[id] == nil {
		s.ui.Changes[id] = map[string]any{}
	}
	for name, prop := range props {
		s.ui.Changes[id][name] = prop
	}
	return 0
}

// get reads an element as it is shown.
func (s *uiScript) get(state *lua.LState) int {
	_, node := s.element(state)
	visible := node.Shown()
	state.Push(luabox.ToLua(state, map[string]any{
		"type": node.Type, "text": node.Text, "label": node.Label, "value": node.Value, "placeholder": node.Placeholder,
		"title": node.Title, "fg": node.FG, "bg": node.BG, "visible": visible, "disabled": node.Disabled, "bold": node.Bold,
		"width": sizeValue(node.Width), "height": sizeValue(node.Height), "border": node.Border, "options": node.Options, "svg": node.SVG,
		"min": node.Min, "max": node.Max, "step": node.Step, "checked": node.Checked(),
	}))
	return 1
}

// value is a control's value as the user left it.
func (s *uiScript) value(state *lua.LState) int {
	_, node := s.element(state)
	state.Push(lua.LString(node.Current()))
	return 1
}

// tell is ui.notify, ui.submit or ui.close, with data for the model.
func (s *uiScript) tell(action string) lua.LGFunction {
	return func(state *lua.LState) int {
		if s.uis == nil {
			state.RaiseError("ui.%s works in an event's function", action)
		}
		data := ""
		if state.GetTop() >= 1 && state.Get(1) != lua.LNil {
			value, err := luabox.ToGo(state.Get(1))
			if err != nil {
				state.RaiseError("ui.%s: %s", action, err.Error())
			}
			encoded, _ := json.Marshal(value)
			data = string(encoded)
		}
		outcome := s.uis.act(s.ui, s.event, action, data)
		s.outcome.Told = s.outcome.Told || outcome.Told
		s.outcome.Submitted = s.outcome.Submitted || outcome.Submitted
		s.outcome.Hidden = s.outcome.Hidden || outcome.Hidden
		s.outcome.Removed = s.outcome.Removed || outcome.Removed
		return 0
	}
}

// applyProps changes a node's fields from a script, checking each as the
// tree is checked.
func applyProps(n *UINode, props map[string]any) error {
	for name, value := range props {
		text, isText := value.(string)
		flag, isFlag := value.(bool)
		var number int
		isNumber := true
		switch v := value.(type) {
		case int64:
			number = int(v)
		case float64:
			number = int(v)
		default:
			isNumber = false
		}
		switch name {
		case "text", "label", "placeholder", "title", "svg":
			if !isText {
				text = fmt.Sprint(value)
			}
			switch name {
			case "svg":
				if n.Type != UISVG {
					return fmt.Errorf("only an svg node has svg")
				}
				n.SVG = text
			case "text":
				n.Text = text
			case "label":
				n.Label = text
			case "placeholder":
				n.Placeholder = text
			default:
				n.Title = text
			}
		case "fg", "bg":
			if !isText || (text != "" && !hexColor.MatchString(text) && !slices.Contains(UIColors, text)) {
				return fmt.Errorf("%s %v: use #rrggbb or one of %s", name, value, strings.Join(UIColors, ", "))
			}
			if name == "fg" {
				n.FG = text
			} else {
				n.BG = text
			}
		case "border":
			if !isText || !slices.Contains(uiBorders, text) {
				return fmt.Errorf("border %v: use %s", value, strings.Join(uiBorders[1:], ", "))
			}
			n.Border = text
		case "visible", "disabled", "bold":
			if !isFlag {
				return fmt.Errorf("%s is true or false", name)
			}
			switch name {
			case "visible":
				n.Visible = &flag
			case "disabled":
				n.Disabled = flag
			default:
				n.Bold = flag
			}
		case "options":
			list, isList := value.([]any)
			if !isList {
				return fmt.Errorf("options is a list of strings")
			}
			n.Options = nil
			for _, option := range list {
				n.Options = append(n.Options, valueText(option))
			}
		case "min", "max", "step":
			amount, isAmount := asFloat(value)
			if !isAmount {
				return fmt.Errorf("%s is a number", name)
			}
			switch name {
			case "min":
				n.Min = amount
			case "max":
				n.Max = amount
			default:
				n.Step = amount
			}
		case "width", "height":
			size, err := parseUISize(value)
			if err != nil {
				return fmt.Errorf("%s %w", name, err)
			}
			if name == "width" {
				n.Width = size
			} else {
				n.Height = size
			}
		case "min_width", "max_width", "min_height", "max_height":
			if !isNumber || number < 0 {
				return fmt.Errorf("%s is a number of cells", name)
			}
			switch name {
			case "min_width":
				n.MinWidth = number
			case "max_width":
				n.MaxWidth = number
			case "min_height":
				n.MinHeight = number
			default:
				n.MaxHeight = number
			}
		default:
			return fmt.Errorf("%q cannot be set: use text, label, value, placeholder, title, options, min, max, step, svg, fg, bg, border, visible, disabled, bold, width, height, min_width, max_width, min_height or max_height", name)
		}
	}
	return nil
}

// valueText is a value as a control keeps it: true or false, a number
// without needless decimals, or the text.
func valueText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return fmt.Sprint(v)
	}
	if amount, ok := asFloat(value); ok {
		return FormatNumber(amount)
	}
	return fmt.Sprint(value)
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	}
	return 0, false
}

// sizeValue is a size as a script reads it: a number, or "full".
func sizeValue(size UISize) any {
	if size.Full() {
		return uiFullName
	}
	return int(size)
}
