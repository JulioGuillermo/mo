package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | LT-01 | Happy    | /tree shows the files at the left, with the keys, and narrows the conversation; again hides them | P0 |
// | LT-02 | Happy    | enter on a file opens the viewer with the same tree; esc comes back with the tree kept | P0 |
// | LT-03 | Happy    | esc gives the keys back to the input; ctrl+t takes them again                | P0       |
// | LT-04 | Happy    | A click on a folder opens it, one on a file shows it                         | P0       |
// | LT-05 | Happy    | A click past the tree counts the conversation's columns from its own left    | P1       |
// | LT-06 | Edge     | A file that does not open keeps the keys in the tree                         | P1       |
// | LT-07 | Edge     | A narrow terminal hides the tree, and no line is wider than the screen       | P1       |
// | LT-08 | Happy    | /tree on and off say which, again on keeps the tree; anything else gets a note | P0     |
// | LT-09 | Happy    | /tree width N sets the columns for the session, the tree of /view too; out of range changes nothing | P0 |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var keyEscMsg = tea.KeyPressMsg{Code: tea.KeyEscape}

func withTree(t *testing.T) model {
	t.Helper()
	m := searchModel(t)
	opened, _ := m.runCommand("/tree")
	return opened.(model)
}

func leftRow(m model, name string) int {
	for i, row := range m.leftTree.rows {
		if row.name == name {
			return i
		}
	}
	return -1
}

func TestLeftTree(t *testing.T) {
	t.Run("LT-01 /tree shows the files at the left, with the keys, and narrows the conversation; again hides them", func(t *testing.T) {
		m := withTree(t)

		plain := seen(m)
		first := strings.Split(plain, "\n")[1]

		if !m.leftTreeFocused() {
			t.Error("the tree should have the keys")
		}
		if m.mainWidth() != 100-m.treeWidth()-1 {
			t.Errorf("conversation width %d", m.mainWidth())
		}
		if !strings.HasPrefix(first, "▸ .hidden/") {
			t.Errorf("first row %q:\n%s", first, plain)
		}
		hidden, _ := m.runCommand("/tree")
		m = hidden.(model)
		if m.leftTree != nil || m.mainWidth() != 100 {
			t.Errorf("tree %v, width %d", m.leftTree, m.mainWidth())
		}
	})

	t.Run("LT-02 enter on a file opens the viewer with the same tree; esc comes back with the tree kept", func(t *testing.T) {
		m := withTree(t)
		tree := m.leftTree
		m.leftTree.cursor = leftRow(m, "mi_file.go")

		m = pressed(t, m, keyEnterTree)

		if m.viewer == nil || m.viewer.tree != tree || filepath.Base(m.viewer.file) != "mi_file.go" {
			t.Fatalf("viewer %+v", m.viewer)
		}
		if m.viewer.treeFocus || m.leftTreeFocused() {
			t.Error("the file should have the keys")
		}
		m = pressed(t, m, keyEscMsg)
		if m.viewer != nil || m.leftTree != tree || m.leftTreeFocused() {
			t.Errorf("viewer %v, tree kept %v, focused %v", m.viewer, m.leftTree == tree, m.leftTreeFocused())
		}
		if !strings.Contains(seen(m), "mi_file.go") {
			t.Error("the tree should still show")
		}
	})

	t.Run("LT-03 esc gives the keys back to the input; ctrl+t takes them again", func(t *testing.T) {
		m := withTree(t)

		m = pressed(t, m, keyEscMsg, letter("x"))
		typed := m.input.Value()
		m = pressed(t, m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}, keyDown)

		if typed != "x" {
			t.Errorf("input %q", typed)
		}
		if !m.leftTreeFocused() || m.leftTree.cursor != 1 {
			t.Errorf("focused %v, cursor %d", m.leftTreeFocused(), m.leftTree.cursor)
		}
	})

	t.Run("LT-04 a click on a folder opens it, one on a file shows it", func(t *testing.T) {
		m := withTree(t)
		docs := leftRow(m, "docs")

		clicked, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: docs + 1})
		m = clicked.(model)
		inside := leftRow(m, "MI_FILE.md")
		clicked, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: inside + 1})
		m = clicked.(model)

		if inside != docs+1 {
			t.Errorf("docs at %d, its file at %d", docs, inside)
		}
		if m.viewer == nil || filepath.Base(m.viewer.file) != "MI_FILE.md" {
			t.Errorf("viewer %+v", m.viewer)
		}
	})

	t.Run("LT-05 a click past the tree counts the conversation's columns from its own left", func(t *testing.T) {
		m := withTree(t)
		x := m.treeWidth() + 1 + 5

		clicked, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: 3})
		m = clicked.(model)

		if m.selected == nil || m.selected.from.x != 5 {
			t.Errorf("selection %+v", m.selected)
		}
		if m.leftTreeFocused() {
			t.Error("the click should take the keys from the tree")
		}
		if got := m.paneX(x, m.height-1); got != x {
			t.Errorf("the status line is the whole width: %d", got)
		}
	})

	t.Run("LT-06 a file that does not open keeps the keys in the tree", func(t *testing.T) {
		m := searchModel(t)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "blob.bin"), []byte{0, 1, 2}, 0o644)
		opened, _ := m.runCommand("/tree")
		m = opened.(model)
		m.leftTree.cursor = leftRow(m, "blob.bin")

		m = pressed(t, m, keyEnterTree)

		if m.viewer != nil || !m.leftTreeFocused() {
			t.Errorf("viewer %v, focused %v", m.viewer, m.leftTreeFocused())
		}
	})

	t.Run("LT-07 a narrow terminal hides the tree, and no line is wider than the screen", func(t *testing.T) {
		m := withTree(t)
		m.sidebarSession = sidebarOn
		m.width = 140
		for _, line := range strings.Split(seen(m), "\n") {
			if width := ansi.StringWidth(line); width > m.width {
				t.Errorf("line %d wide: %q", width, line)
			}
		}

		resized, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
		m = resized.(model)

		if m.leftTreeShown() || m.leftTreeFocused() || m.mainWidth() != 40 {
			t.Errorf("shown %v, focused %v, width %d", m.leftTreeShown(), m.leftTreeFocused(), m.mainWidth())
		}
	})
	t.Run("LT-08 /tree on and off say which, again on keeps the tree; anything else gets a note", func(t *testing.T) {
		m := withTree(t)
		tree := m.leftTree

		again, _ := m.runCommand("/tree on")
		kept := again.(model).leftTree
		off, _ := again.(model).runCommand("/tree off")
		stillOff, _ := off.(model).runCommand("/tree off")
		wrong, _ := stillOff.(model).runCommand("/tree sideways")

		if kept != tree {
			t.Error("/tree on with the tree shown should keep it")
		}
		if off.(model).leftTree != nil || stillOff.(model).leftTree != nil || wrong.(model).leftTree != nil {
			t.Error("/tree off should hide it, and stay off")
		}
		if !strings.Contains(seen(wrong.(model)), "/tree takes on, off or width") {
			t.Error("a wrong argument should get a note")
		}
	})

	t.Run("LT-09 /tree width N sets the columns for the session, the tree of /view too; out of range changes nothing", func(t *testing.T) {
		m := withTree(t)
		before := m.widthNote(treeSize)

		set, _ := m.runCommand("/tree width 40")
		m = set.(model)
		tooWide, _ := m.runCommand("/tree width 500")
		viewed, _ := tooWide.(model).runCommand("/view")

		if !strings.Contains(before, "file tree 30 columns, from the default") {
			t.Errorf("note %q", before)
		}
		if m.treeWidth() != 40 || m.mainWidth() != 100-41 || tooWide.(model).treeWidth() != 40 {
			t.Errorf("width %d, main %d, after 500 %d", m.treeWidth(), m.mainWidth(), tooWide.(model).treeWidth())
		}
		if got := viewed.(model).viewerWidth(); got != 100-41 {
			t.Errorf("/view text width %d", got)
		}
		if names := m.treeSuggestions("width "); len(names) == 0 || names[0].description != "now" {
			t.Errorf("suggestions %+v", names)
		}
	})
}
