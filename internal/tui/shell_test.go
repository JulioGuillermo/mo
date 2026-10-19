package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | SH-01 | Happy    | !command runs in the working directory and shows as a bash call with its output | P0    |
// | SH-02 | Happy    | The model gets the command and what it printed with the next message, once  | P0       |
// | SH-03 | Edge     | A failing command shows as failed and the model is told how it ended         | P1       |
// | SH-04 | Happy    | esc stops a command still running                                            | P1       |
// | SH-05 | Edge     | ! alone says how it is used                                                  | P2       |

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func runShellLine(t *testing.T, m model, line string) model {
	t.Helper()
	updated, cmd := m.submitted(line)
	return deliver(updated.(model), cmd, 2)
}

func TestShell(t *testing.T) {
	t.Run("SH-01 !command runs in the working directory and shows as a bash call with its output", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()

		m = runShellLine(t, m, "!pwd && echo hello")

		if text := shown(m); !strings.Contains(text, "bash") || !strings.Contains(text, "/tmp/TestShell") || !strings.Contains(text, "hello") {
			t.Errorf("shown:\n%s", text)
		}
	})

	t.Run("SH-02 the model gets the command and what it printed with the next message, once", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()
		m = runShellLine(t, m, "!echo hello")

		first := m.withShellNotes("what do you see?")
		second := m.withShellNotes("and now?")

		if !strings.Contains(first, "I ran `echo hello` in the shell; it ended well") || !strings.Contains(first, "hello\n```\n\nwhat do you see?") || second != "and now?" {
			t.Errorf("first = %q second = %q", first, second)
		}
	})

	t.Run("SH-03 a failing command shows as failed and the model is told how it ended", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()

		m = runShellLine(t, m, "!echo oops; exit 3")

		if text := shown(m); !strings.Contains(text, "exit status 3") {
			t.Errorf("shown:\n%s", text)
		}
		if note := m.withShellNotes("?"); !strings.Contains(note, "exit status 3") || !strings.Contains(note, "oops") {
			t.Errorf("note = %q", note)
		}
	})

	t.Run("SH-04 esc stops a command still running", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()
		updated, cmd := m.submitted("!sleep 30")
		m = updated.(model)
		done := make(chan tea.Msg)
		go func() { done <- cmd() }()

		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(model)

		select {
		case msg := <-done:
			updated, _ = m.Update(msg)
			m = updated.(model)
		case <-time.After(5 * time.Second):
			t.Fatal("still running")
		}
		if len(m.shells) != 0 || !strings.Contains(shown(m), "stopped with esc") {
			t.Errorf("shells = %d shown:\n%s", len(m.shells), shown(m))
		}
	})

	t.Run("SH-05 ! alone says how it is used", func(t *testing.T) {
		m := conversationModel()

		m = runShellLine(t, m, "!")

		if !strings.Contains(shown(m), "!<command> runs it in the shell") {
			t.Errorf("shown:\n%s", shown(m))
		}
	})
}
