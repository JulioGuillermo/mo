package tui

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | UD-01 | Happy    | The model's interface is listed in the panel, new until opened as a dialog | P0      |
// | UD-02 | Happy    | Typing fills the focused input; submit sends every value to the model    | P0       |
// | UD-03 | Happy    | A click outside hides the dialog; close removes it and tells the model   | P1       |

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func uiModel(t *testing.T) (model, *tools.UIs) {
	t.Helper()
	m := sidebarModel(t, 140)
	uis := tools.NewUIs()
	uis.SetConversation("c1")
	m.session.Agent = &agent.Agent{Tools: tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Jobs: m.jobs(), UIs: uis}, tools.Options{})}
	m.running = true
	tree := `{"type":"card","title":"Sign up","children":[
	  {"type":"input","id":"email","placeholder":"you@example.com"},
	  {"type":"panel","direction":"row","gap":2,"children":[
	    {"type":"button","id":"cancel","label":"Cancel","on_click":"close"},
	    {"type":"button","id":"ok","label":"Create","on_click":"submit"}]}]}`
	output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: []byte(`{"action":"create","title":"Sign up","tree":` + tree + `}`)})
	if isError {
		t.Fatalf("create: %s", output)
	}
	return m, uis
}

func TestUIDialog(t *testing.T) {
	t.Run("UD-01 the model's interface is listed in the panel, new until opened as a dialog", func(t *testing.T) {
		m, uis := uiModel(t)
		panel := ansi.Strip(m.View().Content)

		m = clickOn(t, m, "Sign up")
		screen := ansi.Strip(m.View().Content)
		ui, _ := uis.Get(1)

		if !strings.Contains(panel, "UIs") || !strings.Contains(panel, "● 1 Sign up") {
			t.Errorf("panel:\n%s", panel)
		}
		if m.uiOpen != 1 || !ui.Seen || !strings.Contains(screen, "UI 1 · Sign up") || !strings.Contains(screen, "Create") || !strings.Contains(screen, "Jobs") {
			t.Errorf("dialog:\n%s", screen)
		}
	})

	t.Run("UD-02 typing fills the focused input; submit sends every value to the model", func(t *testing.T) {
		m, uis := uiModel(t)
		updated, _ := m.openUI(1)
		m = updated.(model)
		for _, key := range []tea.KeyPressMsg{{Code: 'a', Text: "a"}, {Code: '@', Text: "@"}, {Code: 'b', Text: "b"}, {Code: tea.KeyTab}, {Code: tea.KeyTab}} {
			updated, _ = m.Update(key)
			m = updated.(model)
		}
		typed := ansi.Strip(m.View().Content)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(model)
		events := uis.TakeEvents()

		if !strings.Contains(typed, "a@b") || m.uiOpen != 0 || len(events) != 1 {
			t.Fatalf("open = %d events = %+v screen:\n%s", m.uiOpen, events, typed)
		}
		if note := events[0].Note(); note != `UI 1 "Sign up": submitted with ok; values: email="a@b"` {
			t.Errorf("note = %q", note)
		}
	})

	t.Run("UD-03 a click outside hides the dialog; close removes it and tells the model", func(t *testing.T) {
		m, uis := uiModel(t)
		updated, _ := m.openUI(1)
		hidden, _ := updated.(model).Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}))
		stillListed := len(uis.List())
		reopened, _ := hidden.(model).openUI(1)
		m = reopened.(model)
		frame, _ := m.uiFrame()
		var cancel uiHit
		for _, hit := range frame.hits {
			if hit.id == "cancel" {
				cancel = hit
			}
		}
		closed, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: cancel.x0, Y: cancel.y0, Button: tea.MouseLeft}))
		events := uis.TakeEvents()

		if hidden.(model).uiOpen != 0 || stillListed != 1 {
			t.Error("a click outside should only hide it")
		}
		if closed.(model).uiOpen != 0 || len(uis.List()) != 0 || len(events) != 1 || events[0].Action != tools.UIClose {
			t.Errorf("closed: open = %d left = %d events = %+v", closed.(model).uiOpen, len(uis.List()), events)
		}
	})
}
