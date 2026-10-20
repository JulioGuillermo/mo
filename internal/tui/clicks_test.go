package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | CK-01 | Happy    | Clicking a dialog button presses it; clicking a tab shows that part  | P0       |
// | CK-02 | Happy    | Clicking an option answers the question; on a multiple one it ticks  | P1       |
// | CK-03 | Happy    | Clicking a suggested command runs it                                 | P1       |
// | CK-04 | Happy    | Clicking the mode goes to the next; the model asks for models to pick | P1       |
// | CK-05 | Happy    | Clicking an option of a settings list opens it                       | P1       |
// | CK-06 | Happy    | Clicking a job in the side panel shows its last lines                | P2       |
// | CK-07 | Happy    | Buttons underline the key that presses them                          | P2       |

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

// clickOn clicks the first place on screen showing text, as a user would.
func clickOn(t *testing.T, m model, text string) model {
	t.Helper()
	return clickOnFor(t, m, text, 3)
}

// clickOnFor clicks text and delivers what follows for as many rounds.
func clickOnFor(t *testing.T, m model, text string, rounds int) model {
	t.Helper()
	view := m.View().Content
	if m.settings != nil {
		view = m.settings.form.View()
	}
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		if x := strings.Index(line, text); x >= 0 {
			at := tea.Mouse{X: ansi.StringWidth(line[:x]) + 1, Y: y, Button: tea.MouseLeft}
			pressed, cmd := m.Update(tea.MouseClickMsg(at))
			m = deliver(pressed.(model), cmd, rounds)
			updated, cmd := m.Update(tea.MouseReleaseMsg(at))
			return deliver(updated.(model), cmd, rounds)
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, ansi.Strip(view))
	return m
}

func TestClicks(t *testing.T) {
	t.Run("CK-01 clicking a dialog button presses it; clicking a tab shows that part", func(t *testing.T) {
		m, reply := asking(agent.Interaction{
			Approvals: []agent.ApprovalRequest{bash("make"), {Call: llm.ToolCall{Name: "rm"}, Summary: "build"}},
		})
		m.width, m.height = 100, 40

		m = clickOn(t, m, "⚑ rm")
		onRm := m.dialog.current
		m = clickOn(t, m, "Yes")
		m = clickOn(t, m, "No")

		got := receive(t, reply)
		if onRm != 1 || got.Decisions[1] != agent.Allow || got.Decisions[0] != agent.Deny {
			t.Errorf("on rm = %v decisions = %v", onRm == 1, got.Decisions)
		}
	})

	t.Run("CK-02 clicking an option answers the question; on a multiple one it ticks", func(t *testing.T) {
		m, reply := asking(agent.Interaction{Questions: []tools.Question{
			{Text: "Db?", Options: []string{"Postgres", "SQLite"}},
			{Text: "Parts?", Options: []string{"api", "ui"}, Multiple: true},
		}})
		m.width, m.height = 100, 40

		m = clickOn(t, m, "SQLite")
		m = clickOn(t, m, "ui")
		ticked := m.dialog.item().picked[1]
		m = keys(t, m, keyEnter)

		got := receive(t, reply)
		if got.Answers[0] != "SQLite" || !ticked || got.Answers[1] != "ui" {
			t.Errorf("answers = %q ticked = %v", got.Answers, ticked)
		}
	})

	t.Run("CK-03 clicking a suggested command runs it", func(t *testing.T) {
		m := sidebarModel(t, 140)
		m.input.SetValue("/side")

		m = clickOn(t, m, "/sidebar")

		if m.sidebarShown() || m.input.Value() != "" {
			t.Errorf("shown = %v input = %q", m.sidebarShown(), m.input.Value())
		}
	})

	t.Run("CK-04 clicking the mode goes to the next; the model asks for the models to pick from", func(t *testing.T) {
		m := sidebarModel(t, 140)
		m.session.Agent.Permissions, _ = permissions.New(permissions.ModeEdits, nil, nil)
		m.session.Model = "qwen3-coder"

		m = clickOn(t, m, "● edits")
		modeAfter := m.session.Agent.Permissions.Mode()
		m = clickOn(t, m, "qwen3-coder")

		if modeAfter != permissions.ModeAuto || m.settings == nil || !strings.Contains(ansi.Strip(m.settings.form.View()), "Model · ") {
			t.Errorf("mode = %s model screen = %v", modeAfter, m.settings != nil)
		}
	})

	t.Run("CK-05 clicking an option of a settings list opens it", func(t *testing.T) {
		m, _ := menuModel()

		m = clickOn(t, m, "Safety ›")
		m = clickOn(t, m, "Watch for loops")

		if m.settings == nil || !strings.Contains(m.settings.form.View(), "Watch for loops") || strings.Contains(m.settings.form.View(), "‹ back") {
			t.Errorf("view:\n%s", ansi.Strip(m.settings.form.View()))
		}
	})

	t.Run("CK-06 clicking a job in the side panel shows its last lines", func(t *testing.T) {
		m := sidebarModel(t, 140)
		jobs := m.jobs()
		ended := make(chan tools.JobStatus, 1)
		jobs.OnEnd(func(status tools.JobStatus) { ended <- status })
		_, _ = jobs.Start(tools.Env{WorkDir: t.TempDir()}, "echo compiled ok")
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the job did not end")
		}

		m = clickOn(t, m, "2 echo compiled ok")

		if !strings.Contains(shown(m), "job 2 done") || !strings.Contains(shown(m), "compiled ok") {
			t.Errorf("shown:\n%s", shown(m))
		}
	})

	t.Run("CK-07 buttons underline the key that presses them", func(t *testing.T) {
		button := buttonLabel("Yes", false, dialogPalette(true))

		if ansi.Strip(button) != "  Yes  " || !strings.Contains(button, ";4") && !strings.Contains(button, "[4") {
			t.Errorf("button = %q", button)
		}
	})
}

// deliver feeds back the messages of a command, as the program would, a few
// rounds deep: huh ends a choice through one.
func deliver(m model, cmd tea.Cmd, rounds int) model {
	if cmd == nil || rounds == 0 {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, each := range batch {
			m = deliver(m, each, rounds-1)
		}
		return m
	}
	if msg == nil {
		return m
	}
	updated, next := m.Update(msg)
	return deliver(updated.(model), next, rounds-1)
}

// mouseClick presses and lets go at a cell, as a click arrives.
func mouseClick(m model, x, y int) tea.Model {
	at := tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}
	pressed, _ := m.Update(tea.MouseClickMsg(at))
	released, _ := pressed.(model).Update(tea.MouseReleaseMsg(at))
	return released
}
