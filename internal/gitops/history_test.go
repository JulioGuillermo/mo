package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GH-01 | Happy    | A file's history is the commits that changed it, the newest first    | P0       |
// | GH-02 | Edge     | A merge that changed the file is listed; one that took a side's version is not | P0 |
// | GH-03 | Happy    | FileChange is the file before and after a commit; UncommittedFile, the disk against HEAD | P0 |
// | GH-04 | Happy    | Blame names each line's commit, author and subject; lines changed on disk are not committed | P0 |
// | GH-05 | Edge     | A file not committed yet has every line not committed               | P1       |

import (
	"slices"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// commitAt commits files as Ana at a time, so the order of commits is known.
func (tr *testRepo) commitAt(message string, when time.Time, files map[string]string) string {
	tr.t.Helper()
	tr.write(files)
	for path := range files {
		if _, err := tr.tree.Add(path); err != nil {
			tr.t.Fatal(err)
		}
	}
	signature := &object.Signature{Name: "Ana", Email: "ana@x", When: when}
	hash, err := tr.tree.Commit(message, &git.CommitOptions{Author: signature, Committer: signature})
	if err != nil {
		tr.t.Fatal(err)
	}
	return hash.String()
}

func subjects(history []FileCommit) []string {
	var out []string
	for _, commit := range history {
		out = append(out, commit.Subject)
	}
	return out
}

func TestFileHistory(t *testing.T) {
	later := time.Now().Add(time.Hour)

	t.Run("GH-01 a file's history is the commits that changed it, the newest first", func(t *testing.T) {
		tr := newRepo(t)
		tr.commitAt("other file", later, map[string]string{"other.txt": "x\n"})
		tr.commitAt("change a", later.Add(time.Minute), map[string]string{"main.go": "package main\n\nfunc a() { one() }\n\nfunc b() {}\n\nfunc c() {}\n"})
		tr.commitAt("change c", later.Add(2*time.Minute), map[string]string{"main.go": "package main\n\nfunc a() { one() }\n\nfunc b() {}\n\nfunc c() { two() }\n"})

		history, err := tr.r.FileHistory("main.go")

		if err != nil || !slices.Equal(subjects(history), []string{"change c", "change a", "first"}) {
			t.Errorf("history = %q err = %v", subjects(history), err)
		}
		if history[0].Author != "Ana" || len(history[0].Short()) != 7 {
			t.Errorf("newest = %+v", history[0])
		}
	})

	t.Run("GH-02 a merge that changed the file is listed; one that took a side's version is not", func(t *testing.T) {
		tr := diverged(t, "package main\n\nfunc a() { master() }\n\nfunc b() {}\n\nfunc c() {}\n")
		if _, err := tr.r.Merge("feature"); err != nil {
			t.Fatal(err)
		}

		main, _ := tr.r.FileHistory("main.go")
		added, _ := tr.r.FileHistory("added.go")

		mainSubjects := subjects(main)
		if len(main) != 4 || !slices.Contains(mainSubjects, "Merge feature into master") {
			t.Errorf("main.go = %q", mainSubjects)
		}
		if !slices.Equal(subjects(added), []string{"feature changes b"}) {
			t.Errorf("added.go = %q", subjects(added))
		}
	})

	t.Run("GH-03 FileChange is the file before and after a commit; UncommittedFile, the disk against HEAD", func(t *testing.T) {
		tr := newRepo(t)
		hash := tr.commitAt("change a", later, map[string]string{"main.go": "package main\n\nfunc a() { one() }\n"})

		change, err := tr.r.FileChange(hash, "main.go")
		before, after := change.Contents("main.go")
		_, cleanOK := tr.r.UncommittedFile("main.go")
		tr.write(map[string]string{"main.go": "package main\n\nfunc a() { two() }\n"})
		uncommitted, changedOK := tr.r.UncommittedFile("main.go")
		_, onDisk := uncommitted.Contents("main.go")

		if err != nil || before != "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() {}\n" || after != "package main\n\nfunc a() { one() }\n" {
			t.Errorf("before %q after %q err %v", before, after, err)
		}
		if cleanOK || !changedOK || onDisk != "package main\n\nfunc a() { two() }\n" || len(uncommitted.Files) != 1 {
			t.Errorf("clean %v changed %v disk %q", cleanOK, changedOK, onDisk)
		}
	})

	t.Run("GH-04 blame names each line's commit, author and subject; lines changed on disk are not committed", func(t *testing.T) {
		tr := newRepo(t)
		hash := tr.commitAt("change a", later, map[string]string{"main.go": "package main\n\nfunc a() { one() }\n\nfunc b() {}\n\nfunc c() {}\n"})
		tr.write(map[string]string{"main.go": "package main\n\nfunc a() { one() }\n\nfunc b() { mine() }\n\nfunc c() {}\n"})

		blame, err := tr.r.BlameFile("main.go")

		if err != nil || len(blame) != 7 {
			t.Fatalf("blame = %+v err = %v", blame, err)
		}
		if blame[2].Hash != hash || blame[2].Subject != "change a" || blame[2].Author != "Ana" || blame[0].Subject != "first" {
			t.Errorf("committed lines = %+v / %+v", blame[2], blame[0])
		}
		if blame[4].Committed() || !blame[6].Committed() {
			t.Errorf("line 5 = %+v line 7 = %+v", blame[4], blame[6])
		}
	})

	t.Run("GH-05 a file not committed yet has every line not committed", func(t *testing.T) {
		tr := newRepo(t)
		tr.write(map[string]string{"new.go": "one\ntwo\n"})

		blame, err := tr.r.BlameFile("new.go")

		if err != nil || len(blame) != 2 || blame[0].Committed() || blame[1].Committed() {
			t.Errorf("blame = %+v err = %v", blame, err)
		}
	})
}
