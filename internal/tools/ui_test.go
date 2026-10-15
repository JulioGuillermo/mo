package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | UI-01 | Happy    | create shows a valid tree under a new id; update and remove change it   | P0       |
// | UI-02 | Error    | A bad tree is refused naming where: type, ids, colours, handlers        | P0       |
// | UI-03 | Happy    | An event becomes a note for the model of its own conversation only     | P0       |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func uiRegistry(t *testing.T) (*Registry, *UIs) {
	t.Helper()
	uis := NewUIs()
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
			`type "box"`:              {"type": "box"},
			"needs an id":             {"type": "panel", "children": []any{map[string]any{"type": "button"}}},
			"used twice":              {"type": "panel", "children": []any{map[string]any{"type": "input", "id": "a"}, map[string]any{"type": "button", "id": "a"}}},
			`bg "pink"`:               {"type": "label", "bg": "pink"},
			"script handlers":         {"type": "button", "id": "b", "on_click": "validate"},
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
		_, _ = uis.create("Login", UINode{Type: UIButton, ID: "go"})
		uis.Event(UIEvent{UI: 1, Action: UISubmit, Event: "click", Element: "go", Values: map[string]string{"user": "ana"}})
		uis.SetConversation("c2")
		elsewhere := uis.HasEvents()
		uis.SetConversation("c1")
		events := uis.TakeEvents()

		if elsewhere || len(events) != 1 || events[0].Note() != `UI 1 "Login": submitted with go; values: user="ana"` || uis.HasEvents() {
			t.Errorf("elsewhere = %v events = %+v", elsewhere, events)
		}
	})
}
