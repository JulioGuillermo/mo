package tui

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | FC-01 | Happy    | c comments the cursor's line, shown marked; d deletes it                  | P0       |
// | FC-02 | Happy    | Lines dragged over get one comment together                               | P1       |
// | FC-03 | Happy    | a attaches the file with its comments, which the model gets with the lines | P0      |
// | FC-04 | Happy    | Without comments a attaches the file alone                                | P1       |
// | FC-05 | Happy    | @ offers the files that match, the name's first; tab attaches one          | P0       |

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func typeText(t *testing.T, m model, text string) model {
	t.Helper()
	for _, r := range text {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(model)
	}
	return m
}

func openMain(t *testing.T) model {
	t.Helper()
	opened, _ := viewModel(t).runCommand("/view main.go")
	return opened.(model)
}

func TestFileComments(t *testing.T) {
	t.Run("FC-01 c comments the cursor's line, shown marked; d deletes it", func(t *testing.T) {
		m := typeText(t, openMain(t), "jjc")
		m = typeText(t, m, "rename this")
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(model)
		screen := ansi.Strip(m.View().Content)
		comments := m.comments()
		deleted := typeText(t, m, "d")

		if len(comments) != 1 || comments[0].from != 2 || comments[0].text != "rename this" {
			t.Fatalf("comments = %+v", comments)
		}
		if !strings.Contains(screen, "3◆func main() {}") || !strings.Contains(screen, "◆ rename this") {
			t.Errorf("screen:\n%s", screen)
		}
		if len(deleted.comments()) != 0 {
			t.Error("d should delete it")
		}
	})

	t.Run("FC-02 lines dragged over get one comment together", func(t *testing.T) {
		m := openMain(t)
		for _, msg := range []tea.Msg{
			tea.MouseClickMsg(tea.Mouse{X: 5, Y: 1, Button: tea.MouseLeft}),
			tea.MouseMotionMsg(tea.Mouse{X: 5, Y: 3, Button: tea.MouseLeft}),
			tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: 3, Button: tea.MouseLeft}),
		} {
			updated, _ := m.Update(msg)
			m = updated.(model)
		}
		m = typeText(t, m, "cwhole block")
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		comments := updated.(model).comments()

		if len(comments) != 1 || comments[0].from != 0 || comments[0].to != 2 {
			t.Errorf("comments = %+v", comments)
		}
	})

	t.Run("FC-03 a attaches the file with its comments, which the model gets with the lines", func(t *testing.T) {
		m := typeText(t, openMain(t), "jjcrename this")
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = typeText(t, updated.(model), "a")
		input := m.input.Value()
		sent := m.withAttachments("please " + strings.TrimSpace(input))

		if m.viewer != nil || input != "[comments main.go] " {
			t.Fatalf("viewer open = %v input = %q", m.viewer != nil, input)
		}
		path := filepath.Join(m.session.WorkDir, "main.go")
		if !strings.Contains(sent, "please "+path) || !strings.Contains(sent, "The user's comments on main.go:\n- line 3 (`func main() {}`): rename this") {
			t.Errorf("sent = %q", sent)
		}
		if len(m.fileComments["main.go"]) != 0 {
			t.Error("comments sent should be dropped")
		}
	})

	t.Run("FC-04 without comments a attaches the file alone", func(t *testing.T) {
		m := typeText(t, openMain(t), "a")
		if m.input.Value() != "[file main.go] " {
			t.Errorf("input = %q", m.input.Value())
		}
	})

	t.Run("FC-05 @ offers the files that match, the name's first; tab attaches one", func(t *testing.T) {
		m := viewModel(t)
		m = typeText(t, m, "look at @a")
		var names []string
		for _, each := range m.suggestions() {
			names = append(names, each.name)
		}
		if len(names) < 2 || names[0] != "internal/a.txt" {
			t.Fatalf("names = %v", names)
		}
		m.input.SetValue("look at @mai")
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = updated.(model)

		if m.input.Value() != "look at [file main.go] " || !strings.Contains(m.withAttachments(m.input.Value()), filepath.Join(m.session.WorkDir, "main.go")) {
			t.Errorf("input = %q", m.input.Value())
		}
		if strings.Contains(ansi.Strip(m.View().Content), "@") && len(m.suggestions()) > 0 {
			t.Error("suggestions should be gone once picked")
		}
	})
}
