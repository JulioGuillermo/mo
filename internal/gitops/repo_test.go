package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GR-01 | Happy    | status, diff, log, show, blame and branches read the repository      | P0       |
// | GR-02 | Happy    | A merge fast-forwards when HEAD is behind; nothing to do when ahead  | P1       |
// | GR-03 | Happy    | A three-way merge without conflicts commits with both parents        | P0       |
// | GR-04 | Happy    | A conflict stops with markers; commit and continue refuse until they are gone | P0 |
// | GR-05 | Happy    | Abort goes back to HEAD and removes the files the merge added        | P0       |
// | GR-06 | Happy    | A cherry-pick copies a commit with its author; a conflict stops it like a merge | P0 |
// | GR-07 | Error    | A merge or cherry-pick over uncommitted changes is refused           | P0       |
// | GR-08 | Edge     | The git command sees the merge mo left in progress                   | P2       |
// | GR-09 | Security | A plain commit is refused while a changed file holds conflict markers | P1      |

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type testRepo struct {
	t    *testing.T
	dir  string
	repo *git.Repository
	tree *git.Worktree
	r    *Repo
}

func newRepo(t *testing.T) *testRepo {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := repo.Worktree()
	test := &testRepo{t: t, dir: dir, repo: repo, tree: tree}
	test.commit("first", map[string]string{"main.go": "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() {}\n"})
	opened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	test.r = opened
	return test
}

func (tr *testRepo) write(files map[string]string) {
	tr.t.Helper()
	for path, content := range files {
		full := filepath.Join(tr.dir, path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			tr.t.Fatal(err)
		}
	}
}

func (tr *testRepo) commit(message string, files map[string]string) plumbing.Hash {
	tr.t.Helper()
	tr.write(files)
	for path := range files {
		if _, err := tr.tree.Add(path); err != nil {
			tr.t.Fatal(err)
		}
	}
	hash, err := tr.tree.Commit(message, &git.CommitOptions{Author: &object.Signature{Name: "Ana", Email: "ana@x", When: time.Now()}})
	if err != nil {
		tr.t.Fatal(err)
	}
	return hash
}

func (tr *testRepo) branch(name string, create bool) {
	tr.t.Helper()
	if _, err := tr.r.Switch(name, create); err != nil {
		tr.t.Fatal(err)
	}
}

func (tr *testRepo) read(path string) string {
	data, _ := os.ReadFile(filepath.Join(tr.dir, path))
	return string(data)
}

func (tr *testRepo) headCommit() *object.Commit {
	ref, _ := tr.repo.Head()
	commit, _ := tr.repo.CommitObject(ref.Hash())
	return commit
}

// diverged makes branch feature change b(), and master change a() or b().
func diverged(t *testing.T, masterChange string) *testRepo {
	tr := newRepo(t)
	tr.branch("feature", true)
	tr.commit("feature changes b", map[string]string{"main.go": "package main\n\nfunc a() {}\n\nfunc b() { feature() }\n\nfunc c() {}\n", "added.go": "package main\n"})
	tr.branch("master", false)
	tr.commit("master changes", map[string]string{"main.go": masterChange})
	return tr
}

func TestReading(t *testing.T) {
	t.Run("GR-01 status, diff, log, show, blame and branches read the repository", func(t *testing.T) {
		tr := newRepo(t)
		tr.write(map[string]string{"main.go": "package main\n\nfunc a() { changed() }\n\nfunc b() {}\n\nfunc c() {}\n", "new.txt": "x"})

		status, _ := tr.r.Status()
		diff, _ := tr.r.Diff(false, nil)
		log, _ := tr.r.Log(5, "")
		show, _ := tr.r.Show("HEAD")
		blame, _ := tr.r.Blame("main.go")
		branches, _ := tr.r.Branches()

		if !strings.Contains(status, "branch master") || !strings.Contains(status, "not staged: M main.go") || !strings.Contains(status, "untracked: new.txt") {
			t.Errorf("status:\n%s", status)
		}
		if !strings.Contains(diff, "-func a() {}\n+func a() { changed() }") {
			t.Errorf("diff:\n%s", diff)
		}
		if !strings.Contains(log, "Ana  first") || !strings.Contains(show, "+func a() {}") || !strings.Contains(blame, "Ana") || branches != "* master" {
			t.Errorf("log = %q show = %q blame = %q branches = %q", log, show, blame, branches)
		}
	})
}

func TestMerging(t *testing.T) {
	t.Run("GR-02 a merge fast-forwards when HEAD is behind; nothing to do when ahead", func(t *testing.T) {
		tr := newRepo(t)
		tr.branch("feature", true)
		ahead := tr.commit("more", map[string]string{"x.txt": "x"})
		tr.branch("master", false)

		forwarded, err := tr.r.Merge("feature")
		again, _ := tr.r.Merge("feature")

		if err != nil || !strings.Contains(forwarded, "fast-forwarded") || tr.headCommit().Hash != ahead || tr.read("x.txt") != "x" || !strings.Contains(again, "already up to date") {
			t.Errorf("forwarded = %q err = %v again = %q", forwarded, err, again)
		}
	})

	t.Run("GR-03 a three-way merge without conflicts commits with both parents", func(t *testing.T) {
		tr := diverged(t, "package main\n\nfunc a() { master() }\n\nfunc b() {}\n\nfunc c() {}\n")

		merged, err := tr.r.Merge("feature")

		head := tr.headCommit()
		if err != nil || !strings.Contains(merged, "merge committed") || head.NumParents() != 2 {
			t.Fatalf("merged = %q err = %v parents = %d", merged, err, head.NumParents())
		}
		if tr.read("main.go") != "package main\n\nfunc a() { master() }\n\nfunc b() { feature() }\n\nfunc c() {}\n" || tr.read("added.go") != "package main\n" {
			t.Errorf("main.go =\n%s", tr.read("main.go"))
		}
		if status, _ := tr.r.Status(); !strings.HasSuffix(status, "clean") {
			t.Errorf("status:\n%s", status)
		}
	})

	t.Run("GR-04 a conflict stops with markers; commit and continue refuse until they are gone", func(t *testing.T) {
		tr := diverged(t, "package main\n\nfunc a() {}\n\nfunc b() { master() }\n\nfunc c() {}\n")

		stopped, err := tr.r.Merge("feature")
		check, _ := tr.r.Check()
		_, refusedCommit := tr.r.Commit("done")
		_, refusedContinue := tr.r.Continue()

		if err != nil || !strings.Contains(stopped, "conflicts in 1 files: main.go") || !strings.Contains(stopped, "<<<<<<< master") || !strings.Contains(stopped, ">>>>>>> feature") {
			t.Fatalf("stopped = %q err = %v", stopped, err)
		}
		if !strings.Contains(check, "merge of") || !strings.Contains(check, "main.go: line 5") {
			t.Errorf("check:\n%s", check)
		}
		if refusedCommit == nil || refusedContinue == nil || !strings.Contains(refusedContinue.Error(), "main.go: line 5") {
			t.Errorf("commit = %v continue = %v", refusedCommit, refusedContinue)
		}

		tr.write(map[string]string{"main.go": "package main\n\nfunc a() {}\n\nfunc b() { master(); feature() }\n\nfunc c() {}\n"})
		finished, err := tr.r.Continue()

		head := tr.headCommit()
		if err != nil || !strings.Contains(finished, "merge committed") || head.NumParents() != 2 || tr.r.inProgress() != nil {
			t.Errorf("finished = %q err = %v parents = %d", finished, err, head.NumParents())
		}
		if file, err := head.File("added.go"); err != nil || file == nil {
			t.Error("the file the merge brought was not committed")
		}
	})

	t.Run("GR-05 abort goes back to HEAD and removes the files the merge added", func(t *testing.T) {
		tr := diverged(t, "package main\n\nfunc a() {}\n\nfunc b() { master() }\n\nfunc c() {}\n")
		before := tr.headCommit().Hash
		_, _ = tr.r.Merge("feature")

		aborted, err := tr.r.Abort()

		_, statErr := os.Stat(filepath.Join(tr.dir, "added.go"))
		if err != nil || !strings.Contains(aborted, "aborted") || tr.headCommit().Hash != before || !strings.Contains(tr.read("main.go"), "b() { master() }") || statErr == nil || tr.r.inProgress() != nil {
			t.Errorf("aborted = %q err = %v added left = %v", aborted, err, statErr == nil)
		}
	})

	t.Run("GR-06 a cherry-pick copies a commit with its author; a conflict stops it like a merge", func(t *testing.T) {
		tr := newRepo(t)
		tr.branch("feature", true)
		clean := tr.commit("add helper", map[string]string{"helper.go": "package main\n"})
		clashing := tr.commit("change c", map[string]string{"main.go": "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() { feature() }\n"})
		tr.branch("master", false)
		tr.commit("master c", map[string]string{"main.go": "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() { master() }\n"})

		picked, err := tr.r.CherryPick(clean.String())
		head := tr.headCommit()
		stopped, _ := tr.r.CherryPick(clashing.String())
		state := tr.r.inProgress()

		if err != nil || !strings.Contains(picked, "cherry-pick committed") || head.Author.Name != "Ana" || !strings.Contains(head.Message, "(cherry picked from commit "+clean.String()+")") || head.NumParents() != 1 {
			t.Errorf("picked = %q err = %v message = %q", picked, err, head.Message)
		}
		if !strings.Contains(stopped, "cherry-pick stopped at conflicts") || state == nil || state.kind != "cherry-pick" {
			t.Errorf("stopped = %q", stopped)
		}
	})

	t.Run("GR-07 a merge or cherry-pick over uncommitted changes is refused", func(t *testing.T) {
		tr := diverged(t, "package main\n\nfunc a() { master() }\n\nfunc b() {}\n\nfunc c() {}\n")
		tr.write(map[string]string{"main.go": "unsaved work\n"})

		_, mergeErr := tr.r.Merge("feature")
		_, pickErr := tr.r.CherryPick("feature")

		if mergeErr == nil || !strings.Contains(mergeErr.Error(), "main.go") || pickErr == nil || tr.read("main.go") != "unsaved work\n" {
			t.Errorf("merge = %v pick = %v", mergeErr, pickErr)
		}
	})

	t.Run("GR-08 the git command sees the merge mo left in progress", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git command here")
		}
		tr := diverged(t, "package main\n\nfunc a() {}\n\nfunc b() { master() }\n\nfunc c() {}\n")
		_, _ = tr.r.Merge("feature")

		command := exec.Command("git", "-C", tr.dir, "status")
		command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
		output, err := command.CombinedOutput()

		if err != nil || !strings.Contains(strings.ToLower(string(output)), "merg") {
			t.Errorf("git status:\n%s", output)
		}
	})

	t.Run("GR-09 a plain commit is refused while a changed file holds conflict markers", func(t *testing.T) {
		tr := newRepo(t)
		tr.write(map[string]string{"main.go": "<<<<<<< HEAD\na\n=======\nb\n>>>>>>> other\n"})
		_, _ = tr.r.Add(nil)

		_, err := tr.r.Commit("oops")

		if err == nil || !strings.Contains(err.Error(), "main.go: line 1") {
			t.Errorf("err = %v", err)
		}
	})
}
