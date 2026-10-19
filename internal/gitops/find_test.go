package gitops

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | GF-01 | Happy    | ReposBelow finds the repositories 2 levels down, the shallow ones first  | P0       |
// | GF-02 | Edge     | It goes into no repository, hidden folder, node_modules nor vendor        | P1       |
// | GF-03 | Edge     | Past SearchLimit it stops and says there were more                        | P2       |
// | GF-04 | Happy    | KnownRepos adds every repository opened, however deep                     | P1       |
// | GF-05 | Happy    | BranchesLine gives the current branch, then a few others and how many more | P2      |
// | GF-06 | Happy    | A file the user's core.excludesFile names is not untracked, nor staged by add | P0    |
// | GF-07 | Edge     | !/tmp/keep/ brings the folder back, not the files /tmp/keep/* ignores, as git | P0    |

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
)

func makeRepos(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := git.PlainInit(filepath.Join(root, path), false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReposBelow(t *testing.T) {
	t.Run("GF-01 ReposBelow finds the repositories 2 levels down, the shallow ones first", func(t *testing.T) {
		root := t.TempDir()
		makeRepos(t, root, "z", "group/a", "deep/er/c")

		repos, more := ReposBelow(root)

		if !slices.Equal(repos, []string{"z", "group/a"}) || more {
			t.Errorf("repos = %v more = %v", repos, more)
		}
	})

	t.Run("GF-02 it goes into no repository, hidden folder, node_modules nor vendor", func(t *testing.T) {
		root := t.TempDir()
		makeRepos(t, root, "outer", "outer/inner", ".hidden/a", "node_modules/b", "vendor/c")

		repos, _ := ReposBelow(root)

		if !slices.Equal(repos, []string{"outer"}) {
			t.Errorf("repos = %v", repos)
		}
	})

	t.Run("GF-03 past SearchLimit it stops and says there were more", func(t *testing.T) {
		root := t.TempDir()
		for i := range SearchLimit + 1 {
			makeRepos(t, root, fmt.Sprintf("r%02d", i))
		}

		repos, more := ReposBelow(root)

		if len(repos) != SearchLimit || !more {
			t.Errorf("%d repos, more = %v", len(repos), more)
		}
	})

	t.Run("GF-04 KnownRepos adds every repository opened, however deep", func(t *testing.T) {
		root := t.TempDir()
		makeRepos(t, root, "a", "deep/er/c")
		before := KnownRepos(root)

		if _, err := Open(filepath.Join(root, "deep/er/c")); err != nil {
			t.Fatal(err)
		}
		after := KnownRepos(root)

		deep := filepath.Join(root, "deep/er/c")
		if !slices.Contains(before, filepath.Join(root, "a")) || slices.Contains(before, deep) || !slices.Contains(after, deep) {
			t.Errorf("before = %v after = %v", before, after)
		}
	})

	t.Run("GF-05 BranchesLine gives the current branch, then a few others and how many more", func(t *testing.T) {
		test := newRepo(t)
		alone := test.r.BranchesLine()
		for i := range shownBranches + 2 {
			if _, err := test.r.Branch(fmt.Sprintf("b%d", i)); err != nil {
				t.Fatal(err)
			}
		}

		many := test.r.BranchesLine()

		if alone != "master" || many != "master · also b0, b1, b2, b3, b4, b5, b6, b7 and 2 more" {
			t.Errorf("alone = %q many = %q", alone, many)
		}
	})

	t.Run("GF-06 a file the user's core.excludesFile names is not untracked, nor staged by add", func(t *testing.T) {
		test := newRepo(t)
		home, _ := os.UserHomeDir()
		test.write(map[string]string{"certs/ca.crt": "secret\n", "kept.txt": "x\n"})
		writeTestFile(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = ~/global_gitignore\n")
		writeTestFile(t, filepath.Join(home, "global_gitignore"), "certs/\n")
		reopened, _ := Open(test.dir)

		changes, _ := reopened.Changes()
		reopened.Add(nil)
		status, _ := reopened.Status()

		if len(changes) != 1 || changes[0].Path != "kept.txt" || strings.Contains(status, "ca.crt") {
			t.Errorf("changes = %v status = %q", changes, status)
		}
	})

	t.Run("GF-07 !/tmp/keep/ brings the folder back, not the files /tmp/keep/* ignores, as git", func(t *testing.T) {
		test := newRepo(t)
		test.write(map[string]string{
			".gitignore":     "/tmp/*\n/tmp/keep/*\n!/tmp/keep/\n!/tmp/keep/.keep\n",
			"tmp/a.log":      "x\n",
			"tmp/keep/b.id":  "x\n",
			"tmp/keep/.keep": "",
		})

		changes, _ := test.r.Changes()

		var paths []string
		for _, change := range changes {
			paths = append(paths, change.Path)
		}
		slices.Sort(paths)
		if !slices.Equal(paths, []string{".gitignore", "tmp/keep/.keep"}) {
			t.Errorf("paths = %v", paths)
		}
	})
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
