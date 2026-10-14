package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | CM-01 | Happy    | A right click on the mode lists the modes; the one picked is the session's  | P1       |
// | CM-02 | Happy    | A right click on a task changes its state or deletes it, and the model is told at its next step | P0 |
// | CM-03 | Happy    | A right click on a job offers its output and how it stands, and kill while it runs | P1   |
// | CM-04 | Happy    | The panel scrolls with the wheel when its tasks and jobs do not fit, saying how many are hidden | P1 |
// | CM-05 | Happy    | With the side panel the context and the speeds sit at its bottom, not in the status line | P1 |

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

// rightClickOn right clicks the first place text is drawn.
func rightClickOn(t *testing.T, m model, text string) model {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if x := strings.Index(line, text); x >= 0 {
			updated, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: ansi.StringWidth(line[:x]) + 1, Y: y, Button: tea.MouseRight}))
			return deliver(updated.(model), cmd, 3)
		}
	}
	t.Fatalf("%q is not drawn", text)
	return m
}

// pick moves a menu's cursor to the option holding text and takes it.
func pick(t *testing.T, m model, text string) model {
	t.Helper()
	if m.settings == nil {
		t.Fatal("no menu open")
	}
	for range 12 {
		if strings.Contains(ansi.Strip(m.settings.form.View()), "> "+text) {
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			return deliver(updated.(model), cmd, 3)
		}
		updated, _ := m.Update(keyDown)
		m = updated.(model)
	}
	t.Fatalf("no option %q in:\n%s", text, m.settings.form.View())
	return m
}

func TestContextMenus(t *testing.T) {
	t.Run("CM-01 a right click on the mode lists the modes; the one picked is the session's", func(t *testing.T) {
		m := sidebarModel(t, 140)
		m.session.Agent.Permissions, _ = permissions.New(permissions.ModeEdits, nil, nil)

		m = rightClickOn(t, m, "● edits")
		menu := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "ask:")

		if !strings.Contains(menu, "edits: file changes go through, commands and fetches ask  ● now") || m.session.Agent.Permissions.Mode() != permissions.ModeAsk {
			t.Errorf("mode %s, menu:\n%s", m.session.Agent.Permissions.Mode(), menu)
		}
	})

	t.Run("CM-02 a right click on a task changes its state or deletes it, and the model is told at its next step", func(t *testing.T) {
		m := sidebarModel(t, 140)
		m.session.Agent.Provider = &replying{text: "ok"}

		m = rightClickOn(t, m, "read the code")
		m = pick(t, m, "mark it pending")
		m = rightClickOn(t, m, "write the sidebar")
		m = pick(t, m, "delete it")
		m.session.Agent.Run(t.Context(), "go on", agent.Hooks{})

		told := m.session.Agent.History[0].Text
		if len(m.todos) != 1 || m.todos[0].Status != tools.TodoPending {
			t.Errorf("todos = %+v", m.todos)
		}
		if !strings.Contains(told, `marked the task "read the code" pending`) || !strings.Contains(told, `deleted the task "write the sidebar`) || !strings.Contains(told, "- [pending] read the code") {
			t.Errorf("told:\n%s", told)
		}
	})

	t.Run("CM-03 a right click on a job offers its output and how it stands, and kill while it runs", func(t *testing.T) {
		m := sidebarModel(t, 140)

		m = rightClickOn(t, m, "sleep 30")
		menu := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "see its output")
		dialog := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "kill it")

		status, _, _ := m.jobs().Output(1, 0)
		if !strings.Contains(menu, "kill it") || !strings.Contains(dialog, "job 1 running for") || !strings.Contains(dialog, "refresh") || status.State != tools.JobKilled {
			t.Errorf("menu:\n%s\ndialog:\n%s\nstate %s", menu, dialog, status.State)
		}
	})

	t.Run("CM-04 the panel scrolls with the wheel when its tasks and jobs do not fit, saying how many are hidden", func(t *testing.T) {
		m := sidebarModel(t, 140)
		for i := range 30 {
			m.todos = append(m.todos, tools.Todo{Text: fmt.Sprintf("task number %d", i), Status: tools.TodoPending})
		}
		top := ansi.Strip(m.sidebar(20))
		updated, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{X: m.mainWidth() + 3, Y: 5, Button: tea.MouseWheelDown}))
		scrolledDown := ansi.Strip(updated.(model).sidebar(20))

		if !strings.Contains(top, "↓") || strings.Contains(top, "↑") || !strings.Contains(scrolledDown, "↑ 3 more") || strings.Count(scrolledDown, "\n") != 19 {
			t.Errorf("top:\n%s\nscrolled:\n%s", top, scrolledDown)
		}
	})

	t.Run("CM-05 with the side panel the context and the speeds move to it from the status line", func(t *testing.T) {
		m := sidebarModel(t, 140)
		m.session.Agent.Window = 32000
		m.usage = llm.Usage{InputTokens: 16000}
		m.speed = llm.Usage{PromptRate: 900, GenerationRate: 40}
		narrow := m
		narrow.width = 100

		wide, side := ansi.Strip(m.statusLine()), ansi.Strip(m.sidebar(30))

		bottom := strings.Split(strings.TrimRight(side, " \n"), "\n")
		if !strings.Contains(bottom[len(bottom)-1], "pp 900") || strings.Contains(side, "Context") {
			t.Errorf("not at the bottom:\n%s", side)
		}
		if strings.Contains(wide, "50%") || strings.Contains(wide, "pp ") || !strings.Contains(side, "50%") || !strings.Contains(side, "16.0k of 32.0k tokens") || !strings.Contains(side, "pp 900") {
			t.Errorf("status %q\nside:\n%s", wide, side)
		}
		if !strings.Contains(ansi.Strip(narrow.statusLine()), "50%") {
			t.Errorf("without the panel: %q", ansi.Strip(narrow.statusLine()))
		}
	})
}
