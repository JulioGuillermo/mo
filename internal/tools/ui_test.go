package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | UI-01 | Happy    | create shows a valid tree under a new id; update and remove change it   | P0       |
// | UI-02 | Error    | A bad tree is refused naming where: type, ids, colours, handlers        | P0       |
// | UI-03 | Happy    | An event becomes a note for the model of its own conversation only     | P0       |
// | UI-04 | Happy    | A script validates as the user types and submits once valid, keeping its state | P0 |
// | UI-05 | Error    | A missing function is refused; a failing or endless one is shown and told | P0     |
// | UI-06 | Happy    | Interfaces are saved with the conversation and come back on resume, scripts and all | P0 |
// | UI-07 | Edge     | A tree as small models send it is mended; what cannot be is told with an example | P0 |
// | UI-08 | Happy    | help lists every topic with an example, one topic alone, and each node type as its own | P1 |
// | UI-09 | Happy    | Controls start at their defaults, move within their options and range, and go with submit | P0 |
// | UI-10 | Error    | A control's bad options, range or value are refused; a script sets one only to a valid value | P0 |
// | UI-12 | Happy    | A grid takes columns or widths, refuses neither; rows of cells are flattened, short rows filled | P1 |
// | UI-11 | Edge     | Controls as small models send them are mended: other names, options as objects or children, bools and numbers | P1 |

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
)

func uiRegistry(t *testing.T) (*Registry, *UIs) {
	t.Helper()
	uis := NewUIs("")
	uis.SetConversation("c1")
	return NewRegistry(Env{WorkDir: t.TempDir(), UIs: uis}, Options{}), uis
}

func TestUITool(t *testing.T) {
	tree := map[string]any{"type": "card", "title": "Login", "children": []any{
		map[string]any{"type": "input", "id": "user"},
		map[string]any{"type": "button", "id": "go", "label": "Go", "on_click": "submit"},
	}}

	t.Run("UI-01 create shows a valid tree under a new id; update and remove change it", func(t *testing.T) {
		registry, uis := uiRegistry(t)
		changes := 0
		uis.OnChange(func() { changes++ })

		created, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "title": "Login", "tree": tree})})
		updated, _ := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "update", "id": 1, "title": "Sign in"})})
		ui, _ := uis.Get(1)
		removed, _ := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "remove", "id": 1})})

		if isError || !strings.Contains(created, "UI 1 created") || updated != "UI 1 updated" || ui.Title != "Sign in" || ui.Version != 2 {
			t.Errorf("created = %q updated = %q ui = %+v", created, updated, ui)
		}
		if removed != "UI 1 removed" || len(uis.List()) != 0 || changes != 3 {
			t.Errorf("removed = %q left = %d changes = %d", removed, len(uis.List()), changes)
		}
	})

	t.Run("UI-02 a bad tree is refused naming where: type, ids, colours, handlers", func(t *testing.T) {
		registry, _ := uiRegistry(t)
		for want, bad := range map[string]map[string]any{
			`type "knob"`: {"type": "knob"},
			"needs an id": {"type": "panel", "children": []any{map[string]any{"type": "button"}}},
			"used twice":  {"type": "panel", "children": []any{map[string]any{"type": "input", "id": "a"}, map[string]any{"type": "button", "id": "a"}}},
			`bg "pink"`:   {"type": "label", "bg": "pink"},

			"only a panel, a card or a grid": {"type": "label", "children": []any{map[string]any{"type": "label"}}},
			"padding takes 1, 2 or 4":        {"type": "label", "padding": []int{1, 2, 3}},
		} {
			output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": bad})})
			if !isError || !strings.Contains(output, want) {
				t.Errorf("%s: output = %q", want, output)
			}
		}
	})

	t.Run("UI-03 an event becomes a note for the model of its own conversation only", func(t *testing.T) {
		_, uis := uiRegistry(t)
		_, _ = uis.create("Login", "", UINode{Type: UIButton, ID: "go"})
		uis.Event(UIEvent{UI: 1, Action: UISubmit, Event: "click", Element: "go", Values: map[string]string{"user": "ana"}})
		uis.SetConversation("c2")
		elsewhere := uis.HasEvents()
		uis.SetConversation("c1")
		events := uis.TakeEvents()

		if elsewhere || len(events) != 1 || events[0].Note() != `UI 1 "Login": submitted with go; values: user="ana"` || uis.HasEvents() {
			t.Errorf("elsewhere = %v events = %+v", elsewhere, events)
		}
	})

	signup := UINode{Type: UICard, Title: "Sign up", Children: []UINode{
		{Type: UIInput, ID: "email", OnInput: "check"},
		{Type: UILabel, ID: "err", Text: "not an email", Visible: new(false)},
		{Type: UIButton, ID: "ok", Label: "Create", OnClick: "send", Disabled: true},
	}}
	script := `
function check(e)
  local ok = e.value:match("^[^@]+@[^@]+%.%w+$") ~= nil
  ui.set("err", {visible = not ok})
  ui.set("ok", {disabled = not ok})
  ui.state.checks = (ui.state.checks or 0) + 1
end
function send(e)
  if not ui.get("ok").disabled then ui.submit({checks = ui.state.checks}) end
end`

	t.Run("UI-04 a script validates as the user types and submits once valid, keeping its state", func(t *testing.T) {
		_, uis := uiRegistry(t)
		if _, err := uis.create("Sign up", script, signup); err != nil {
			t.Fatal(err)
		}
		uis.SetValue(1, "email", "ana@")
		typing := uis.Fire(1, "email", "input")
		ui, _ := uis.Get(1)
		bad, _ := findUINode(ui.Shown(), "err")
		uis.SetValue(1, "email", "ana@x.io")
		uis.Fire(1, "email", "input")
		ui, _ = uis.Get(1)
		good, _ := findUINode(ui.Shown(), "err")
		button, _ := findUINode(ui.Shown(), "ok")
		sent := uis.Fire(1, "ok", "click")
		events := uis.TakeEvents()

		if typing.Told || !bad.Shown() || good.Shown() || button.Disabled {
			t.Errorf("typing = %+v error shown %v then %v, button disabled %v", typing, bad.Shown(), good.Shown(), button.Disabled)
		}
		if !sent.Submitted || len(events) != 1 || events[0].Note() != `UI 1 "Sign up": submitted with ok; values: email="ana@x.io"; data: {"checks":2}` {
			t.Errorf("sent = %+v events = %+v", sent, events)
		}
	})

	t.Run("UI-05 a missing function is refused; a failing or endless one is shown and told", func(t *testing.T) {
		registry, uis := uiRegistry(t)
		missing, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{
			"action": "create", "tree": map[string]any{"type": "button", "id": "b", "on_click": "absent"}, "script": "function present() end",
		})})
		if !isError || !strings.Contains(missing, `defines no function "absent"`) {
			t.Errorf("missing = %q", missing)
		}
		_, _ = uis.create("Broken", "function boom() error('kaboom') end\nfunction spin() while true do end end",
			UINode{Type: UIPanel, Children: []UINode{{Type: UIButton, ID: "a", OnClick: "boom"}, {Type: UIButton, ID: "b", OnClick: "spin"}}})

		boom := uis.Fire(1, "a", "click")
		ui, _ := uis.Get(1)
		started := time.Now()
		spin := uis.Fire(1, "b", "click")
		events := uis.TakeEvents()

		if !boom.Told || !strings.Contains(ui.Error, "kaboom") || !spin.Told || time.Since(started) > 3*time.Second {
			t.Errorf("boom = %+v error = %q spin took %v", boom, ui.Error, time.Since(started))
		}
		if len(events) != 2 || events[0].Action != UIError || !strings.Contains(events[0].Note(), "its script failed on click a") {
			t.Errorf("events = %+v", events)
		}
	})

	t.Run("UI-06 interfaces are saved with the conversation and come back on resume, scripts and all", func(t *testing.T) {
		root := t.TempDir()
		before := NewUIs(root)
		before.SetConversation("c1")
		if _, err := before.create("Sign up", script, signup); err != nil {
			t.Fatal(err)
		}
		before.SetValue(1, "email", "ana@x.io")
		before.Fire(1, "email", "input")

		after := NewUIs(root)
		after.SetConversation("c1")
		ui, ok := after.Get(1)
		after.SetValue(1, "email", "ana@y.io")
		after.Fire(1, "email", "input")
		ui2, _ := after.Get(1)

		if !ok || ui.Values["email"] != "ana@x.io" || ui.State["checks"] != float64(1) {
			t.Fatalf("values = %v state = %#v ok = %v", ui.Values, ui.State, ok)
		}
		if ui2.State["checks"] != int64(2) || ui2.Error != "" {
			t.Errorf("the script should go on from its state: %+v", ui2.State)
		}
	})

	t.Run("UI-07 a tree as small models send it is mended; what cannot be is told with an example", func(t *testing.T) {
		for name, tree := range map[string]any{
			"as a string":      `{"type":"card","children":[{"type":"button","id":"ok","text":"OK"}]}`,
			"a list":           []any{map[string]any{"type": "button", "id": "ok", "label": "OK"}},
			"under its type":   map[string]any{"card": map[string]any{"children": []any{map[string]any{"button": map[string]any{"id": "ok", "label": "OK"}}}}},
			"no types":         map[string]any{"title": "T", "items": []any{map[string]any{"text": "hi"}, map[string]any{"id": "ok", "onClick": "submit", "label": "OK"}}},
			"other type names": map[string]any{"type": "column", "children": []any{map[string]any{"type": "text", "content": "hi"}, map[string]any{"type": "btn", "id": "ok", "text": "OK"}}},
		} {
			registry, uis := uiRegistry(t)
			output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": tree})})
			ui, _ := uis.Get(1)
			button, found := findUINode(ui.Tree, "ok")
			if isError || !found || button.Type != UIButton || button.Label != "OK" {
				t.Errorf("%s: output = %q button = %+v", name, output, button)
			}
		}
		registry, uis := uiRegistry(t)
		_, _ = registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": map[string]any{
			"type": "row", "children": []any{map[string]any{"type": "col"}, map[string]any{"type": "panel", "direction": "col"}},
		}})})
		ui, _ := uis.Get(1)
		if ui.Tree.Direction != "row" || len(ui.Tree.Children) != 2 || ui.Tree.Children[0].Direction != "column" || ui.Tree.Children[1].Direction != "column" {
			t.Errorf("row and col should be panels with their direction, as type or direction: %+v", ui.Tree)
		}
		for want, bad := range map[string]any{
			`no "type"`:              map[string]any{"type": "panel", "children": []any{map[string]any{"id": "x"}}},
			`unknown field "colour"`: map[string]any{"type": "label", "colour": "red"},
		} {
			registry, _ := uiRegistry(t)
			output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": bad})})
			if !isError || !strings.Contains(output, want) || !strings.Contains(output, `"type":"card"`) && !strings.Contains(output, "a node takes") {
				t.Errorf("%s: output = %q", want, output)
			}
		}
	})

	t.Run("UI-08 help lists every topic with an example, one topic alone, and each node type as its own", func(t *testing.T) {
		registry, _ := uiRegistry(t)
		all, _ := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "help"})})
		example, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "help", "topic": "example"})})
		slider, _ := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "help", "topic": "slider"})})
		unknown, _ := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "help", "topic": "knob"})})
		for _, kind := range UITypes {
			if uiNodeHelp[kind] == "" || strings.Contains(all, uiNodeHelp[kind]) {
				t.Errorf("%s should have a topic of its own, out of the list", kind)
			}
		}
		if !strings.HasPrefix(slider, "a number on a track") || !strings.Contains(slider, `{"type":"slider"`) || !strings.Contains(unknown, "or a node type: panel, card, grid") {
			t.Errorf("slider = %q unknown = %q", slider, unknown)
		}
		if !strings.Contains(all, "\n\ntopic nodes:\nEvery node") || !strings.Contains(all, "\ntopic example:\n{") || strings.Contains(all, "view:") || isError || !strings.HasPrefix(example, `{"action":"create"`) {
			t.Errorf("all = %q example = %q", all, example)
		}
	})

	controls := map[string]any{"type": "panel", "children": []any{
		map[string]any{"type": "select", "id": "size", "options": []any{"S", "M", "L"}},
		map[string]any{"type": "slider", "id": "vol", "min": 0, "max": 1, "step": 0.25, "value": "0.5"},
		map[string]any{"type": "checkbox", "id": "terms"},
		map[string]any{"type": "switch", "id": "dark", "value": "true"},
		map[string]any{"type": "radio", "id": "plan", "options": []any{"free", "pro"}},
		map[string]any{"type": "button", "id": "go", "on_click": "submit"},
	}}

	t.Run("UI-09 controls start at their defaults, move within their options and range, and go with submit", func(t *testing.T) {
		registry, uis := uiRegistry(t)
		if output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": controls})}); isError {
			t.Fatal(output)
		}
		ui, _ := uis.Get(1)
		node := func(id string) UINode { found, _ := findUINode(ui.Shown(), id); return found }
		size, vol, plan := node("size"), node("vol"), node("plan")
		moves := []string{size.Moved(-1), size.Moved(1), vol.Moved(1), vol.Moved(-3), vol.AtFraction(0.6), plan.Moved(-1), plan.Moved(5), node("terms").Toggled()}
		uis.SetValue(1, "plan", "pro")
		uis.Fire(1, "go", "click")
		events := uis.TakeEvents()

		if want := []string{"L", "M", "0.75", "0", "0.5", "pro", "free", "true"}; strings.Join(moves, " ") != strings.Join(want, " ") {
			t.Errorf("moves = %v, want %v", moves, want)
		}
		if len(events) != 1 || events[0].Note() != `UI 1 "Interface": submitted with go; values: dark="true", plan="pro", size="S", terms="false", vol="0.5"` {
			t.Errorf("events = %+v", events)
		}
	})

	t.Run("UI-10 a control's bad options, range or value are refused; a script sets one only to a valid value", func(t *testing.T) {
		registry, uis := uiRegistry(t)
		for want, bad := range map[string]map[string]any{
			"needs options":         {"type": "select", "id": "s"},
			"none of its options":   {"type": "radio", "id": "r", "options": []any{"a"}, "value": "b"},
			"not above min":         {"type": "slider", "id": "v", "min": 5, "max": 2},
			"not a number":          {"type": "slider", "id": "v", "value": "loud"},
			"use true or false":     {"type": "switch", "id": "w", "value": "maybe"},
			"checkbox) needs an id": {"type": "checkbox"},
		} {
			output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": bad})})
			if !isError || !strings.Contains(output, want) {
				t.Errorf("%s: output = %q", want, output)
			}
		}
		script := `function bad() ui.set("size", {value = "XL"}) end
function good() ui.set("size", {value = "L", options = {"M", "L"}}) ui.set("vol", {value = 0.25}) ui.notify({size = ui.value("size"), checked = ui.get("dark").checked}) end`
		tree := map[string]any{"type": "panel", "children": []any{
			controls["children"].([]any)[0], controls["children"].([]any)[1], controls["children"].([]any)[3],
			map[string]any{"type": "button", "id": "b", "on_click": "bad"}, map[string]any{"type": "button", "id": "g", "on_click": "good"},
		}}
		if output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": tree, "script": script})}); isError {
			t.Fatal(output)
		}
		uis.Fire(1, "b", "click")
		failed, _ := uis.Get(1)
		uis.Fire(1, "g", "click")
		ui, _ := uis.Get(1)
		size, _ := findUINode(ui.Shown(), "size")
		vol, _ := findUINode(ui.Shown(), "vol")
		events := uis.TakeEvents()

		if !strings.Contains(failed.Error, "none of its options") || size.Current() != "L" || len(size.Options) != 2 || vol.Current() != "0.25" {
			t.Errorf("error = %q size = %+v vol = %q", failed.Error, size, vol.Current())
		}
		if len(events) != 2 || !strings.HasSuffix(events[1].Note(), `data: {"checked":true,"size":"L"}`) {
			t.Errorf("events = %+v", events)
		}
	})

	t.Run("UI-11 controls as small models send them are mended: other names, options as objects or children, bools and numbers", func(t *testing.T) {
		registry, uis := uiRegistry(t)
		tree := map[string]any{"type": "form", "items": []any{
			map[string]any{"type": "dropdown", "id": "size", "choices": []any{map[string]any{"label": "Small", "value": "s"}, map[string]any{"label": "Big", "value": "b"}}, "selected": "b"},
			map[string]any{"type": "radio_group", "id": "plan", "children": []any{map[string]any{"text": "free"}, "pro"}},
			map[string]any{"type": "range", "id": "vol", "minimum": 1, "maximum": 5, "value": 3},
			map[string]any{"type": "toggle", "id": "dark", "text": "Dark", "checked": true},
			map[string]any{"id": "terms", "type": "check", "label": "OK", "value": false},
			map[string]any{"id": "guess", "options": []any{"a", "b"}},
			map[string]any{"id": "tick", "label": "Tick", "checked": true},
		}}
		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create", "tree": tree})})
		ui, _ := uis.Get(1)
		got := map[string]string{}
		for _, id := range []string{"size", "plan", "vol", "dark", "terms", "guess", "tick"} {
			node, _ := findUINode(ui.Shown(), id)
			got[id] = node.Type + "=" + node.Current() + "/" + strings.Join(node.Options, ",") + "/" + node.Label
		}
		want := map[string]string{"size": "select=b/s,b/", "plan": "radio=/free,pro/", "vol": "slider=3//", "dark": "switch=true//Dark", "terms": "checkbox=false//OK", "guess": "select=a/a,b/", "tick": "checkbox=true//Tick"}
		if isError {
			t.Fatal(output)
		}
		for id, value := range want {
			if got[id] != value {
				t.Errorf("%s = %q, want %q", id, got[id], value)
			}
		}
	})

	t.Run("UI-12 a grid takes columns or widths, refuses neither; rows of cells are flattened, short rows filled", func(t *testing.T) {
		registry, uis := uiRegistry(t)
		bad, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create",
			"tree": map[string]any{"type": "grid", "children": []any{"a", "b"}}})})
		if !isError || !strings.Contains(bad, "needs columns") {
			t.Errorf("bad = %q", bad)
		}
		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create",
			"tree": map[string]any{"type": "table", "rows": []any{[]any{"Host", "Status"}, []any{"db1"}, []any{"web", map[string]any{"type": "switch", "id": "on"}}}}})})
		ui, _ := uis.Get(1)
		widths, _ := registry.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args(t, map[string]any{"action": "create",
			"tree": map[string]any{"widths": []any{10, 0}, "children": []any{"a", "b"}}})})
		second, _ := uis.Get(2)

		if isError || ui.Tree.Type != UIGrid || ui.Tree.GridColumns() != 2 || len(ui.Tree.Children) != 6 || ui.Tree.Children[3].Text != "" || ui.Tree.Children[5].ID != "on" {
			t.Errorf("output = %q tree = %+v", output, ui.Tree)
		}
		if second.Tree.Type != UIGrid || second.Tree.GridColumns() != 2 {
			t.Errorf("widths = %q tree = %+v", widths, second.Tree)
		}
	})
}
