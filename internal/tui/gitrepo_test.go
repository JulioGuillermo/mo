package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | GW-01 | Happy    | compare takes the branch alone, or the repository then the branch; show and branch an optional repository | P0 |
// | GW-02 | Happy    | /git show <repo> from a folder above opens that repository's changes         | P0       |
// | GW-03 | Happy    | Outside a repository with one below, /git show opens it                      | P1       |
// | GW-04 | Happy    | Outside a repository with several below, a list offers them                  | P1       |
// | GW-05 | Edge     | Outside a repository with none below, it says so and how deep it looked     | P2       |
// | GW-06 | Happy    | The folders offered list the repositories first, then the others             | P1       |
// | GW-07 | Happy    | After compare and a repository, its branches are offered                     | P1       |
// | GW-08 | Happy    | /git branch lists the branches, the current one marked, and switches to the one picked | P0 |
// | GW-09 | Edge     | /git branch does not switch while the model works                            | P1       |
// | GW-10 | Happy    | /git repos lists those below and a deeper one once opened, each with its branch | P1    |

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/go-git/go-git/v5"

	"github.com/jg/mo/internal/gitops"
)

// workspaceModel is mo in a folder with no repository: a and group/b
// below it, deep/er/est/c too deep to be found, and docs a plain folder.
func workspaceModel(t *testing.T) (model, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	workspace := t.TempDir()
	for _, path := range []string{"a", "group/b", "deep/er/est/c"} {
		initRepo(t, filepath.Join(workspace, path))
	}
	writeFile(t, filepath.Join(workspace, "docs/readme.md"), "hi\n")
	m := conversationModel()
	m.session.WorkDir = workspace
	return m, workspace
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	commitFiles(t, repo, dir, map[string]string{"main.go": "package main\n"})
}

func suggestionNames(found []command) []string {
	var names []string
	for _, each := range found {
		names = append(names, each.name)
	}
	return names
}

func TestGitRepos(t *testing.T) {
	t.Run("GW-01 compare takes the branch alone, or the repository then the branch; show and branch an optional repository", func(t *testing.T) {
		cases := map[string]gitRequest{
			"compare master":    {action: "compare", branch: "master"},
			"compare ./mo main": {action: "compare", repo: "./mo", branch: "main"},
			"show":              {action: "show"},
			"show ../x":         {action: "show", repo: "../x"},
			"branch /home/a/mo": {action: "branch", repo: "/home/a/mo"},
			"repos":             {action: "repos"},
		}
		for argument, want := range cases {
			if got, ok := parseGitRequest(argument); !ok || got != want {
				t.Errorf("%q = %+v %v", argument, got, ok)
			}
		}
		for _, wrong := range []string{"", "compare", "show a b", "repos a", "push"} {
			if _, ok := parseGitRequest(wrong); ok {
				t.Errorf("%q taken", wrong)
			}
		}
	})

	t.Run("GW-02 /git show <repo> from a folder above opens that repository's changes", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		writeFile(t, filepath.Join(workspace, "group/b/new.txt"), "hello\n")

		m = runGit(t, m, "show ./group/b")

		if m.viewer == nil || m.viewer.changes == nil || m.viewer.changes.root != filepath.Join(workspace, "group/b") {
			t.Fatalf("not opened:\n%s", shown(m))
		}
	})

	t.Run("GW-03 outside a repository with one below, /git show opens it", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		m.session.WorkDir = filepath.Join(workspace, "group")
		writeFile(t, filepath.Join(workspace, "group/b/new.txt"), "hello\n")
		m = runGit(t, m, "show")

		if m.viewer == nil || m.viewer.changes == nil || m.viewer.changes.root != filepath.Join(workspace, "group/b") {
			t.Fatalf("not opened:\n%s", shown(m))
		}
	})

	t.Run("GW-04 outside a repository with several below, a list offers them", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		writeFile(t, filepath.Join(workspace, "group/b/new.txt"), "hello\n")

		m = runGit(t, m, "show")
		list := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "group/b")

		if !strings.Contains(list, "> a") || strings.Contains(list, "est/c") || !strings.Contains(list, "deeper ones are not listed") {
			t.Errorf("list:\n%s", list)
		}
		if m.viewer == nil || m.viewer.changes.root != filepath.Join(workspace, "group/b") {
			t.Errorf("not opened:\n%s", shown(m))
		}
	})

	t.Run("GW-05 outside a repository with none below, it says so and how deep it looked", func(t *testing.T) {
		m, _ := workspaceModel(t)

		m = runGit(t, m, "show docs")

		if m.settings != nil || !strings.Contains(shown(m), "nor is any in the 2 levels below it") {
			t.Errorf("shown:\n%s", shown(m))
		}
	})

	t.Run("GW-06 the folders offered list the repositories first, then the others", func(t *testing.T) {
		m, _ := workspaceModel(t)

		top := suggestionNames(m.gitSuggestions("show "))
		inside := suggestionNames(m.gitSuggestions("branch group/"))

		if strings.Join(top, ",") != "git show a,git show deep/,git show docs/,git show group/" {
			t.Errorf("top = %v", top)
		}
		if strings.Join(inside, ",") != "git branch group/b" {
			t.Errorf("inside = %v", inside)
		}
	})

	t.Run("GW-07 after compare and a repository, its branches are offered", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		repo, _ := gitops.Open(filepath.Join(workspace, "a"))
		repo.Branch("feature")

		repos := suggestionNames(m.gitSuggestions("compare a"))
		branches := suggestionNames(m.gitSuggestions("compare a "))

		if strings.Join(repos, ",") != "git compare a " || strings.Join(branches, ",") != "git compare a feature" {
			t.Errorf("repos = %v branches = %v", repos, branches)
		}
	})

	t.Run("GW-08 /git branch lists the branches, the current one marked, and switches to the one picked", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		repo, _ := gitops.Open(filepath.Join(workspace, "a"))
		repo.Branch("feature")

		m = runGit(t, m, "branch a")
		list := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "feature")

		reopened, _ := gitops.Open(filepath.Join(workspace, "a"))
		if !strings.Contains(list, "master  ● current") || reopened.CurrentBranch() != "feature" || !strings.Contains(shown(m), "switched to feature") {
			t.Errorf("list:\n%s\nbranch %s shown:\n%s", list, reopened.CurrentBranch(), shown(m))
		}
	})

	t.Run("GW-09 /git branch does not switch while the model works", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		repo, _ := gitops.Open(filepath.Join(workspace, "a"))
		repo.Branch("feature")
		m.running = true

		updated, _ := m.switchBranch(repo, "feature")
		m = updated.(model)

		if repo.CurrentBranch() != "master" || !strings.Contains(shown(m), "once it ends") {
			t.Errorf("branch %s shown:\n%s", repo.CurrentBranch(), shown(m))
		}
	})

	t.Run("GW-10 /git repos lists those below and a deeper one once opened, each with its branch", func(t *testing.T) {
		m, workspace := workspaceModel(t)
		repo, _ := gitops.Open(filepath.Join(workspace, "a"))
		repo.Branch("feature")

		before := ansi.Strip(runGit(t, m, "repos").settings.form.View())
		runGit(t, m, "show deep/er/est/c")
		after := ansi.Strip(runGit(t, m, "repos").settings.form.View())

		if !strings.Contains(before, "./a/  master · 2 branches") || !strings.Contains(before, "./group/b/") || strings.Contains(before, "est/c") {
			t.Errorf("before:\n%s", before)
		}
		if !strings.Contains(after, "./deep/er/est/c/") {
			t.Errorf("after:\n%s", after)
		}
	})
}
