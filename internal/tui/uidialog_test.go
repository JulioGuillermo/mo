package tui

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | UD-01 | Happy    | The model's interface is listed in the panel, new until opened as a dialog | P0      |
// | UD-02 | Happy    | Typing fills the focused input; submit sends every value to the model    | P0       |
// | UD-03 | Happy    | A click outside or close hides the dialog; remove takes it away and tells the model | P1 |
// | UD-04 | Happy    | A script reacts to typing in the dialog; its error shows under the interface | P0   |

import (
	"context"
	"encoding/json"
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
	uis := tools.NewUIs("")
	uis.SetConversation("c1")
	m.session.Agent = &agent.Agent{Tools: tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Jobs: m.jobs(), UIs: uis}, tools.Options{})}
	m.running = true
	tree := `{"type":"card","title":"Sign up","children":[
	  {"type":"input","id":"email","placeholder":"you@example.com"},
	  {"type":"panel","direction":"row","gap":2,"children":[
	    {"type":"button","id":"cancel","label":"Cancel","on_click":"close"},
	    {"type":"button","id":"drop","label":"Drop","on_click":"remove"},
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
		for _, key := range []tea.KeyPressMsg{{Code: 'a', Text: "a"}, {Code: '@', Text: "@"}, {Code: 'b', Text: "b"}, {Code: tea.KeyTab}, {Code: tea.KeyTab}, {Code: tea.KeyTab}} {
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

	t.Run("UD-03 a click outside or close hides the dialog; remove takes it away and tells the model", func(t *testing.T) {
		m, uis := uiModel(t)
		updated, _ := m.openUI(1)
		hidden, _ := updated.(model).Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}))
		stillListed := len(uis.List())
		reopened, _ := hidden.(model).openUI(1)
		m = reopened.(model)
		frame, _ := m.uiFrame()
		hits := map[string]uiHit{}
		for _, hit := range frame.hits {
			hits[hit.id] = hit
		}
		closed, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: hits["cancel"].x0, Y: hits["cancel"].y0, Button: tea.MouseLeft}))
		afterClose, told := len(uis.List()), uis.HasEvents()
		reopened, _ = closed.(model).openUI(1)
		removed, _ := reopened.(model).Update(tea.MouseClickMsg(tea.Mouse{X: hits["drop"].x0, Y: hits["drop"].y0, Button: tea.MouseLeft}))
		events := uis.TakeEvents()

		if hidden.(model).uiOpen != 0 || stillListed != 1 {
			t.Error("a click outside should only hide it")
		}
		if closed.(model).uiOpen != 0 || afterClose != 1 || told {
			t.Errorf("close should only hide it: open = %d listed = %d told = %v", closed.(model).uiOpen, afterClose, told)
		}
		if removed.(model).uiOpen != 0 || len(uis.List()) != 0 || len(events) != 1 || events[0].Action != tools.UIRemove {
			t.Errorf("removed: open = %d left = %d events = %+v", removed.(model).uiOpen, len(uis.List()), events)
		}
	})

	t.Run("UD-04 a script reacts to typing in the dialog; its error shows under the interface", func(t *testing.T) {
		m, uis := uiModel(t)
		tree := `{"type":"panel","children":[
		  {"type":"input","id":"name","on_input":"check"},
		  {"type":"label","id":"hint","text":"too short","visible":false},
		  {"type":"button","id":"boom","label":"Boom","on_click":"boom"}]}`
		script := "function check(e) ui.set('hint', {visible = #e.value < 3}) end\nfunction boom() error('it broke') end"
		args, _ := json.Marshal(map[string]any{"action": "create", "title": "Name", "tree": json.RawMessage(tree), "script": script})
		if output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args}); isError {
			t.Fatal(output)
		}
		updated, _ := m.openUI(2)
		m = updated.(model)
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		m = updated.(model)
		short := ansi.Strip(m.View().Content)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		updated, _ = updated.(model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(model)
		broken := ansi.Strip(m.View().Content)

		if !strings.Contains(short, "too short") {
			t.Errorf("the hint should show:\n%s", short)
		}
		if !strings.Contains(broken, "script:") || !strings.Contains(broken, "it broke") || !uis.HasEvents() {
			t.Errorf("the error should show and be told:\n%s", broken)
		}
	})
}
