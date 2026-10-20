package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | SH-01 | Happy    | !command runs in the working directory and shows as a bash call with its output | P0    |
// | SH-02 | Happy    | With !!, the model gets the command, its exit code and what it printed, as a note of mo's after the next message, once | P0 |
// | SH-03 | Edge     | A failing command shows as failed and the model, with !!, is told its exit code | P1     |
// | SH-06 | Security | With a single !, the model gets nothing of the command                       | P0       |
// | SH-07 | Security | Files a command printed are never taken as attached; those the user wrote are | P0     |
// | SH-04 | Happy    | esc stops a command still running                                            | P1       |
// | SH-05 | Edge     | ! alone says how it is used                                                  | P2       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// stubProvider answers every request with ok.
type stubProvider struct{}

func (stubProvider) Stream(context.Context, llm.Request, func(llm.Event)) (llm.Response, error) {
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: "ok"}}, nil
}

func runShellLine(t *testing.T, m model, line string) model {
	t.Helper()
	updated, cmd := m.submitted(line)
	return deliver(updated.(model), cmd, 4)
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

	t.Run("SH-02 with !!, the model gets the command, its exit code and what it printed, as a note of mo's after the next message, once", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()
		m = runShellLine(t, m, "!!echo hello")

		first := m.withShellNotes("what do you see?")
		second := m.withShellNotes("and now?")

		want := "what do you see?\n\n" + agent.ShellNote("echo hello", "exit code 0", "hello\n")
		if first != want || second != "and now?" || !strings.Contains(first, "you did not run it") || !strings.Contains(shown(m), "the model reads this with your next message") {
			t.Errorf("first = %q second = %q", first, second)
		}
		if shown := agent.PromptText(llm.Message{Role: llm.RoleUser, Text: first}); shown != "what do you see?" {
			t.Errorf("shown as %q", shown)
		}
	})

	t.Run("SH-03 a failing command shows as failed and the model, with !!, is told its exit code", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()

		m = runShellLine(t, m, "!!echo oops; exit 3")

		if text := shown(m); !strings.Contains(text, "exit status 3") {
			t.Errorf("shown:\n%s", text)
		}
		if note := m.withShellNotes("?"); !strings.Contains(note, "exit code 3") || !strings.Contains(note, "oops") {
			t.Errorf("note = %q", note)
		}
	})

	t.Run("SH-06 with a single !, the model gets nothing of the command", func(t *testing.T) {
		m := conversationModel()
		m.session.WorkDir = t.TempDir()

		m = runShellLine(t, m, "!echo private")

		if sent := m.withShellNotes("hi"); sent != "hi" || !strings.Contains(shown(m), "private") || strings.Contains(shown(m), "the model reads this") {
			t.Errorf("sent %q shown:\n%s", sent, shown(m))
		}
	})

	t.Run("SH-07 files a command printed are never taken as attached; those the user wrote are", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644)
		os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("n"), 0o644)
		provider := &stubProvider{}
		assistant := &agent.Agent{Provider: provider, Tools: tools.NewRegistry(tools.Env{WorkDir: dir}, tools.Options{})}

		sent := "look at notes.txt\n\n" + agent.ShellNote("ls", "exit code 0", "go.mod\nnotes.txt\n")
		assistant.Run(context.Background(), sent, agent.Hooks{})

		text := assistant.History[0].Text
		if !strings.Contains(text, filepath.Join(dir, "notes.txt")) || strings.Contains(text, filepath.Join(dir, "go.mod")) {
			t.Errorf("attached:\n%s", text)
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
