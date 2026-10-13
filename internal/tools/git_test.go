package tools

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GT-01 | Happy    | The git tool reads and commits in the working directory, no git command needed | P0 |
// | GT-02 | Happy    | Reading actions change nothing; the others count as changes          | P0       |
// | GT-03 | Error    | Outside a repository it says so                                      | P2       |
// | GT-04 | Security | A push and a hard reset warn, so they ask in every mode but all       | P0       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/jg/mo/internal/llm"
)

func TestGitTool(t *testing.T) {
	t.Run("GT-01 the git tool reads and commits in the working directory, no git command needed", func(t *testing.T) {
		t.Setenv("PATH", "")
		t.Setenv("HOME", t.TempDir())
		dir := t.TempDir()
		if _, err := git.PlainInit(dir, false); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)
		registry := NewRegistry(Env{WorkDir: dir}, Options{MaxOutput: 10000})
		run := func(fields map[string]any) string {
			output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, fields)})
			if isError {
				t.Fatalf("%v: %s", fields, output)
			}
			return output
		}

		before := run(map[string]any{"action": "status"})
		run(map[string]any{"action": "add"})
		committed := run(map[string]any{"action": "commit", "message": "first"})
		log := run(map[string]any{"action": "log"})

		if !strings.Contains(before, "untracked: a.txt") || !strings.Contains(committed, "committed") || !strings.Contains(log, "first") {
			t.Errorf("before = %q committed = %q log = %q", before, committed, log)
		}
	})

	t.Run("GT-02 reading actions change nothing; the others count as changes", func(t *testing.T) {
		registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{})
		changes := func(action string) bool {
			return registry.Changes(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": action})})
		}

		for _, action := range []string{"status", "diff", "log", "show", "blame", "branches", "check", "fetch", "stash_list", "tags"} {
			if changes(action) {
				t.Errorf("%s counts as a change", action)
			}
		}
		for _, action := range []string{"add", "commit", "merge", "cherry_pick", "continue", "abort", "switch", "branch", "rebase", "pull", "push", "stash", "stash_pop", "stash_drop", "tag", "reset"} {
			if !changes(action) {
				t.Errorf("%s does not count as a change", action)
			}
		}
	})

	t.Run("GT-03 outside a repository it says so", func(t *testing.T) {
		registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "status"})})

		if !isError || !strings.Contains(output, "not a git repository") {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("GT-04 a push and a hard reset warn, so they ask in every mode but all", func(t *testing.T) {
		registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{})

		push := registry.Warning(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "push", "remote": "upstream"})})
		pull := registry.Warning(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "pull"})})

		hard := registry.Warning(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "reset", "mode": "hard"})})
		soft := registry.Warning(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "reset", "mode": "soft"})})

		if push != "publishes to upstream" || pull != "" || hard != "drops the changes not committed" || soft != "" {
			t.Errorf("push = %q pull = %q hard = %q soft = %q", push, pull, hard, soft)
		}
	})
}
