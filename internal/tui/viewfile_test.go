package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VF-01 | Happy    | tab after /view offers folders then files; a folder fills in, a file runs | P0      |
// | VF-02 | Happy    | /view on code opens it highlighted, numbered, for the user only          | P0       |
// | VF-04 | Happy    | A file without an extension is highlighted as its #! line says             | P1       |
// | VF-05 | Happy    | The cursor's line keeps its highlighting under the cursor's background     | P1       |
// | VF-03 | Edge     | A folder is browsed; a binary file or a picture are not opened as text             | P1       |

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

		if m.viewer == nil || !m.viewer.coloured() || m.viewer.language != "Go" || len(m.session.Agent.History) != history {
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

		if viewer := folder.(model).viewer; viewer == nil || !viewer.placeholder || viewer.tree == nil || viewer.file != "" {
			t.Errorf("a folder is browsed in a tree, not opened as text: %+v", viewer)
		}
		if binary.(model).viewer != nil || !strings.Contains(shown(binary.(model)), "is not text") {
			t.Error("a binary file should be refused")
		}
		if !drawnInline("chart.svg") || !drawnInline("flow.mmd") || !drawnInline("pic.PNG") || drawnInline("main.go") {
			t.Error("pictures, SVG and Mermaid are drawn in the conversation")
		}
	})

	t.Run("VF-04 a file without an extension is highlighted as its #! line says", func(t *testing.T) {
		m := viewModel(t)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "deploy"), []byte("#!/bin/bash\nset -e\necho \"deploying $1\"\n"), 0o755)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "tool"), []byte("#!/usr/bin/env python3\nprint('hi')\n"), 0o755)

		bash, _ := m.runCommand("/view deploy")
		python, _ := m.runCommand("/view tool")

		if viewer := bash.(model).viewer; viewer == nil || viewer.language != "Bash" || !viewer.coloured() {
			t.Errorf("bash viewer = %+v", viewer)
		}
		if viewer := python.(model).viewer; viewer == nil || !strings.HasPrefix(viewer.language, "Python") {
			t.Errorf("python language = %q", python.(model).viewer.language)
		}
	})

	t.Run("VF-05 the cursor's line keeps its highlighting under the cursor's background", func(t *testing.T) {
		opened, _ := viewModel(t).runCommand("/view main.go")
		lines := strings.Split(opened.(model).View().Content, "\n")
		cursor := lines[1]

		if !strings.Contains(cursor, cursorBackground) || !strings.Contains(cursor, "38;2;") || !strings.Contains(ansi.Strip(cursor), "package main") {
			t.Errorf("cursor line = %q", cursor)
		}
		reset := strings.LastIndex(cursor[:len(cursor)-len("\x1b[m")], "\x1b[m")
		if reset >= 0 && !strings.HasPrefix(cursor[reset+len("\x1b[m"):], cursorBackground) {
			t.Errorf("the background should come back after each reset: %q", cursor)
		}
	})
}
