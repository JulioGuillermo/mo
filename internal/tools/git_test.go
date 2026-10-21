package tools

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GT-01 | Happy    | The git tool reads and commits in the working directory, no git command needed | P0 |
// | GT-02 | Happy    | Reading actions change nothing; the others count as changes          | P0       |
// | GT-03 | Error    | Outside a repository it says so                                      | P2       |
// | GT-04 | Security | A push and a hard reset warn, so they ask in every mode but all       | P0       |
// | GT-05 | Happy    | repo picks the repository, relative or absolute, its paths taken from it | P0     |
// | GT-06 | Error    | Outside a repository the error lists those below and how deep it looked | P1      |
// | GT-07 | Security | A repo outside the working directory warns, whatever the action     | P0       |
// | GT-08 | Happy    | repos lists those below and any opened, each with its branch          | P1       |
// | GT-09 | Happy    | init makes the repository and its folder, listed in repos; a second init is refused | P0 |
// | GT-10 | Error    | merge, cherry_pick and rebase without revision say what they need    | P1       |
// | GT-11 | Happy    | name, branch, tag and commit name the revision; branch, tag and commit must be one | P0 |

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

	t.Run("GT-05 repo picks the repository, relative or absolute, its paths taken from it", func(t *testing.T) {
		workspace := t.TempDir()
		dir := filepath.Join(workspace, "mo")
		if _, err := git.PlainInit(dir, false); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)
		registry := NewRegistry(Env{WorkDir: workspace}, Options{MaxOutput: 10000})
		run := func(fields map[string]any) string {
			output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, fields)})
			if isError {
				t.Fatalf("%v: %s", fields, output)
			}
			return output
		}

		run(map[string]any{"action": "add", "repo": "./mo", "paths": []string{"a.txt"}})
		status := run(map[string]any{"action": "status", "repo": dir})

		if !strings.Contains(status, "a.txt") || strings.Contains(status, "untracked: a.txt") {
			t.Errorf("status = %q", status)
		}
	})

	t.Run("GT-06 outside a repository the error lists those below and how deep it looked", func(t *testing.T) {
		workspace := t.TempDir()
		for _, path := range []string{"api", "group/mo"} {
			if _, err := git.PlainInit(filepath.Join(workspace, path), false); err != nil {
				t.Fatal(err)
			}
		}
		registry := NewRegistry(Env{WorkDir: workspace}, Options{})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "status"})})

		if !isError || !strings.Contains(output, "repositories below it: api, group/mo") || !strings.Contains(output, "searched 2 levels below") || !strings.Contains(output, `"repo":"api"`) {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("GT-07 a repo outside the working directory warns, whatever the action", func(t *testing.T) {
		workspace := t.TempDir()
		registry := NewRegistry(Env{WorkDir: filepath.Join(workspace, "mo")}, Options{})
		warning := func(repo string) string {
			return registry.Warning(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "status", "repo": repo})})
		}

		if warning("../x") != warningOutsideWorkDir || warning(workspace) != warningOutsideWorkDir || warning("sub") != "" || warning("") != "" {
			t.Errorf("../x %q, absolute %q, sub %q, none %q", warning("../x"), warning(workspace), warning("sub"), warning(""))
		}
	})

	t.Run("GT-08 repos lists those below and any opened, each with its branch", func(t *testing.T) {
		workspace := t.TempDir()
		for _, path := range []string{"api", "deep/er/est"} {
			if _, err := git.PlainInit(filepath.Join(workspace, path), false); err != nil {
				t.Fatal(err)
			}
		}
		registry := NewRegistry(Env{WorkDir: workspace}, Options{})
		repos := func() string {
			output, _ := registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "repos"})})
			return output
		}

		before := repos()
		registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "status", "repo": "deep/er/est"})})
		after := repos()

		if !strings.Contains(before, "api  master") || strings.Contains(before, "deep/er/est") || !strings.Contains(after, "deep/er/est  master") {
			t.Errorf("before = %q after = %q", before, after)
		}
	})

	t.Run("GT-09 init makes the repository and its folder, listed in repos; a second init is refused", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		workspace := t.TempDir()
		registry := NewRegistry(Env{WorkDir: workspace}, Options{})
		run := func(fields map[string]any) (string, bool) {
			return registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, fields)})
		}

		made, failed := run(map[string]any{"action": "init", "repo": "deep/er/new"})
		repos, _ := run(map[string]any{"action": "repos"})
		again, refused := run(map[string]any{"action": "init", "repo": "deep/er/new"})
		changes := registry.Changes(llm.ToolCall{Name: "git", Args: args(t, map[string]any{"action": "init"})})

		if failed || !strings.Contains(made, "on branch main") || !strings.Contains(repos, "deep/er/new  main") {
			t.Errorf("made = %q repos = %q", made, repos)
		}
		if !refused || !strings.Contains(again, "already a git repository") || !changes {
			t.Errorf("again = %q refused = %v changes = %v", again, refused, changes)
		}
	})

	t.Run("GT-10 merge, cherry_pick and rebase without revision say what they need", func(t *testing.T) {
		run, _ := gitRepoTool(t)

		for _, action := range []string{"merge", "cherry_pick", "rebase"} {
			output, isError := run(map[string]any{"action": action})

			if !isError || !strings.Contains(output, action+" needs revision") {
				t.Errorf("%s: %q", action, output)
			}
		}
	})

	t.Run("GT-11 name, branch, tag and commit name the revision; branch, tag and commit must be one", func(t *testing.T) {
		run, hash := gitRepoTool(t)
		run(map[string]any{"action": "tag", "tag": "v1"})
		run(map[string]any{"action": "branch", "branch": "side"})

		byName, nameFailed := run(map[string]any{"action": "merge", "name": "side"})
		byTag, tagFailed := run(map[string]any{"action": "show", "tag": "v1"})
		byCommit, commitFailed := run(map[string]any{"action": "show", "commit": hash[:7]})
		notBranch, _ := run(map[string]any{"action": "merge", "branch": "v1"})
		notCommit, _ := run(map[string]any{"action": "show", "commit": "main"})
		twice, _ := run(map[string]any{"action": "merge", "revision": "side", "branch": "side"})

		if nameFailed || !strings.Contains(byName, "up to date with side") {
			t.Errorf("by name: %q", byName)
		}
		if tagFailed || commitFailed || !strings.Contains(byTag, "first") || !strings.Contains(byCommit, "first") {
			t.Errorf("by tag: %q by commit: %q", byTag, byCommit)
		}
		if !strings.Contains(notBranch, `no branch "v1"`) || !strings.Contains(notCommit, "not a commit hash") || !strings.Contains(twice, "give only one of revision, branch") {
			t.Errorf("not branch: %q not commit: %q twice: %q", notBranch, notCommit, twice)
		}
	})
}

// gitRepoTool is the git tool on a new repository with one commit, and
// that commit's hash.
func gitRepoTool(t *testing.T) (func(map[string]any) (string, bool), string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)
	registry := NewRegistry(Env{WorkDir: dir}, Options{MaxOutput: 10000})
	run := func(fields map[string]any) (string, bool) {
		return registry.Run(context.Background(), llm.ToolCall{Name: "git", Args: args(t, fields)})
	}
	run(map[string]any{"action": "add"})
	run(map[string]any{"action": "commit", "message": "first"})
	repo, _ := git.PlainOpen(dir)
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	return run, head.Hash().String()
}
