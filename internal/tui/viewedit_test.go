package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VE-01 | Happy    | i edits the cursor's line; enter writes it to the file and shows it      | P0       |
// | VE-02 | Happy    | A new line typed goes in, and the comments below move down with their lines | P0    |
// | VE-03 | Happy    | v selects lines; i edits them together                                   | P1       |
// | VE-04 | Edge     | Tabs of the indentation show as spaces and are written back as tabs; CRLF kept | P0 |
// | VE-05 | Edge     | esc leaves the file as it was                                            | P1       |
// | VE-06 | Security | A file changed on disk since it was opened is not overwritten            | P0       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var keyNewLine = tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}

func editFile(t *testing.T, content string) (model, string) {
	t.Helper()
	m := viewModel(t)
	path := filepath.Join(m.session.WorkDir, "edit.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, _ := m.runCommand("/view edit.go")
	return opened.(model), path
}

func onDisk(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestViewerEdit(t *testing.T) {
	t.Run("VE-01 i edits the cursor's line; enter writes it to the file and shows it", func(t *testing.T) {
		m, path := editFile(t, "one\ntwo\nthree\n")
		m = typeText(t, m, "ji")
		m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnd})
		m = typeText(t, m, "!")
		m, _ = press(m, keyEnter)

		if got := onDisk(t, path); got != "one\ntwo!\nthree\n" {
			t.Errorf("file = %q", got)
		}
		if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "2 two!") || !strings.Contains(screen, "saved line 2") {
			t.Errorf("screen:\n%s", screen)
		}
	})

	t.Run("VE-02 a new line typed goes in, and the comments below move down with their lines", func(t *testing.T) {
		m, path := editFile(t, "one\ntwo\nthree\n")
		m = typeText(t, m, "jjcnote")
		m, _ = press(m, keyEnter)
		m = typeText(t, m, "kki")
		m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnd})
		m, _ = press(m, keyNewLine)
		m = typeText(t, m, "added")
		m, _ = press(m, keyEnter)

		if got := onDisk(t, path); got != "one\nadded\ntwo\nthree\n" {
			t.Errorf("file = %q", got)
		}
		if comments := m.comments(); len(comments) != 1 || comments[0].from != 3 {
			t.Errorf("comments = %+v", comments)
		}
	})

	t.Run("VE-03 v selects lines; i edits them together", func(t *testing.T) {
		m, path := editFile(t, "one\ntwo\nthree\n")
		m = typeText(t, m, "vji")
		if got := m.viewer.input.Value(); got != "one\ntwo" {
			t.Fatalf("input = %q", got)
		}
		m.viewer.input.SetValue("both")
		m, _ = press(m, keyEnter)

		if got := onDisk(t, path); got != "both\nthree\n" {
			t.Errorf("file = %q", got)
		}
	})

	t.Run("VE-04 tabs of the indentation show as spaces and are written back as tabs; CRLF kept", func(t *testing.T) {
		m, path := editFile(t, "func a() {\r\n\tif x {\r\n\t\ty()\r\n\t}\r\n}\r\n")
		m = typeText(t, m, "jvji")
		if got := m.viewer.input.Value(); got != "    if x {\n        y()" {
			t.Fatalf("input = %q", got)
		}
		m.viewer.input.SetValue("    if x {\n        y()\n        z()")
		m, _ = press(m, keyEnter)

		if got := onDisk(t, path); got != "func a() {\r\n\tif x {\r\n\t\ty()\r\n\t\tz()\r\n\t}\r\n}\r\n" {
			t.Errorf("file = %q", got)
		}
	})

	t.Run("VE-05 esc leaves the file as it was", func(t *testing.T) {
		m, path := editFile(t, "one\n")
		m = typeText(t, m, "ichanged")
		m, _ = press(m, keyEsc)

		if got := onDisk(t, path); got != "one\n" || m.viewer.editing {
			t.Errorf("file = %q, editing %v", got, m.viewer.editing)
		}
	})

	t.Run("VE-06 a file changed on disk since it was opened is not overwritten", func(t *testing.T) {
		m, path := editFile(t, "one\n")
		if err := os.WriteFile(path, []byte("from the model\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m = typeText(t, m, "imine")
		m, _ = press(m, keyEnter)

		if got := onDisk(t, path); got != "from the model\n" || !strings.Contains(m.viewer.notice, "changed since it was opened") {
			t.Errorf("file = %q, notice %q", got, m.viewer.notice)
		}
	})
}
