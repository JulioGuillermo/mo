package tui

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | FC-01 | Happy    | c comments the cursor's line, shown marked; d deletes it                  | P0       |
// | FC-02 | Happy    | Lines dragged over get one comment together                               | P1       |
// | FC-03 | Happy    | a attaches the file with its comments, which the model gets with the lines | P0      |
// | FC-04 | Happy    | Without comments a attaches the file alone                                | P1       |
// | FC-05 | Happy    | @ offers the files that match, the name's first; tab attaches one          | P0       |
// | FC-06 | Happy    | shift+enter starts a new line; the comment keeps its lines, the model gets them indented | P0 |
// | FC-07 | Happy    | A comment longer than the line wraps, its end in view as it is typed        | P0       |
// | FC-08 | Happy    | The arrows move in the comment, and typing goes where the cursor is        | P1       |
// | FC-09 | Edge     | Past three lines the comment scrolls inside; the line commented stays in view above it | P0 |
// | FC-10 | Happy    | v then the arrows select lines; c comments them together and ends the selection | P0 |
// | FC-11 | Happy    | shift+↑↓ select from the cursor; y copies the lines selected                | P1       |
// | FC-12 | Edge     | The arrows alone, or esc, let a selection go; c comments the cursor's line again | P1 |

import (
	"fmt"
	"os"
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
		if !strings.Contains(screen, "◆3 func main() {}") || !strings.Contains(screen, "◆ rename this") {
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

	t.Run("FC-06 shift+enter starts a new line; the comment keeps its lines, the model gets them indented", func(t *testing.T) {
		m := typeText(t, openMain(t), "jjcfirst")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		m = typeText(t, m, "second")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

		comments := m.comments()
		told := commentNote("main.go", strings.Split("package main\n\nfunc main() {}", "\n"), comments)

		if len(comments) != 1 || comments[0].text != "first\nsecond" {
			t.Fatalf("comments = %+v", comments)
		}
		if !strings.Contains(told, "first\n  second") {
			t.Errorf("told:\n%s", told)
		}
	})

	t.Run("FC-07 a comment longer than the line wraps, its end in view as it is typed", func(t *testing.T) {
		long := strings.Repeat("word ", 30) + "theend"
		m := typeText(t, typeText(t, openMain(t), "c"), long)

		screen := ansi.Strip(m.View().Content)

		if !strings.Contains(screen, "theend") || strings.Contains(screen, "…") || m.inputRows() < 2 {
			t.Errorf("rows %d screen:\n%s", m.inputRows(), screen)
		}
	})

	t.Run("FC-08 the arrows move in the comment, and typing goes where the cursor is", func(t *testing.T) {
		m := typeText(t, typeText(t, openMain(t), "c"), "world")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
		m = typeText(t, m, "hello ")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

		if comments := m.comments(); len(comments) != 1 || comments[0].text != "hello world" {
			t.Errorf("comments = %+v", comments)
		}
	})

	t.Run("FC-09 past three lines the comment scrolls inside; the line commented stays in view above it", func(t *testing.T) {
		m := viewModel(t)
		m.height = 10
		var lines []string
		for i := range 40 {
			lines = append(lines, fmt.Sprintf("line number %d", i+1))
		}
		if err := os.WriteFile(filepath.Join(m.session.WorkDir, "long.txt"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		opened, _ := m.runCommand("/view long.txt")
		m = opened.(model)
		m = typeText(t, m, strings.Repeat("j", 39)+"c")
		for i := range 5 {
			m = typeText(t, m, fmt.Sprintf("note %d", i+1))
			m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		}

		screen := ansi.Strip(m.View().Content)

		if m.inputRows() != commentInputLines || !strings.Contains(screen, "line number 40") || !strings.Contains(screen, "note 5") || strings.Contains(screen, "note 1") {
			t.Errorf("rows %d screen:\n%s", m.inputRows(), screen)
		}
		if height := strings.Count(screen, "\n") + 1; height > 10 {
			t.Errorf("the view is %d rows on a screen of 10", height)
		}
	})

	t.Run("FC-10 v then the arrows select lines; c comments them together and ends the selection", func(t *testing.T) {
		m := typeText(t, openMain(t), "vjj")
		selected := strings.Count(m.View().Content, viewerPicked.Render("func main() {}"))
		m = typeText(t, m, "cthese three")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

		comments := m.comments()

		if selected != 1 || len(comments) != 1 || comments[0].from != 0 || comments[0].to != 2 || comments[0].text != "these three" {
			t.Errorf("selected shown %d comments %+v", selected, comments)
		}
		if m.viewer.selecting || m.viewer.selected {
			t.Error("the selection should end with the comment")
		}
	})

	t.Run("FC-11 shift+↑↓ select from the cursor; y copies the lines selected", func(t *testing.T) {
		m := typeText(t, openMain(t), "j")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
		from, to, ok := m.viewer.selectedLines()
		_, cmd := m.viewerKey("y")

		if !ok || from != 1 || to != 2 || cmd == nil || m.viewer.cursor != 2 {
			t.Errorf("selected %d-%d %v, cursor %d", from, to, ok, m.viewer.cursor)
		}
	})

	t.Run("FC-12 the arrows alone, or esc, let a selection go; c comments the cursor's line again", func(t *testing.T) {
		shifted := keys(t, openMain(t), tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
		moved := typeText(t, shifted, "j")
		escaped := keys(t, typeText(t, openMain(t), "vj"), tea.KeyPressMsg{Code: tea.KeyEscape})

		if moved.viewer.selected || escaped.viewer.selected || escaped.viewer.selecting || escaped.viewer == nil {
			t.Fatalf("moved %v escaped %v %v", moved.viewer.selected, escaped.viewer.selected, escaped.viewer.selecting)
		}
		m := typeText(t, moved, "cone")
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if comments := m.comments(); len(comments) != 1 || comments[0].from != 2 || comments[0].to != 2 {
			t.Errorf("comments = %+v", comments)
		}
	})
}
