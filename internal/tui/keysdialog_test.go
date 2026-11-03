package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | KD-01 | Happy    | f1 in the conversation opens the keys of the conversation, centred         | P0       |
// | KD-02 | Happy    | ? in a file opens the file's keys; in the input it is typed                 | P0       |
// | KD-03 | Happy    | Typing narrows the keys; esc clears the search, then closes                 | P0       |
// | KD-04 | Happy    | Keys that do not fit scroll with ↓ and pgdown, and say which are in view    | P1       |
// | KD-05 | Happy    | The keys follow the view: the tree at the left, the tree of /view           | P1       |
// | KD-06 | Edge     | ? while searching the tree is typed, not opening the keys                   | P1       |
// | KD-07 | Edge     | A click outside the box closes it                                           | P2       |
// | TF-01 | Happy    | c folds the folder of the row and every folder inside it, the cursor on it  | P0       |
// | TF-02 | Happy    | C folds every folder, the cursor on the top folder that held it             | P0       |

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var keyF1 = tea.KeyPressMsg{Code: tea.KeyF1}

func question() tea.KeyPressMsg { return tea.KeyPressMsg{Code: '?', Text: "?"} }

func openedFolders(tree *fileTree) []string {
	var open []string
	for _, row := range tree.rows {
		if row.open {
			open = append(open, row.name)
		}
	}
	return open
}

func TestKeysDialog(t *testing.T) {
	t.Run("KD-01 f1 in the conversation opens the keys of the conversation, centred", func(t *testing.T) {
		m := pressed(t, searchModel(t), keyF1)

		screen := seen(m)
		box := m.keysLayout()

		if m.keys == nil || m.keys.title != "conversation" {
			t.Fatalf("keys %+v", m.keys)
		}
		if !strings.Contains(screen, "Keys · conversation") || !strings.Contains(screen, "new line") {
			t.Errorf("screen:\n%s", screen)
		}
		if box.x == 0 || box.y == 0 {
			t.Errorf("box at %d,%d", box.x, box.y)
		}
	})

	t.Run("KD-02 ? in a file opens the file's keys; in the input it is typed", func(t *testing.T) {
		m := pressed(t, searchModel(t), question())
		typed := m.input.Value()
		viewed, _ := m.openFile("mi_file.go")

		m = pressed(t, viewed.(model), question())

		if typed != "?" {
			t.Errorf("input %q", typed)
		}
		if m.keys == nil || m.keys.title != "file" || !strings.Contains(seen(m), "comment the line") {
			t.Errorf("keys %+v:\n%s", m.keys, seen(m))
		}
	})

	t.Run("KD-03 typing narrows the keys; esc clears the search, then closes", func(t *testing.T) {
		viewed, _ := searchModel(t).openFile("mi_file.go")
		m := pressed(t, viewed.(model), question(), letter("b"), letter("l"), letter("a"), letter("m"))

		found := m.keys.matching()
		m = pressed(t, m, keyEscMsg)
		cleared := m.keys != nil && m.keys.filter == ""
		m = pressed(t, m, keyEscMsg)

		if len(found) != 1 || found[0].keys != "B" {
			t.Errorf("found %+v", found)
		}
		if !cleared || m.keys != nil || m.viewer == nil {
			t.Errorf("cleared %v, keys %v, viewer %v", cleared, m.keys, m.viewer)
		}
	})

	t.Run("KD-04 keys that do not fit scroll with ↓ and pgdown, and say which are in view", func(t *testing.T) {
		m := searchModel(t)
		m.height = 14
		m = pressed(t, m, keyF1)
		before := m.keysLayout()

		m = pressed(t, m, keyDown, tea.KeyPressMsg{Code: tea.KeyPgDown})

		if before.rows >= before.total {
			t.Fatalf("rows %d of %d should not fit", before.rows, before.total)
		}
		if m.keys.top != 1+before.rows {
			t.Errorf("top %d", m.keys.top)
		}
		if !strings.Contains(seen(m), "of "+strconv.Itoa(before.total)) || len(m.keysLayout().lines) > m.height {
			t.Errorf("screen:\n%s", seen(m))
		}
	})

	t.Run("KD-05 the keys follow the view: the tree at the left, the tree of /view", func(t *testing.T) {
		left := pressed(t, withTree(t), question())
		viewed := pressed(t, explorer(t), question())

		if left.keys == nil || left.keys.title != "files at the left" {
			t.Errorf("left %+v", left.keys)
		}
		if viewed.keys == nil || viewed.keys.title != "file tree" {
			t.Errorf("view %+v", viewed.keys)
		}
	})

	t.Run("KD-06 ? while searching the tree is typed, not opening the keys", func(t *testing.T) {
		m := pressed(t, withTree(t), letter("/"), question())

		if m.keys != nil || m.leftTree.typed != "?" {
			t.Errorf("keys %v, typed %q", m.keys, m.leftTree.typed)
		}
	})

	t.Run("KD-07 a click outside the box closes it", func(t *testing.T) {
		m := pressed(t, searchModel(t), keyF1)

		clicked, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: 0})

		if clicked.(model).keys != nil {
			t.Error("the dialog should close")
		}
	})
}

func TestTreeFolds(t *testing.T) {
	t.Run("TF-01 c folds the folder of the row and every folder inside it, the cursor on it", func(t *testing.T) {
		m := withTree(t)
		tree := m.leftTree
		for _, folder := range []string{"src", "src/deep", "src/deep/er", "src/pkg", "docs"} {
			tree.expanded[tree.root+"/"+folder] = true
		}
		tree.rebuild()
		tree.cursor = leftRow(m, "mi_file.test.go")

		m = pressed(t, m, letter("c"))
		onFile := strings.Join(openedFolders(tree), ",")
		tree.cursor = leftRow(m, "deep")
		m = pressed(t, m, letter("c"))

		if onFile != "docs,src,deep,pkg" {
			t.Errorf("open after c on a file: %s", onFile)
		}
		if open := strings.Join(openedFolders(tree), ","); open != "docs,src,pkg" {
			t.Errorf("open after c on deep: %s", open)
		}
		if row, _ := tree.selected(); row.name != "deep" {
			t.Errorf("cursor on %s", row.name)
		}
	})

	t.Run("TF-02 C folds every folder, the cursor on the top folder that held it", func(t *testing.T) {
		m := withTree(t)
		tree := m.leftTree
		for _, folder := range []string{"src", "src/pkg", "docs"} {
			tree.expanded[tree.root+"/"+folder] = true
		}
		tree.rebuild()
		tree.cursor = leftRow(m, "readme.md")

		m = pressed(t, m, letter("C"))

		if open := openedFolders(tree); len(open) != 0 {
			t.Errorf("open %v", open)
		}
		if row, _ := tree.selected(); row.name != "src" {
			t.Errorf("cursor on %s", row.name)
		}
	})
}
