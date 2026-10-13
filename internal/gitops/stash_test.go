package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GS-01 | Happy    | stash saves changes to tracked files and leaves HEAD's; pop brings them back | P0 |
// | GS-02 | Happy    | Stashes stack, the newest first; drop removes one                    | P1       |
// | GS-03 | Happy    | A stash applies over new commits; a clash leaves markers and keeps it | P0      |
// | GS-04 | Edge     | The git command lists the stash mo saved                             | P2       |
// | GT-01 | Happy    | Tags are made light or annotated and listed with their commit        | P1       |
// | GZ-01 | Happy    | reset soft keeps the changes staged, mixed keeps the files, hard drops them | P0 |
// | GU-01 | Security | Untracked files survive switch, merge, stash, rebase, abort and reset hard | P0  |

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const changedA = "package main\n\nfunc a() { stashed() }\n\nfunc b() {}\n\nfunc c() {}\n"

func TestStash(t *testing.T) {
	t.Run("GS-01 stash saves changes to tracked files and leaves HEAD's; pop brings them back", func(t *testing.T) {
		tr := newRepo(t)
		head := tr.headCommit().Hash
		tr.write(map[string]string{"main.go": changedA, "untracked.txt": "u"})

		stashed, err := tr.r.Stash("")
		clean := tr.read("main.go")
		popped, popErr := tr.r.ApplyStash(0, true)

		if err != nil || !strings.Contains(stashed, "stashed 1 files") || strings.Contains(clean, "stashed()") || tr.headCommit().Hash != head || tr.read("untracked.txt") != "u" {
			t.Fatalf("stashed = %q err = %v main.go = %q", stashed, err, clean)
		}
		list, _ := tr.r.Stashes()
		if popErr != nil || !strings.Contains(popped, "applied to 1 files and dropped") || tr.read("main.go") != changedA || list != "no stashes" {
			t.Errorf("popped = %q err = %v list = %q", popped, popErr, list)
		}
		if status, _ := tr.r.Status(); !strings.Contains(status, "not staged: M main.go") {
			t.Errorf("status:\n%s", status)
		}
	})

	t.Run("GS-02 stashes stack, the newest first; drop removes one", func(t *testing.T) {
		tr := newRepo(t)
		tr.write(map[string]string{"main.go": changedA})
		_, _ = tr.r.Stash("first")
		tr.write(map[string]string{"main.go": "package main\n"})
		_, _ = tr.r.Stash("second")

		listed, _ := tr.r.Stashes()
		dropped, _ := tr.r.DropStash(0)
		left, _ := tr.r.Stashes()

		if listed != "stash 0: WIP on master: second\nstash 1: WIP on master: first" || !strings.Contains(dropped, "second") || left != "stash 0: WIP on master: first" {
			t.Errorf("listed = %q dropped = %q left = %q", listed, dropped, left)
		}
	})

	t.Run("GS-03 a stash applies over new commits; a clash leaves markers and keeps it", func(t *testing.T) {
		tr := newRepo(t)
		tr.write(map[string]string{"main.go": changedA})
		_, _ = tr.r.Stash("")
		tr.commit("other line", map[string]string{"main.go": "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() { later() }\n"})
		clean, err := tr.r.ApplyStash(0, false)
		merged := tr.read("main.go")
		_, _ = tr.r.Reset("", "hard")
		tr.commit("same line", map[string]string{"main.go": "package main\n\nfunc a() { committed() }\n\nfunc b() {}\n\nfunc c() { later() }\n"})

		clash, _ := tr.r.ApplyStash(0, true)

		if err != nil || !strings.Contains(clean, "applied") || !strings.Contains(merged, "stashed()") || !strings.Contains(merged, "later()") {
			t.Errorf("clean = %q err = %v merged =\n%s", clean, err, merged)
		}
		if list, _ := tr.r.Stashes(); !strings.Contains(clash, "conflicts in main.go; it is kept") || !strings.Contains(tr.read("main.go"), "<<<<<<<") || list == "no stashes" {
			t.Errorf("clash = %q list = %q", clash, list)
		}
	})

	t.Run("GS-04 the git command lists the stash mo saved", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git command here")
		}
		tr := newRepo(t)
		tr.write(map[string]string{"main.go": changedA})
		_, _ = tr.r.Stash("for git")

		command := exec.Command("git", "-C", tr.dir, "stash", "list")
		command.Env = append(os.Environ(), "LC_ALL=C")
		output, err := command.CombinedOutput()

		if err != nil || !strings.Contains(string(output), "stash@{0}: WIP on master: for git") {
			t.Errorf("git stash list = %q err = %v", output, err)
		}
	})
}

func TestTagsAndReset(t *testing.T) {
	t.Run("GT-01 tags are made light or annotated and listed with their commit", func(t *testing.T) {
		tr := newRepo(t)
		head := tr.headCommit().Hash.String()[:7]

		_, lightErr := tr.r.Tag("v1", "", "")
		_, annotatedErr := tr.r.Tag("v2", "", "release two")
		listed, _ := tr.r.Tags()

		if lightErr != nil || annotatedErr != nil || !strings.Contains(listed, "v1 → "+head) || !strings.Contains(listed, "v2 → "+head+"  release two") {
			t.Errorf("listed = %q errs = %v %v", listed, lightErr, annotatedErr)
		}
	})

	t.Run("GZ-01 reset soft keeps the changes staged, mixed keeps the files, hard drops them", func(t *testing.T) {
		tr := newRepo(t)
		first := tr.headCommit().Hash
		tr.commit("second", map[string]string{"main.go": changedA})

		_, softErr := tr.r.Reset("HEAD~1", "soft")
		soft, _ := tr.r.Status()
		tr.commit("again", map[string]string{"main.go": changedA})
		_, _ = tr.r.Reset("HEAD~1", "mixed")
		mixed, _ := tr.r.Status()
		_, _ = tr.r.Reset("", "hard")
		hard, _ := tr.r.Status()

		if softErr != nil || tr.headCommit().Hash != first || !strings.Contains(soft, "staged: M main.go") {
			t.Errorf("soft:\n%s err = %v", soft, softErr)
		}
		if !strings.Contains(mixed, "not staged: M main.go") || strings.Contains(mixed, "staged: M") && !strings.Contains(mixed, "not staged") || !strings.HasSuffix(hard, "clean") {
			t.Errorf("mixed:\n%s\nhard:\n%s", mixed, hard)
		}
	})
}

func TestUntrackedSurvive(t *testing.T) {
	t.Run("GU-01 untracked files survive switch, merge, stash, rebase, abort and reset hard", func(t *testing.T) {
		tr := branched(t, true)
		tr.write(map[string]string{"notes.txt": "mine"})

		steps := []func() error{
			func() error { _, err := tr.r.Switch("master", false); return err },
			func() error { _, err := tr.r.Switch("feature", false); return err },
			func() error { _, err := tr.r.Rebase("master"); return err },
			func() error { _, err := tr.r.Abort(); return err },
			func() error { _, err := tr.r.Reset("HEAD~1", "hard"); return err },
			func() error {
				tr.write(map[string]string{"a.txt": "changed\n"})
				_, err := tr.r.Stash("")
				return err
			},
		}
		for i, step := range steps {
			if err := step(); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			if tr.read("notes.txt") != "mine" {
				t.Fatalf("step %d deleted the untracked file", i)
			}
		}
	})
}
