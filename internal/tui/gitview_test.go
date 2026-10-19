package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | GV-01 | Happy    | /git show lists only the changed files, coloured by what happened to them   | P0       |
// | GV-02 | Happy    | The first file's diff shows beside the tree: lines out, lines in, numbered  | P0       |
// | GV-03 | Happy    | A file picked in the tree shows its diff, the tree kept, and t hides and shows it | P1  |
// | GV-04 | Happy    | /git compare <branch> shows what this branch adds over it                   | P0       |
// | GV-05 | Edge     | Nothing to show, or no action, says so instead of opening                   | P1       |
// | GV-06 | Happy    | /git suggests its actions, then for compare the other branches before folders | P2     |
// | GV-07 | Happy    | Lines far from a change fold into one row; e shows 20 of them, E all        | P0       |
// | GV-08 | Happy    | f shows the whole file and back, the cursor kept on its line                | P0       |
// | GV-09 | Happy    | A comment on a diff line goes to the model with its line, kept as lines unfold | P0    |
// | GV-10 | Edge     | A fold has nothing to comment, and e off a fold says where to press it      | P2       |
// | GV-11 | Happy    | The cursor's row is one lighter colour end to end: fold, added, removed     | P1       |
// | GV-12 | Happy    | /git opens while the model works                                            | P1       |

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/jg/mo/internal/gitops"
)

func gitModel(t *testing.T) (model, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	commitFiles(t, repo, dir, map[string]string{"src/edit.go": "package src\n\nfunc a() {}\n", "gone.txt": "bye\n"})
	m := conversationModel()
	m.session.WorkDir = dir
	return m, dir
}

func commitFiles(t *testing.T, repo *git.Repository, dir string, files map[string]string) {
	t.Helper()
	tree, _ := repo.Worktree()
	for path, content := range files {
		writeFile(t, filepath.Join(dir, path), content)
		if _, err := tree.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tree.Commit("files", &git.CommitOptions{Author: &object.Signature{Name: "Ana", Email: "ana@x", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
}

func changeUncommitted(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "src/edit.go"), "package src\n\nfunc b() {}\n")
	writeFile(t, filepath.Join(dir, "new.txt"), "hello\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
}

func runGit(t *testing.T, m model, argument string) model {
	t.Helper()
	updated, _ := m.runCommand("/git " + argument)
	return updated.(model)
}

func TestGitView(t *testing.T) {
	t.Run("GV-01 /git show lists only the changed files, coloured by what happened to them", func(t *testing.T) {
		m, dir := gitModel(t)
		changeUncommitted(t, dir)

		m = runGit(t, m, "show")

		if m.viewer == nil || m.viewer.tree == nil {
			t.Fatal("no tree opened")
		}
		var names []string
		for _, row := range m.viewer.tree.rows {
			names = append(names, row.name)
		}
		if strings.Join(names, ",") != "src,edit.go,gone.txt,new.txt" {
			t.Errorf("rows = %v", names)
		}
		kinds := m.viewer.tree.kinds
		if kinds[filepath.Join(dir, "new.txt")] != gitops.ChangeUntracked || kinds[filepath.Join(dir, "gone.txt")] != gitops.ChangeDeleted || kinds[filepath.Join(dir, "src/edit.go")] != gitops.ChangeModified {
			t.Errorf("kinds = %v", kinds)
		}
	})

	t.Run("GV-02 the first file's diff shows beside the tree: lines out, lines in, numbered", func(t *testing.T) {
		m, dir := gitModel(t)
		changeUncommitted(t, dir)

		m = runGit(t, m, "show")

		screen := ansi.Strip(m.viewerView())
		if m.viewer.diffFile != "gone.txt" || !strings.Contains(screen, "1 - bye") {
			t.Errorf("file %q screen:\n%s", m.viewer.diffFile, screen)
		}
	})

	t.Run("GV-03 a file picked in the tree shows its diff, the tree kept, and t hides and shows it", func(t *testing.T) {
		m, dir := gitModel(t)
		changeUncommitted(t, dir)
		m = runGit(t, m, "show")

		updated, _ := m.openFromTree(filepath.Join(dir, "src/edit.go"), true)
		m = updated.(model)

		screen := ansi.Strip(m.viewerView())
		if !strings.Contains(screen, "3 - func a() {}") || !strings.Contains(screen, "3 + func b() {}") || m.viewer.tree == nil || m.viewer.treeFocus {
			t.Fatalf("screen:\n%s", screen)
		}
		m.toggleTree()
		hidden := m.viewer.tree == nil
		m.toggleTree()
		if !hidden || m.viewer.tree == nil || len(m.viewer.tree.rows) != 4 {
			t.Errorf("hidden %v tree back %v", hidden, m.viewer.tree != nil)
		}
	})

	t.Run("GV-04 /git compare <branch> shows what this branch adds over it", func(t *testing.T) {
		m, dir := gitModel(t)
		repo, _ := gitops.Open(dir)
		repo.Switch("feature", true)
		gitRepo, _ := git.PlainOpen(dir)
		commitFiles(t, gitRepo, dir, map[string]string{"feature.txt": "only here\n"})

		m = runGit(t, m, "compare master")

		if m.viewer == nil || m.viewer.diffFile != "feature.txt" || !strings.Contains(ansi.Strip(m.viewerView()), "master → feature") {
			t.Fatalf("viewer %v", m.viewer)
		}
		if kind := m.viewer.tree.kinds[filepath.Join(dir, "feature.txt")]; kind != gitops.ChangeAdded {
			t.Errorf("kind %c", kind)
		}
	})

	t.Run("GV-05 nothing to show, or no action, says so instead of opening", func(t *testing.T) {
		m, _ := gitModel(t)

		clean := runGit(t, m, "show")
		usage := runGit(t, m, "")

		if clean.viewer != nil || !strings.Contains(shown(clean), "nothing changed") || !strings.Contains(shown(usage), "usage: /git show") {
			t.Errorf("clean:\n%s\nusage:\n%s", shown(clean), shown(usage))
		}
	})

	t.Run("GV-06 /git suggests its actions, then for compare the other branches before folders", func(t *testing.T) {
		m, dir := gitModel(t)
		repo, _ := gitops.Open(dir)
		repo.Switch("feature", true)

		actions := m.gitSuggestions("co")
		branches := m.gitSuggestions("compare ")

		if len(actions) != 1 || actions[0].name != "git compare " || len(branches) != 2 || branches[0].name != "git compare master" || branches[1].name != "git compare src/" {
			t.Errorf("actions %v branches %v", actions, branches)
		}
	})
}

func longFileModel(t *testing.T) (model, string) {
	t.Helper()
	m, dir := gitModel(t)
	var before strings.Builder
	for i := 1; i <= 100; i++ {
		before.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	gitRepo, _ := git.PlainOpen(dir)
	commitFiles(t, gitRepo, dir, map[string]string{"long.txt": before.String()})
	writeFile(t, filepath.Join(dir, "long.txt"), strings.Replace(before.String(), "line 50\n", "line fifty\n", 1))
	m = runGit(t, m, "show")
	m.viewer.treeFocus = false
	return m, dir
}

func pressKeys(m model, keys ...string) model {
	for _, key := range keys {
		var msg tea.KeyPressMsg
		switch key {
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		default:
			msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
		}
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	return m
}

func TestDiffFolds(t *testing.T) {
	t.Run("GV-07 lines far from a change fold into one row; e shows 20 of them, E all", func(t *testing.T) {
		m, _ := longFileModel(t)
		lines := m.viewer.lines
		if len(lines) != 10 || !strings.Contains(lines[0], "⋯ 46 unchanged lines") || !strings.Contains(lines[9], "⋯ 47 unchanged lines") {
			t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
		}

		m = pressKeys(m, "e")
		afterStep := len(m.viewer.lines)
		m.viewer.cursor = 0
		m = pressKeys(m, "E")

		if afterStep != 30 || len(m.viewer.lines) != 55 || !strings.Contains(m.viewer.lines[0], "1   line 1") {
			t.Errorf("after e %d rows, after E %d:\n%s", afterStep, len(m.viewer.lines), strings.Join(m.viewer.lines[:3], "\n"))
		}
	})

	t.Run("GV-08 f shows the whole file and back, the cursor kept on its line", func(t *testing.T) {
		m, _ := longFileModel(t)
		m = pressKeys(m, "down", "down", "down", "down")
		on := m.viewer.lines[m.viewer.cursor]

		m = pressKeys(m, "f")
		whole, wholeOn := len(m.viewer.lines), m.viewer.lines[m.viewer.cursor]
		m = pressKeys(m, "f")

		if whole != 101 || wholeOn != on || m.viewer.lines[m.viewer.cursor] != on || len(m.viewer.lines) != 10 {
			t.Errorf("whole %d on %q / %q, back %d on %q", whole, on, wholeOn, len(m.viewer.lines), m.viewer.lines[m.viewer.cursor])
		}
	})

	t.Run("GV-09 a comment on a diff line goes to the model with its line, kept as lines unfold", func(t *testing.T) {
		m, _ := longFileModel(t)
		m = pressKeys(m, "down", "down", "down", "down", "down")
		m = pressKeys(m, "c", "w", "h", "y", "enter")
		m.viewer.cursor = 0
		m = pressKeys(m, "E")

		row := m.viewer.rowOf(m.comments()[0].from)
		marked := strings.Contains(m.viewer.lines[row], "+ line fifty")
		m = pressKeys(m, "a")
		prompt := m.withAttachments(m.input.Value() + "check")

		if !marked || !strings.Contains(prompt, "line 50, added (`line fifty`): why") || !strings.Contains(prompt, "long.txt") {
			t.Errorf("marked %v prompt:\n%s", marked, prompt)
		}
		if len(m.fileComments) != 0 {
			t.Errorf("comments left after sending: %v", m.fileComments)
		}
	})

	t.Run("GV-10 a fold has nothing to comment, and e off a fold says where to press it", func(t *testing.T) {
		m, _ := longFileModel(t)

		folded := pressKeys(m, "c")
		folded.viewer.notice, folded.viewer.cursor = "", 4
		offFold := pressKeys(folded, "e")

		if folded.viewer.commenting || !strings.Contains(offFold.viewer.notice, "⋯") {
			t.Errorf("commenting %v notice %q", folded.viewer.commenting, offFold.viewer.notice)
		}
	})
}

func TestDiffCursor(t *testing.T) {
	t.Run("GV-11 the cursor's row is one lighter colour end to end: fold, added, removed", func(t *testing.T) {
		m, _ := longFileModel(t)
		colors := changePalette(true)
		for _, each := range []struct {
			down    int
			lighter color.Color
			plain   color.Color
		}{{0, colors.blockCursor, colors.block}, {4, colors.removedCursor, colors.removed}, {5, colors.addedCursor, colors.added}} {
			m.viewer.cursor = each.down

			row := strings.Split(m.viewerView(), "\n")[1+each.down]

			if !strings.Contains(row, backgroundOf(each.lighter)) || strings.Contains(row, backgroundOf(each.plain)) || strings.Contains(row, cursorBackground) {
				t.Errorf("row %d = %q", each.down, row)
			}
		}
	})
}

func backgroundOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestGitWhileRunning(t *testing.T) {
	t.Run("GV-12 /git opens while the model works", func(t *testing.T) {
		m, dir := gitModel(t)
		changeUncommitted(t, dir)
		m.running = true

		m = runGit(t, m, "show")

		if m.viewer == nil || m.viewer.changes == nil {
			t.Errorf("not opened:\n%s", shown(m))
		}
	})
}
