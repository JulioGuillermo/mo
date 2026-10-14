package agent

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | IN-01 | Happy    | The user's file, then the repository root's, then the working folder's     | P0       |
// | IN-02 | Edge     | Without a repository only the working folder is read; empty files are left out | P1    |
// | IN-03 | Happy    | The prompt holds each file with its path; none adds nothing                | P0       |
// | IN-04 | Edge     | A long file is read whole; one reached twice by a link is read once      | P2       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstructions(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func paths(found []Instruction) []string {
	var out []string
	for _, instruction := range found {
		out = append(out, instruction.Path)
	}
	return out
}

func TestInstructions(t *testing.T) {
	t.Run("IN-01 the user's file, then the repository root's, then the working folder's", func(t *testing.T) {
		user, repo := t.TempDir(), t.TempDir()
		os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
		work := filepath.Join(repo, "services", "api")
		writeInstructions(t, filepath.Join(user, "AGENTS.md"), "mine")
		writeInstructions(t, filepath.Join(repo, "AGENTS.md"), "root")
		writeInstructions(t, filepath.Join(repo, "services", "AGENT.md"), "services")
		writeInstructions(t, filepath.Join(work, "AGENTS.md"), "api")

		found := FindInstructions([]string{"AGENTS.md", "AGENT.md"}, user, work)

		want := []string{filepath.Join(user, "AGENTS.md"), filepath.Join(repo, "AGENTS.md"), filepath.Join(repo, "services", "AGENT.md"), filepath.Join(work, "AGENTS.md")}
		if strings.Join(paths(found), "\n") != strings.Join(want, "\n") {
			t.Errorf("found:\n%s", strings.Join(paths(found), "\n"))
		}
	})

	t.Run("IN-02 without a repository only the working folder is read; empty files are left out", func(t *testing.T) {
		outside := t.TempDir()
		work := filepath.Join(outside, "app")
		writeInstructions(t, filepath.Join(outside, "AGENTS.md"), "above, not a repository")
		writeInstructions(t, filepath.Join(work, "AGENTS.md"), "  \n")
		writeInstructions(t, filepath.Join(work, "CLAUDE.md"), "claude")

		found := FindInstructions([]string{"AGENTS.md", "CLAUDE.md"}, "", work)

		if len(found) != 1 || found[0].Path != filepath.Join(work, "CLAUDE.md") {
			t.Errorf("found = %v", paths(found))
		}
	})

	t.Run("IN-03 the prompt holds each file with its path; none adds nothing", func(t *testing.T) {
		with := SystemPrompt("", "/w", []Instruction{{Path: "/w/AGENTS.md", Text: "Run go test before finishing."}})
		without := SystemPrompt("", "/w", nil)

		if !strings.Contains(with, `<instructions path="/w/AGENTS.md">`+"\nRun go test before finishing.\n</instructions>") || strings.Contains(without, "instructions") {
			t.Errorf("with:\n%s\nwithout:\n%s", with, without)
		}
	})

	t.Run("IN-04 a long file is read whole; one reached twice by a link is read once", func(t *testing.T) {
		work := t.TempDir()
		writeInstructions(t, filepath.Join(work, "AGENTS.md"), strings.Repeat("x", 100000))
		os.Symlink(filepath.Join(work, "AGENTS.md"), filepath.Join(work, "CLAUDE.md"))

		found := FindInstructions([]string{"AGENTS.md", "CLAUDE.md"}, "", work)

		if len(found) != 1 || len(found[0].Text) != 100000 {
			t.Errorf("found %d, text %d bytes", len(found), len(found[0].Text))
		}
	})
}
