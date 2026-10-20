package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | GC-01 | Happy    | /git compare suggests the remote branches after the local ones; switch suggests only local ones | P1 |
// | GC-02 | Happy    | Comparing with a local branch behind the remote one it follows says by how many commits and how to compare with the remote | P0 |
// | GC-03 | Edge     | A branch up to date with its remote, or with none, adds no warning          | P1       |
// | GC-04 | Happy    | The file under the tree's cursor keeps the colour of its change, focused or not | P1   |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/jg/mo/internal/gitops"
)

// behindModel is a repository on branch feature whose master lacks the one
// commit origin/master has, as if fetched and not pulled.
func behindModel(t *testing.T) (model, string) {
	t.Helper()
	m, dir := gitModel(t)
	repo, _ := git.PlainOpen(dir)
	start, _ := repo.Head()
	commitFiles(t, repo, dir, map[string]string{"remote.txt": "from the remote\n"})
	ahead, _ := repo.Head()
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", "master"), ahead.Hash())); err != nil {
		t.Fatal(err)
	}
	tree, _ := repo.Worktree()
	if err := tree.Reset(&git.ResetOptions{Commit: start.Hash(), Mode: git.HardReset}); err != nil {
		t.Fatal(err)
	}
	opened, _ := gitops.Open(dir)
	if _, err := opened.Switch("feature", true); err != nil {
		t.Fatal(err)
	}
	commitFiles(t, repo, dir, map[string]string{"feature.txt": "mine\n"})
	return m, dir
}

func TestGitCompare(t *testing.T) {
	t.Run("GC-01 /git compare suggests the remote branches after the local ones; switch suggests only local ones", func(t *testing.T) {
		m, _ := behindModel(t)

		compare := m.gitSuggestions("compare ")
		switches := m.gitSuggestions("switch ")

		var names []string
		for _, each := range compare {
			names = append(names, each.name+" ("+each.description+")")
		}
		if len(compare) < 2 || compare[0].name != "git compare master" || compare[1].name != "git compare origin/master" || !strings.Contains(compare[1].description, "remote") {
			t.Errorf("compare: %v", names)
		}
		for _, each := range switches {
			if strings.Contains(each.name, "origin/") {
				t.Errorf("switch suggests %s", each.name)
			}
		}
	})

	t.Run("GC-02 comparing with a local branch behind the remote one it follows says by how many commits and how to compare with the remote", func(t *testing.T) {
		m, _ := behindModel(t)

		updated, cmd := m.runCommand("/git compare master")
		after := deliver(updated.(model), cmd, 2)

		if after.viewer == nil || !strings.Contains(shown(after), "master is 1 commit behind origin/master") || !strings.Contains(shown(after), "/git compare origin/master") {
			t.Errorf("viewer open = %v shown:\n%s", after.viewer != nil, shown(after))
		}
	})

	t.Run("GC-03 a branch up to date with its remote, or with none, adds no warning", func(t *testing.T) {
		m, dir := gitModel(t)
		repo, _ := gitops.Open(dir)
		repo.Switch("feature", true)
		commitFiles(t, mustOpen(t, dir), dir, map[string]string{"feature.txt": "mine\n"})
		head, _ := mustOpen(t, dir).Reference(plumbing.NewBranchReferenceName("master"), true)
		mustOpen(t, dir).Storer.SetReference(plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", "master"), head.Hash()))

		updated, cmd := m.runCommand("/git compare master")
		after := deliver(updated.(model), cmd, 2)

		if after.viewer == nil || strings.Contains(shown(after), "behind") {
			t.Errorf("shown:\n%s", shown(after))
		}
	})

	t.Run("GC-04 the file under the tree's cursor keeps the colour of its change, focused or not", func(t *testing.T) {
		m, dir := gitModel(t)
		changeUncommitted(t, dir)
		m = runGit(t, m, "show")
		tree := m.viewer.tree
		tree.cursor = 2

		focused := tree.lines(40, 10, true, iconsText)
		blurred := tree.lines(40, 10, false, iconsText)

		red := "38;2;247;118;142"
		for name, lines := range map[string][]string{"focused": focused, "blurred": blurred} {
			row := ""
			for _, line := range lines {
				if strings.Contains(ansi.Strip(line), "gone.txt") {
					row = line
				}
			}
			if !strings.Contains(row, red) {
				t.Errorf("%s row lost the deleted colour: %q", name, row)
			}
		}
	})
}

func mustOpen(t *testing.T, dir string) *git.Repository {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
