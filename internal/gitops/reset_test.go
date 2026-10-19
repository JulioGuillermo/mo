package gitops

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | GS-01 | Happy    | Switching branches leaves a submodule's folder alone and writes every file  | P0       |
// | GS-02 | Happy    | A symbolic link is written as a link, not as a file holding its target      | P1       |
// | GS-03 | Error    | A switch that cannot write its files goes back: HEAD, index and files as before | P0   |

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ana", "GIT_AUTHOR_EMAIL=ana@x", "GIT_COMMITTER_NAME=Ana", "GIT_COMMITTER_EMAIL=ana@x")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func cleanAfter(t *testing.T, r *Repo) {
	t.Helper()
	status, err := r.status()
	if err != nil {
		t.Fatal(err)
	}
	for path, file := range status {
		if file.Worktree == '?' {
			continue
		}
		if file.Worktree != ' ' || file.Staging != ' ' {
			t.Errorf("%s: staging %q worktree %q", path, file.Staging, file.Worktree)
		}
	}
}

func TestSwitching(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs the git command to make a submodule")
	}

	t.Run("GS-01 switching branches leaves a submodule's folder alone and writes every file", func(t *testing.T) {
		test := newRepo(t)
		sub := filepath.Join(test.dir, "docs/sub")
		test.write(map[string]string{"docs/sub/readme.md": "sub\n", ".gitmodules": "[submodule \"docs/sub\"]\n\tpath = docs/sub\n\turl = ../sub\n"})
		gitCommand(t, sub, "init", "-q")
		gitCommand(t, sub, "add", ".")
		gitCommand(t, sub, "commit", "-qm", "sub")
		gitCommand(t, test.dir, "add", ".gitmodules", "docs/sub")
		gitCommand(t, test.dir, "commit", "-qm", "submodule")
		test.r.Branch("feature")
		test.r.Switch("feature", false)
		test.commit("feature only", map[string]string{"main.go": "package main\n\nfunc feature() {}\n", "extra.go": "package main\n"})
		test.r.Switch("master", false)

		_, err := test.r.Switch("feature", false)

		content, _ := os.ReadFile(filepath.Join(test.dir, "main.go"))
		if err != nil || !strings.Contains(string(content), "feature()") || test.r.CurrentBranch() != "feature" {
			t.Fatalf("err = %v branch = %s main.go = %q", err, test.r.CurrentBranch(), content)
		}
		if _, err := os.Stat(filepath.Join(sub, "readme.md")); err != nil {
			t.Errorf("submodule folder touched: %v", err)
		}
		cleanAfter(t, test.r)
	})

	t.Run("GS-02 a symbolic link is written as a link, not as a file holding its target", func(t *testing.T) {
		test := newRepo(t)
		test.r.Branch("feature")
		test.r.Switch("feature", false)
		if err := os.Symlink("main.go", filepath.Join(test.dir, "link.go")); err != nil {
			t.Fatal(err)
		}
		gitCommand(t, test.dir, "add", "link.go")
		gitCommand(t, test.dir, "commit", "-qm", "link")
		test.r.Switch("master", false)

		_, err := test.r.Switch("feature", false)

		target, linkErr := os.Readlink(filepath.Join(test.dir, "link.go"))
		if err != nil || linkErr != nil || target != "main.go" {
			t.Errorf("err = %v link = %q %v", err, target, linkErr)
		}
		cleanAfter(t, test.r)
	})

	t.Run("GS-03 a switch that cannot write its files goes back: HEAD, index and files as before", func(t *testing.T) {
		test := newRepo(t)
		test.r.Branch("feature")
		test.r.Switch("feature", false)
		test.commit("locked", map[string]string{"locked/a.go": "package locked\n", "main.go": "package main\n\nfunc feature() {}\n"})
		test.r.Switch("master", false)
		test.write(map[string]string{"locked/untracked.txt": "mine\n"})
		if err := os.Chmod(filepath.Join(test.dir, "locked"), 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(filepath.Join(test.dir, "locked"), 0o755) })

		_, err := test.r.Switch("feature", false)

		content, _ := os.ReadFile(filepath.Join(test.dir, "main.go"))
		if err == nil || !strings.Contains(err.Error(), "switch undone") || test.r.CurrentBranch() != "master" || strings.Contains(string(content), "feature()") {
			t.Fatalf("err = %v branch = %s main.go = %q", err, test.r.CurrentBranch(), content)
		}
		cleanAfter(t, test.r)
	})
}
