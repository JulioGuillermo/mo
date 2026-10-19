package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | GA-01 | Happy    | What is typed after each action turns into the git tool's arguments          | P0       |
// | GA-02 | Error    | An action missing what it needs, or unknown, says how it is typed            | P1       |
// | GA-03 | Happy    | /git add then /git commit stage and commit, each shown as a git call         | P0       |
// | GA-04 | Edge     | While the model works, an action that changes the repository waits; one that reads runs | P0 |
// | GA-05 | Happy    | /git show <revision> opens that commit's changes in the viewer               | P1       |
// | GA-06 | Happy    | Tab offers the actions, and branches after switch                            | P2       |

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/gitops"
)

func TestGitActions(t *testing.T) {
	t.Run("GA-01 what is typed after each action turns into the git tool's arguments", func(t *testing.T) {
		noRepo := func(string) bool { return false }
		cases := map[string]string{
			"status":                           `{"action":"status"}`,
			"diff --staged a.go":               `{"action":"diff","paths":["a.go"],"staged":true}`,
			"log -n 5 src":                     `{"action":"log","count":5,"path":"src"}`,
			"log -3":                           `{"action":"log","count":3}`,
			"switch -c feature":                `{"action":"switch","create":true,"name":"feature"}`,
			"branch feature":                   `{"action":"branch","name":"feature"}`,
			"add a.go b.go":                    `{"action":"add","paths":["a.go","b.go"]}`,
			`commit -m "fix: the bug"`:         `{"action":"commit","message":"fix: the bug"}`,
			"commit fix it":                    `{"action":"commit","message":"fix it"}`,
			"pull origin main --rebase":        `{"action":"pull","name":"main","rebase":true,"remote":"origin"}`,
			"push upstream":                    `{"action":"push","remote":"upstream"}`,
			"cherry-pick abc123":               `{"action":"cherry_pick","revision":"abc123"}`,
			"stash wip on login":               `{"action":"stash","message":"wip on login"}`,
			"stash list":                       `{"action":"stash_list"}`,
			"stash pop 1":                      `{"action":"stash_pop","index":1}`,
			"tag v1.0 HEAD~1 -m first release": `{"action":"tag","message":"first release","name":"v1.0","revision":"HEAD~1"}`,
			"reset HEAD~1 --hard":              `{"action":"reset","mode":"hard","revision":"HEAD~1"}`,
		}
		for typed, want := range cases {
			args, err := gitToolArgs(parseGitRequest(typed, noRepo))
			got, _ := json.Marshal(args)
			if err != nil || string(got) != want {
				t.Errorf("%q = %s %v, want %s", typed, got, err, want)
			}
		}
	})

	t.Run("GA-02 an action missing what it needs, or unknown, says how it is typed", func(t *testing.T) {
		noRepo := func(string) bool { return false }
		for typed, want := range map[string]string{
			"commit":       "usage: /git commit [repo] <message>",
			"merge":        "usage: /git merge [repo] <revision>",
			"status extra": "usage: /git status [repo]",
			"frobnicate":   `no action "frobnicate"`,
		} {
			_, err := gitToolArgs(parseGitRequest(typed, noRepo))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%q: %v", typed, err)
			}
		}
	})

	t.Run("GA-03 /git add then /git commit stage and commit, each shown as a git call", func(t *testing.T) {
		m, dir := gitModel(t)
		writeFile(t, filepath.Join(dir, "new.txt"), "hello\n")

		updated, cmd := m.runCommand("/git add new.txt")
		m = deliver(updated.(model), cmd, 2)
		updated, cmd = m.runCommand("/git commit -m add new")
		m = deliver(updated.(model), cmd, 2)

		repo, _ := gitops.Open(dir)
		log, _ := repo.Log(1, "")
		if !strings.Contains(log, "add new") {
			t.Errorf("log = %q", log)
		}
		if text := shown(m); !strings.Contains(text, "git add new.txt") || !strings.Contains(text, "staged new.txt") || !strings.Contains(text, "committed") {
			t.Errorf("shown:\n%s", text)
		}
	})

	t.Run("GA-04 while the model works, an action that changes the repository waits; one that reads runs", func(t *testing.T) {
		m, dir := gitModel(t)
		writeFile(t, filepath.Join(dir, "new.txt"), "hello\n")
		m.running = true

		updated, _ := m.runCommand("/git add")
		m = updated.(model)
		updated, cmd := m.runCommand("/git status")
		m = deliver(updated.(model), cmd, 2)

		repo, _ := gitops.Open(dir)
		status, _ := repo.Status()
		if !strings.Contains(shown(m), "run it once it ends") || !strings.Contains(status, "untracked: new.txt") || !strings.Contains(shown(m), "untracked: new.txt") {
			t.Errorf("status = %q shown:\n%s", status, shown(m))
		}
	})

	t.Run("GA-05 /git show <revision> opens that commit's changes in the viewer", func(t *testing.T) {
		m, _ := gitModel(t)

		m = runGit(t, m, "show HEAD")

		if m.viewer == nil || m.viewer.changes == nil || len(m.viewer.changes.comparison.Files) != 2 || !strings.Contains(m.viewer.changes.title, "Commit HEAD") {
			t.Fatalf("not opened:\n%s", shown(m))
		}
	})

	t.Run("GA-06 tab offers the actions, and branches after switch", func(t *testing.T) {
		m, dir := gitModel(t)
		repo, _ := gitops.Open(dir)
		repo.Branch("feature")

		actions := suggestionNames(m.gitSuggestions("st"))
		branches := suggestionNames(m.gitSuggestions("switch f"))

		if strings.Join(actions, ",") != "git status,git stash" || strings.Join(branches, ",") != "git switch feature" {
			t.Errorf("actions = %v branches = %v", actions, branches)
		}
	})
}
