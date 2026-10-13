package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GB-01 | Happy    | A rebase replays the branch's commits on the target, authors and messages kept | P0 |
// | GB-02 | Happy    | A rebase stops at a conflict; continue replays the rest once resolved | P0       |
// | GB-03 | Happy    | Abort puts the branch back where it was before the rebase            | P0       |
// | GN-01 | Happy    | push sends the branch; fetch and pull bring another clone's commits  | P0       |
// | GN-02 | Happy    | A pull that is not a fast-forward merges, and its conflicts stop it   | P0       |
// | GN-03 | Error    | A push behind the remote is refused with what to do, never forced    | P0       |

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

// branched makes feature with two commits on a.txt and b.txt, and master
// with one on c.txt, or on main.go when clash is set.
func branched(t *testing.T, clash bool) *testRepo {
	tr := newRepo(t)
	tr.branch("feature", true)
	tr.commit("feature one", map[string]string{"a.txt": "a\n"})
	tr.commit("feature two", map[string]string{"b.txt": "b\n", "main.go": "package main\n\nfunc a() { feature() }\n\nfunc b() {}\n\nfunc c() {}\n"})
	tr.branch("master", false)
	if clash {
		tr.commit("master clash", map[string]string{"main.go": "package main\n\nfunc a() { master() }\n\nfunc b() {}\n\nfunc c() {}\n"})
	} else {
		tr.commit("master c", map[string]string{"c.txt": "c\n"})
	}
	tr.branch("feature", false)
	return tr
}

func TestRebase(t *testing.T) {
	t.Run("GB-01 a rebase replays the branch's commits on the target, authors and messages kept", func(t *testing.T) {
		tr := branched(t, false)

		result, err := tr.r.Rebase("master")

		head := tr.headCommit()
		parent, _ := head.Parent(0)
		grandparent, _ := parent.Parent(0)
		if err != nil || !strings.Contains(result, "rebased 2 commits") {
			t.Fatalf("result = %q err = %v", result, err)
		}
		if head.Message != "feature two" || parent.Message != "feature one" || grandparent.Message != "master c" || head.Author.Name != "Ana" {
			t.Errorf("history: %q < %q < %q", grandparent.Message, parent.Message, head.Message)
		}
		if tr.read("c.txt") != "c\n" || tr.read("a.txt") != "a\n" || tr.r.inProgress() != nil {
			t.Error("the files are not those of both branches")
		}
	})

	t.Run("GB-02 a rebase stops at a conflict; continue replays the rest once resolved", func(t *testing.T) {
		tr := branched(t, true)

		stopped, err := tr.r.Rebase("master")
		state := tr.r.inProgress()
		_, refused := tr.r.Continue()
		tr.write(map[string]string{"main.go": "package main\n\nfunc a() { master(); feature() }\n\nfunc b() {}\n\nfunc c() {}\n"})
		finished, finishErr := tr.r.Continue()

		if err != nil || !strings.Contains(stopped, "rebase stopped at 2 of 2") || state == nil || state.kind != "rebase" || refused == nil {
			t.Fatalf("stopped = %q err = %v refused = %v", stopped, err, refused)
		}
		head := tr.headCommit()
		if finishErr != nil || !strings.Contains(finished, "rebased 2 commits") || head.Message != "feature two" || tr.r.inProgress() != nil {
			t.Errorf("finished = %q err = %v message = %q", finished, finishErr, head.Message)
		}
	})

	t.Run("GB-03 abort puts the branch back where it was before the rebase", func(t *testing.T) {
		tr := branched(t, true)
		before := tr.headCommit().Hash
		_, _ = tr.r.Rebase("master")

		aborted, err := tr.r.Abort()

		if err != nil || !strings.Contains(aborted, "rebase aborted") || tr.headCommit().Hash != before || tr.r.inProgress() != nil || !strings.Contains(tr.read("main.go"), "feature()") {
			t.Errorf("aborted = %q err = %v", aborted, err)
		}
	})
}

// withRemote gives a repository a bare origin holding its master, and a
// second clone of it standing for someone else.
func withRemote(t *testing.T) (*testRepo, *testRepo) {
	t.Helper()
	tr := newRepo(t)
	bare := t.TempDir()
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.r.Push(""); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	cloned, err := git.PlainClone(other, false, &git.CloneOptions{URL: bare})
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := cloned.Worktree()
	opened, _ := Open(other)
	return tr, &testRepo{t: t, dir: other, repo: cloned, tree: tree, r: opened}
}

func TestRemote(t *testing.T) {
	t.Run("GN-01 push sends the branch; fetch and pull bring another clone's commits", func(t *testing.T) {
		mine, theirs := withRemote(t)
		theirs.commit("their work", map[string]string{"theirs.txt": "t\n"})

		pushed, pushErr := theirs.r.Push("")
		fetched, fetchErr := mine.r.Fetch("")
		pulled, pullErr := mine.r.Pull("")

		if pushErr != nil || !strings.Contains(pushed, "pushed master to origin") {
			t.Fatalf("pushed = %q err = %v", pushed, pushErr)
		}
		if fetchErr != nil || !strings.Contains(fetched, "fetched origin") || pullErr != nil || !strings.Contains(pulled, "fast-forwarded") || mine.read("theirs.txt") != "t\n" {
			t.Errorf("fetched = %q (%v) pulled = %q (%v)", fetched, fetchErr, pulled, pullErr)
		}
	})

	t.Run("GN-02 a pull that is not a fast-forward merges, and its conflicts stop it", func(t *testing.T) {
		if _, err := exec.LookPath("git-upload-pack"); err != nil {
			t.Skip("a folder remote without git is served by go-git's server, which leaves objects out of this fetch")
		}
		mine, theirs := withRemote(t)
		theirs.commit("their a", map[string]string{"main.go": "package main\n\nfunc a() { theirs() }\n\nfunc b() {}\n\nfunc c() {}\n"})
		_, _ = theirs.r.Push("")
		mine.commit("my a", map[string]string{"main.go": "package main\n\nfunc a() { mine() }\n\nfunc b() {}\n\nfunc c() {}\n"})

		pulled, err := mine.r.Pull("")

		if err != nil || !strings.Contains(pulled, "merge stopped at conflicts") || !strings.Contains(mine.read("main.go"), "<<<<<<< master") {
			t.Errorf("pulled = %q err = %v", pulled, err)
		}
	})

	t.Run("GN-03 a push behind the remote is refused with what to do, never forced", func(t *testing.T) {
		mine, theirs := withRemote(t)
		theirs.commit("their work", map[string]string{"theirs.txt": "t\n"})
		_, _ = theirs.r.Push("")
		mine.commit("my work", map[string]string{"mine.txt": "m\n"})

		_, err := mine.r.Push("")

		if err == nil || !strings.Contains(err.Error(), "pull first") {
			t.Errorf("err = %v", err)
		}
	})
}
