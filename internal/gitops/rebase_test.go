package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GB-01 | Happy    | A rebase replays the branch's commits on the target, authors and messages kept | P0 |
// | GB-02 | Happy    | A rebase stops at a conflict; continue replays the rest once resolved | P0       |
// | GB-03 | Happy    | Abort puts the branch back where it was before the rebase            | P0       |
// | GN-01 | Happy    | push sends the branch; fetch and pull bring another clone's commits  | P0       |
// | GN-02 | Happy    | A pull that is not a fast-forward merges, and its conflicts stop it   | P0       |
// | GN-03 | Error    | A push behind the remote is refused with what to do, never forced    | P0       |
// | GN-04 | Happy    | pull with rebase replays my commits on theirs, with no merge commit  | P0       |
// | GN-05 | Happy    | pull follows git's pull.rebase unless told; told, it does as told    | P1       |
// | GN-06 | Happy    | pull takes another branch of the remote when named                   | P0       |
// | GN-07 | Happy    | pull takes the branch the current one tracks                         | P1       |
// | GN-08 | Happy    | After a fetch, the remote branches are listed and a branch says how far behind its upstream it is | P1 |

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
		pulled, pullErr := mine.r.Pull(PullOptions{})

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

		pulled, err := mine.r.Pull(PullOptions{})

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

	t.Run("GN-04 pull with rebase replays my commits on theirs, with no merge commit", func(t *testing.T) {
		needsGitServer(t)
		mine, theirs := divergedClones(t)
		rebase := true

		pulled, err := mine.r.Pull(PullOptions{Rebase: &rebase})

		head := mine.headCommit()
		if err != nil || !strings.Contains(pulled, "rebased") || head.NumParents() != 1 || head.Message != "my work" || head.ParentHashes[0] != theirs.headCommit().Hash {
			t.Errorf("pulled = %q err = %v parents = %d", pulled, err, head.NumParents())
		}
		if mine.read("theirs.txt") != "t\n" || mine.read("mine.txt") != "m\n" {
			t.Error("a side's file is missing")
		}
	})

	t.Run("GN-05 pull follows git's pull.rebase unless told; told, it does as told", func(t *testing.T) {
		needsGitServer(t)
		configured, _ := divergedClones(t)
		setOption(t, configured, "pull", "", "rebase", "true")
		told, _ := divergedClones(t)
		setOption(t, told, "pull", "", "rebase", "true")
		merge := false

		_, err := configured.r.Pull(PullOptions{})
		_, toldErr := told.r.Pull(PullOptions{Rebase: &merge})

		if err != nil || configured.headCommit().NumParents() != 1 {
			t.Errorf("pull.rebase was not followed: err %v", err)
		}
		if toldErr != nil || told.headCommit().NumParents() != 2 {
			t.Errorf("rebase = false did not merge: err %v", toldErr)
		}
	})

	t.Run("GN-06 pull takes another branch of the remote when named", func(t *testing.T) {
		mine, theirs := withRemote(t)
		theirs.branch("feature", true)
		theirs.commit("feature work", map[string]string{"feature.txt": "f\n"})
		if _, err := theirs.r.Push(""); err != nil {
			t.Fatal(err)
		}

		pulled, err := mine.r.Pull(PullOptions{Branch: "feature"})

		if err != nil || mine.read("feature.txt") != "f\n" || !strings.Contains(pulled, "fast-forwarded") {
			t.Errorf("pulled = %q err = %v", pulled, err)
		}
		if _, missing := mine.r.Pull(PullOptions{Branch: "nowhere"}); missing == nil || !strings.Contains(missing.Error(), "no branch nowhere") {
			t.Errorf("missing branch: %v", missing)
		}
	})

	t.Run("GN-07 pull takes the branch the current one tracks", func(t *testing.T) {
		mine, theirs := withRemote(t)
		theirs.branch("trunk", true)
		theirs.commit("trunk work", map[string]string{"trunk.txt": "t\n"})
		if _, err := theirs.r.Push(""); err != nil {
			t.Fatal(err)
		}
		cfg, _ := mine.repo.Config()
		cfg.Branches["master"] = &config.Branch{Name: "master", Remote: "origin", Merge: "refs/heads/trunk"}
		if err := mine.repo.SetConfig(cfg); err != nil {
			t.Fatal(err)
		}

		_, err := mine.r.Pull(PullOptions{})

		if err != nil || mine.read("trunk.txt") != "t\n" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestUpstream(t *testing.T) {
	t.Run("GN-08 after a fetch, the remote branches are listed and a branch says how far behind its upstream it is", func(t *testing.T) {
		mine, theirs := withRemote(t)
		theirs.commit("one", map[string]string{"one.txt": "1\n"})
		theirs.commit("two", map[string]string{"two.txt": "2\n"})
		if _, err := theirs.r.Push(""); err != nil {
			t.Fatal(err)
		}
		before := mine.r.Upstream("master")
		if _, err := mine.r.Fetch(""); err != nil {
			t.Fatal(err)
		}

		upstream := mine.r.Upstream("master")
		behind, err := mine.r.Behind("master", upstream)

		if before != "origin/master" || upstream != "origin/master" || err != nil || behind != 2 {
			t.Errorf("upstream %q then %q, behind %d, err %v", before, upstream, behind, err)
		}
		if names := mine.r.RemoteBranchNames(); strings.Join(names, ",") != "origin/master" {
			t.Errorf("remote branches %v", names)
		}
		if mine.r.Upstream("nowhere") != "" {
			t.Error("a branch with no remote has an upstream")
		}
	})
}

// divergedClones gives mine a commit of its own and the remote one of theirs, on
// different files.
func divergedClones(t *testing.T) (*testRepo, *testRepo) {
	t.Helper()
	mine, theirs := withRemote(t)
	theirs.commit("their work", map[string]string{"theirs.txt": "t\n"})
	if _, err := theirs.r.Push(""); err != nil {
		t.Fatal(err)
	}
	mine.commit("my work", map[string]string{"mine.txt": "m\n"})
	return mine, theirs
}

func needsGitServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git-upload-pack"); err != nil {
		t.Skip("a folder remote without git is served by go-git's server, which leaves objects out of this fetch")
	}
}

// setOption writes a key of the repository's git config. Branch sections
// are written from go-git's own fields, so set those for them instead.
func setOption(t *testing.T, tr *testRepo, section, subsection, key, value string) {
	t.Helper()
	cfg, err := tr.repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	if subsection == "" {
		cfg.Raw.Section(section).SetOption(key, value)
	} else {
		cfg.Raw.Section(section).Subsection(subsection).SetOption(key, value)
	}
	if err := tr.repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
}
