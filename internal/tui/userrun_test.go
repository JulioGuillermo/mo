package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | UR-01 | Happy    | A right click on a command run with ! offers to give it to the model, now or with the next message, and to copy it | P0 |
// | UR-02 | Happy    | Given with the next message, the model reads the command, its exit code and output once | P0 |
// | UR-03 | Happy    | Sent now, a turn starts with the run as its message, and it counts as told     | P0       |
// | UR-04 | Edge     | A run the model has already offers only copying; while the model works, no send now | P1 |
// | UR-05 | Edge     | A command still running, or a call of the model's, opens no such menu         | P1       |

import (
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

func TestUserRunMenu(t *testing.T) {
	ran := func(t *testing.T, line string) model {
		t.Helper()
		m := conversationModel()
		m.width, m.height = 120, 30
		m.session.WorkDir = t.TempDir()
		return runShellLine(t, m, line)
	}

	t.Run("UR-01 a right click on a command run with ! offers to give it to the model, now or with the next message, and to copy it", func(t *testing.T) {
		m := ran(t, "!echo private")

		m = rightClickOn(t, m, "private")

		if m.popup == nil || !slices.Equal(optionValues(m), []string{userRunTell, userRunSend, userRunCommand, userRunOutput}) || !strings.Contains(m.popup.description, "$ echo private") {
			t.Fatalf("popup %+v", m.popup)
		}
	})

	t.Run("UR-02 given with the next message, the model reads the command, its exit code and output once", func(t *testing.T) {
		m := rightClickOn(t, ran(t, "!echo private"), "private")

		m, _ = choose(t, m, userRunTell)

		first := m.withShellNotes("look")
		if want := "look\n\n" + agent.ShellNote("echo private", "exit code 0", "private\n"); first != want || m.withShellNotes("again") != "again" {
			t.Errorf("first %q", first)
		}
		if !strings.Contains(shown(m), "the model reads this with your next message") {
			t.Errorf("shown:\n%s", shown(m))
		}
	})

	t.Run("UR-03 sent now, a turn starts with the run as its message, and it counts as told", func(t *testing.T) {
		m := rightClickOn(t, ran(t, "!echo private"), "private")
		run := m.ranByUser["user-shell-1"]

		chosen, cmd := choose(t, m, userRunSend)

		if !chosen.running || cmd == nil || !run.told || len(chosen.shellNotes) != 0 {
			t.Errorf("running %v told %v notes %v", chosen.running, run.told, chosen.shellNotes)
		}
		if run.note != agent.ShellNote("echo private", "exit code 0", "private\n") {
			t.Errorf("note %q", run.note)
		}
	})

	t.Run("UR-04 a run the model has already offers only copying; while the model works, no send now", func(t *testing.T) {
		told := ran(t, "!!echo private")
		told = rightClickOn(t, told, "private")
		working := ran(t, "!echo private")
		working.running = true
		working = rightClickOn(t, working, "private")

		if !slices.Equal(optionValues(told), []string{userRunCommand, userRunOutput}) || !strings.Contains(told.popup.description, "the model has it") {
			t.Errorf("told %v %q", optionValues(told), told.popup.description)
		}
		if !slices.Equal(optionValues(working), []string{userRunTell, userRunCommand, userRunOutput}) {
			t.Errorf("working %v", optionValues(working))
		}
	})

	t.Run("UR-05 a command still running, or a call of the model's, opens no such menu", func(t *testing.T) {
		m := conversationModel()
		m.width, m.height = 120, 30
		m.addTool(llm.ToolCall{ID: "c1", Name: "bash", Args: []byte(`{"command":"echo model"}`)}, "echo model")
		m.finishTool("c1", "model output", false)

		_, isUserRun := m.userRunAt(cellOf(t, m, "model output").y)

		if isUserRun {
			t.Error("the model's call counted as the user's")
		}
	})
}
