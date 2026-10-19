package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | GV-01 | Happy    | /git show lists only the changed files, coloured by what happened to them   | P0       |
// | GV-02 | Happy    | The first file's diff shows beside the tree: lines out, lines in, numbered  | P0       |
// | GV-03 | Happy    | A file picked in the tree shows its diff, the tree kept, and t hides and shows it | P1  |
// | GV-04 | Happy    | /git compare <branch> shows what this branch adds over it                   | P0       |
// | GV-05 | Edge     | Nothing to show, or no action, says so instead of opening                   | P1       |
// | GV-06 | Happy    | /git suggests its actions, then the other branches                          | P2       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	t.Run("GV-06 /git suggests its actions, then the other branches", func(t *testing.T) {
		m, dir := gitModel(t)
		repo, _ := gitops.Open(dir)
		repo.Switch("feature", true)

		actions := m.gitSuggestions("co")
		branches := m.gitSuggestions("compare ")

		if len(actions) != 1 || actions[0].name != "git compare " || len(branches) != 1 || branches[0].name != "git compare master" {
			t.Errorf("actions %v branches %v", actions, branches)
		}
	})
}
