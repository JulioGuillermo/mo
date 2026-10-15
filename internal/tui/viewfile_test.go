package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VF-01 | Happy    | tab after /view offers folders then files; a folder fills in, a file runs | P0      |
// | VF-02 | Happy    | /view on code opens it highlighted, numbered, for the user only          | P0       |
// | VF-03 | Edge     | A folder, a binary file or a picture are not opened as text             | P1       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func viewModel(t *testing.T) model {
	t.Helper()
	m := conversationModel()
	m.width, m.height = 100, 30
	m.session.WorkDir = t.TempDir()
	for name, content := range map[string]string{"main.go": "package main\n\nfunc main() {}\n", "internal/a.txt": "a", ".hidden": "x", "blob.bin": "a\x00b"} {
		path := filepath.Join(m.session.WorkDir, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(content), 0o644)
	}
	return m
}

func TestViewFile(t *testing.T) {
	t.Run("VF-01 tab after /view offers folders then files; a folder fills in, a file runs", func(t *testing.T) {
		m := viewModel(t)
		m.input.SetValue("/view ")
		var names []string
		for _, each := range m.suggestions() {
			names = append(names, each.name)
		}
		if strings.Join(names, ",") != "view internal/,view blob.bin,view main.go" {
			t.Fatalf("names = %v", names)
		}
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = updated.(model)
		if m.input.Value() != "/view internal/" || len(m.suggestions()) != 1 || m.suggestions()[0].name != "view internal/a.txt" {
			t.Errorf("input = %q suggestions = %v", m.input.Value(), m.suggestions())
		}
		m.input.SetValue("/view ma")
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if viewer := updated.(model).viewer; viewer == nil || viewer.file != "main.go" {
			t.Error("enter on a file should open it")
		}
	})

	t.Run("VF-02 /view on code opens it highlighted, numbered, for the user only", func(t *testing.T) {
		m := viewModel(t)
		history := len(m.session.Agent.History)
		opened, _ := m.runCommand("/view main.go")
		m = opened.(model)
		screen := m.View().Content

		if m.viewer == nil || m.viewer.colored == nil || m.viewer.language != "Go" || len(m.session.Agent.History) != history {
			t.Fatalf("viewer = %+v", m.viewer)
		}
		if plain := ansi.Strip(screen); !strings.Contains(plain, "main.go") || !strings.Contains(plain, "only you see it") || !strings.Contains(plain, "1 package main") {
			t.Errorf("screen:\n%s", plain)
		}
		if !strings.Contains(screen, "\x1b[") {
			t.Error("the code should be coloured")
		}
	})

	t.Run("VF-03 a folder, a binary file or a picture are not opened as text", func(t *testing.T) {
		m := viewModel(t)
		folder, _ := m.runCommand("/view internal")
		binary, _ := m.runCommand("/view blob.bin")

		if folder.(model).viewer != nil || !strings.Contains(shown(folder.(model)), "is a folder") {
			t.Error("a folder should be refused")
		}
		if binary.(model).viewer != nil || !strings.Contains(shown(binary.(model)), "is not text") {
			t.Error("a binary file should be refused")
		}
		if !drawnInline("chart.svg") || !drawnInline("flow.mmd") || !drawnInline("pic.PNG") || drawnInline("main.go") {
			t.Error("pictures, SVG and Mermaid are drawn in the conversation")
		}
	})
}
