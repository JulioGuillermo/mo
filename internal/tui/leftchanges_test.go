package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | LC-01 | Happy    | /tree changes lists only the files not committed, coloured, titled with how many | P0   |
// | LC-02 | Happy    | enter on a file shows its diff with the same tree; esc comes back, the tree kept | P0   |
// | LC-03 | Happy    | Read again after the files change, it lists the new ones, keeps the cursor and the folders closed | P0 |
// | LC-04 | Happy    | /tree files goes back to the files; /tree off hides it and stops reading it   | P1       |
// | LC-05 | Happy    | /tree changes <branch> lists what the branch changed over the other one       | P1       |
// | LC-06 | Edge     | Outside a repository it says so and leaves the tree as it was                 | P1       |
// | LC-07 | Happy    | /tree both shows the files above and the changes below, tab goes between them | P0       |
// | LC-08 | Happy    | In both, a click on the lower half opens the diff, one on the upper the file  | P0       |
// | LC-09 | Happy    | A file changed has a dot of its colour in the tree of files, its folders too  | P0       |
// | LC-10 | Happy    | /tree status off takes the dots away, on brings them; the setting too         | P1       |

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// changesTree runs /tree changes with an argument and takes what it read.
func changesTree(t *testing.T, m model, argument string) model {
	t.Helper()
	next, cmd := m.runCommand(strings.TrimSpace("/tree changes " + argument))
	m = next.(model)
	if cmd == nil {
		t.Fatal("/tree changes should read the changes")
	}
	read, _ := m.Update(cmd())
	return read.(model)
}

func readAgain(t *testing.T, m model) model {
	t.Helper()
	cmd := m.readLeftChangesAgain()
	if cmd == nil {
		return m
	}
	read, _ := m.Update(cmd())
	return read.(model)
}

func changedRow(m model, name string) int {
	for i, row := range m.leftChanges.tree.rows {
		if row.name == name {
			return i
		}
	}
	return -1
}

func changedNames(m model) string {
	var names []string
	for _, row := range m.leftChanges.tree.rows {
		if !row.dir {
			names = append(names, row.name)
		}
	}
	return strings.Join(names, ",")
}

func TestLeftChanges(t *testing.T) {
	t.Run("LC-01 /tree changes lists only the files not committed, coloured, titled with how many", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)

		m = changesTree(t, m, "")

		if got := changedNames(m); got != "edit.go,gone.txt,new.txt" {
			t.Errorf("files %s", got)
		}
		if !m.leftTreeFocused() || m.leftChanges.tree.kinds[filepath.Join(dir, "new.txt")] == 0 {
			t.Errorf("focused %v, kinds %v", m.leftTreeFocused(), m.leftChanges.tree.kinds)
		}
		if !strings.Contains(seen(m), "3 files changed not committed") {
			t.Errorf("screen:\n%s", seen(m))
		}
	})

	t.Run("LC-02 enter on a file shows its diff with the same tree; esc comes back, the tree kept", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)
		m = changesTree(t, m, "")
		tree := m.leftChanges.tree
		tree.cursor = changedRow(m, "edit.go")

		m = pressed(t, m, keyEnterTree)
		diff := m.viewer
		m = pressed(t, m, keyEscMsg)

		if diff == nil || diff.changes == nil || diff.tree != tree || filepath.Base(diff.diffFile) != "edit.go" {
			t.Fatalf("viewer %+v", diff)
		}
		if m.viewer != nil || m.leftChanges.tree != tree || m.leftTreeFocused() {
			t.Errorf("viewer %v, tree kept %v", m.viewer, m.leftChanges.tree == tree)
		}
	})

	t.Run("LC-03 read again after the files change, it lists the new ones, keeps the cursor and the folders closed", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		writeFile(t, filepath.Join(dir, "src/edit.go"), "package src\n\nfunc c() {}\n")
		m = changesTree(t, m, "")
		before := changedNames(m)
		m.leftChanges.tree.cursor = changedRow(m, "edit.go")
		writeFile(t, filepath.Join(dir, "docs/a.txt"), "x\n")
		m = readAgain(t, m)
		m.leftChanges.tree.cursor = changedRow(m, "src")
		m = pressed(t, m, keyEnterTree)

		writeFile(t, filepath.Join(dir, "added.txt"), "x\n")
		m = readAgain(t, m)

		if before != "edit.go" || changedNames(m) != "a.txt,added.txt" {
			t.Errorf("before %s, after %s", before, changedNames(m))
		}
		if row, _ := m.leftChanges.tree.selected(); row.name != "src" {
			t.Errorf("cursor on %s", row.name)
		}
	})

	t.Run("LC-04 /tree files goes back to the files; /tree off hides it and stops reading it", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)
		m = changesTree(t, m, "")

		files, _ := m.runCommand("/tree files")
		m = files.(model)
		listsFiles := m.leftChanges == nil && m.leftTree != nil && leftRow(m, "src") >= 0
		off, _ := m.runCommand("/tree off")

		if !listsFiles {
			t.Errorf("rows %v", rowNames(m))
		}
		if off.(model).leftTree != nil || off.(model).readLeftChangesAgain() != nil {
			t.Error("/tree off should hide the tree and read nothing")
		}
	})

	t.Run("LC-05 /tree changes <branch> lists what the branch changed over the other one", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		repo, _ := git.PlainOpen(dir)
		head, _ := repo.Head()
		_ = repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/base", head.Hash()))
		commitFiles(t, repo, dir, map[string]string{"feature.go": "package main\n"})

		m = changesTree(t, m, "base")

		if got := changedNames(m); got != "feature.go" {
			t.Errorf("files %s", got)
		}
		if !strings.Contains(seen(m), "1 file changed over base") {
			t.Errorf("screen:\n%s", seen(m))
		}
	})

	t.Run("LC-06 outside a repository it says so and leaves the tree as it was", func(t *testing.T) {
		m := withTree(t)
		tree := m.leftTree

		next, cmd := m.runCommand("/tree changes")
		read, _ := next.(model).Update(cmd())
		m = read.(model)

		if m.leftTree != tree || m.leftChanges != nil {
			t.Error("the tree should stay as it was")
		}
		if !strings.Contains(seen(m), "no changes to show") {
			t.Errorf("screen:\n%s", seen(m))
		}
	})
	t.Run("LC-07 /tree both shows the files above and the changes below, tab goes between them", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)

		m = changesTree(t, m, "")
		next, cmd := m.runCommand("/tree both")
		read, _ := next.(model).Update(cmd())
		m = read.(model)
		screen := seen(m)
		onFiles := m.activeLeftHalf().changes
		m = pressed(t, m, keyTabMsg)

		if m.leftMode != leftBoth || m.leftTree == nil || m.leftChanges == nil {
			t.Fatalf("mode %q", m.leftMode)
		}
		lines := strings.Split(screen, "\n")
		half := m.panelRows() / 2
		if !strings.Contains(lines[0], filepath.Base(dir)) || !strings.Contains(lines[half], "3 files changed") {
			t.Errorf("screen:\n%s", screen)
		}
		if onFiles || !m.activeLeftHalf().changes {
			t.Errorf("tab should go from the files to the changes")
		}
	})

	t.Run("LC-08 in both, a click on the lower half opens the diff, one on the upper the file", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)
		m = changesTree(t, m, "")
		next, cmd := m.runCommand("/tree both")
		read, _ := next.(model).Update(cmd())
		m = read.(model)
		lower := m.leftHalves(m.panelRows())[1]

		clicked, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: lower.top + 1 + changedRow(m, "new.txt")})
		diff := clicked.(model).viewer
		clicked, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: 1 + leftRow(m, "new.txt")})
		file := clicked.(model).viewer

		if diff == nil || diff.changes == nil || diff.diffFile != "new.txt" {
			t.Errorf("diff %+v", diff)
		}
		if file == nil || file.changes != nil || filepath.Base(file.file) != "new.txt" {
			t.Errorf("file %+v", file)
		}
	})

	t.Run("LC-09 a file changed has a dot of its colour in the tree of files, its folders too", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)
		updated, _ := m.Update(m.readRepo()())
		m = updated.(model)
		opened, _ := m.runCommand("/tree")
		m = opened.(model)

		lines := strings.Split(seen(m), "\n")
		row := func(name string) string {
			for _, line := range lines {
				if strings.Contains(line, " "+name) {
					return line
				}
			}
			return ""
		}

		for _, name := range []string{"src", "new.txt"} {
			if line := row(name); !strings.Contains(line, treeStatusDot+"│") {
				t.Errorf("%s: %q", name, line)
			}
		}
		if marks := m.gitMarksFor(m.leftTree); marks[filepath.Join(dir, "new.txt")] == 0 || marks[filepath.Join(dir, "src")] != '~' {
			t.Errorf("marks %v", marks)
		}
	})

	t.Run("LC-10 /tree status off takes the dots away, on brings them; the setting too", func(t *testing.T) {
		m, dir := gitModel(t)
		m.width, m.height = 100, 30
		changeUncommitted(t, dir)
		updated, _ := m.Update(m.readRepo()())
		m = updated.(model)
		opened, _ := m.runCommand("/tree")
		m = opened.(model)

		off, _ := m.runCommand("/tree status off")
		offScreen := seen(off.(model))
		on, _ := off.(model).runCommand("/tree status")
		m = on.(model)
		m.treeStatusSession = ""
		no := false
		m.session.Config.Render.TreeStatus = &no

		if strings.Contains(offScreen, treeStatusDot) {
			t.Error("off should take the dots away")
		}
		if !on.(model).treeStatusOn() || m.treeStatusOn() {
			t.Errorf("on %v, setting off %v", on.(model).treeStatusOn(), m.treeStatusOn())
		}
	})
}
