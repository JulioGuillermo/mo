package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | UI-01 | Happy    | create shows a valid tree under a new id; update and remove change it   | P0       |
// | UI-02 | Error    | A bad tree is refused naming where: type, ids, colours, handlers        | P0       |
// | UI-03 | Happy    | An event becomes a note for the model of its own conversation only     | P0       |
// | UI-04 | Happy    | A script validates as the user types and submits once valid, keeping its state | P0 |
// | UI-05 | Error    | A missing function is refused; a failing or endless one is shown and told | P0     |
// | UI-06 | Happy    | Interfaces are saved with the conversation and come back on resume, scripts and all | P0 |

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
			`type "box"`:  {"type": "box"},
			"needs an id": {"type": "panel", "children": []any{map[string]any{"type": "button"}}},
			"used twice":  {"type": "panel", "children": []any{map[string]any{"type": "input", "id": "a"}, map[string]any{"type": "button", "id": "a"}}},
			`bg "pink"`:   {"type": "label", "bg": "pink"},

			"only a panel or a card":  {"type": "label", "children": []any{map[string]any{"type": "label"}}},
			"padding takes 1, 2 or 4": {"type": "label", "padding": []int{1, 2, 3}},
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
}
