package tools

// Test Traceability Table
// | ID    | Category | Description                                                   | Priority |
// | TL-01 | Happy    | edit replaces a unique match and keeps the file mode          | P0       |
// | TL-02 | Error    | edit refuses an ambiguous match unless all is set             | P0       |
// | TL-03 | Error    | edit reports text that is not there                           | P1       |
// | TL-04 | Happy    | read pages with offset and limit and says how to continue     | P0       |
// | TL-05 | Error    | read refuses a binary file                                    | P1       |
// | TL-06 | Happy    | write creates missing parent directories                      | P1       |
// | TL-07 | Security | rm refuses the working directory, home and /                  | P0       |
// | TL-08 | Happy    | rm deletes a directory only with recursive                    | P1       |
// | TL-09 | Happy    | grep finds matches, honours glob and skips hidden and vendor  | P0       |
// | TL-10 | Happy    | bash returns output and reports a non-zero exit as an error   | P0       |
// | TL-11 | Error    | bash stops a command at its timeout                           | P1       |
// | TL-12 | Edge     | Truncate keeps head and tail without splitting a character    | P1       |
// | TL-13 | Happy    | Registry drops disabled tools and truncates output            | P1       |
// | TL-14 | Error    | Registry reports an unknown tool as an error result           | P2       |
// | TL-15 | Security | rm refuses .., top-level directories and ancestors of home    | P0       |
// | TL-16 | Security | Warning flags only changing tools outside the workdir         | P0       |
// | TL-17 | Happy    | bash with a background process returns as a success           | P1       |
// | TL-18 | Edge     | bash keeps head and tail of huge output within its limit      | P1       |
// | TL-19 | Edge     | read refuses and grep skips a FIFO instead of hanging         | P1       |
// | TL-20 | Edge     | bash has no terminal, so a tty prompt fails fast              | P1       |
// | TL-21 | Security | An argument name in another case is refused before running    | P0       |
// | TL-22 | Security | A symlink inside the workdir that points outside is flagged   | P0       |
// | TL-23 | Security | mo config, .git and hidden home entries are flagged           | P0       |
// | TL-24 | Security | rm through a symlinked directory still protects the workdir   | P0       |
// | TL-25 | Happy    | read of an image answers its format, size and a marker        | P1       |
// | TL-26 | Happy    | The title tool hands the title over, and only exists where it can | P1       |
// | TL-27 | Happy    | edit changes several files in one call, each edit in its path or the call's | P0 |
// | TL-28 | Happy    | edit fails whole across files: one edit that does not apply changes none | P0   |
// | TL-29 | Security | Warning checks every file an edit changes, not only the call's path | P0       |
// | TL-30 | Error    | An edit with no path, for the call or for itself, says so        | P1       |

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/peers"
	"github.com/jg/mo/internal/render"
)

func newEnv(t *testing.T) Env {
	t.Helper()
	return Env{WorkDir: t.TempDir(), Titles: NewTitles(), UIs: NewUIs(""), Agents: idleAgents{}, Parent: deafParent{}, Peers: peers.NewNode(t.TempDir())}
}

// deafParent takes notes and does nothing with them.
type deafParent struct{}

func (deafParent) Tell(string) (string, error) { return "told", nil }

// idleAgents are subagents that never start, so the agent tool is there.
type idleAgents struct{}

func (idleAgents) Run(context.Context, []SubTask, bool) (string, error)    { return "", nil }
func (idleAgents) Send(context.Context, int, string, bool) (string, error) { return "", nil }
func (idleAgents) Wait(context.Context, []int) (string, error)             { return "", nil }
func (idleAgents) Log(int) (string, error)                                 { return "", nil }
func (idleAgents) Status() string                                          { return "" }
func (idleAgents) Stop(int) (string, error)                                { return "", nil }
func (idleAgents) TakeEnded() []string                                     { return nil }
func (idleAgents) HasEnded() bool                                          { return false }

func writeFile(t *testing.T, env Env, name, content string) string {
	t.Helper()
	path := filepath.Join(env.WorkDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func args(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEdit(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-01 edit replaces a unique match and keeps the file mode", func(t *testing.T) {
		env := newEnv(t)
		path := writeFile(t, env, "a.txt", "one two three")

		_, err := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "old": "two", "new": "2"}))

		data, _ := os.ReadFile(path)
		info, _ := os.Stat(path)
		if err != nil || string(data) != "one 2 three" || info.Mode().Perm() != 0o600 {
			t.Errorf("content = %q mode = %v err = %v", data, info.Mode().Perm(), err)
		}
	})

	t.Run("TL-02 edit refuses an ambiguous match unless all is set", func(t *testing.T) {
		env := newEnv(t)
		path := writeFile(t, env, "a.txt", "x x")

		_, err := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "old": "x", "new": "y"}))
		if err == nil || !strings.Contains(err.Error(), "2 times") {
			t.Fatalf("err = %v", err)
		}
		_, err = runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "old": "x", "new": "y", "all": true}))
		data, _ := os.ReadFile(path)
		if err != nil || string(data) != "y y" {
			t.Errorf("content = %q err = %v", data, err)
		}
	})

	t.Run("TL-03 edit reports text that is not there", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.txt", "abc")

		_, err := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "old": "zzz", "new": "y"}))

		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("err = %v", err)
		}
	})
}

// call runs a tool through a registry, as the model calls it.
func call(t *testing.T, env Env, name string, fields map[string]any) (string, bool) {
	t.Helper()
	return NewRegistry(env, Options{MaxOutput: 10000}).Run(context.Background(), llm.ToolCall{Name: name, Args: args(t, fields)})
}

func TestRead(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-04 read pages with offset and limit and says how to continue", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.txt", "l1\nl2\nl3\nl4\nl5\n")

		output, isError := call(t, env, "read", map[string]any{"path": "a.txt", "offset": 1, "limit": 2})
		last, _ := call(t, env, "read", map[string]any{"path": "a.txt", "offset": -2})

		if isError || output != "l2\nl3\n[lines 2-3 of 5; offset=3 continues]\n" {
			t.Errorf("output = %q", output)
		}
		if last != "l4\nl5\n[lines 4-5 of 5]\n" {
			t.Errorf("last = %q", last)
		}
	})

	t.Run("read numbers the lines only when asked, and match keeps the matching ones numbered", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.txt", "l1\nl2\nl3\n")

		plain, _ := call(t, env, "read", map[string]any{"path": "a.txt"})
		numbered, _ := call(t, env, "read", map[string]any{"path": "a.txt", "offset": 1, "numbered": true})
		matched, _ := call(t, env, "read", map[string]any{"path": "a.txt", "match": "l[13]"})

		if plain != "l1\nl2\nl3\n" || numbered != "2\tl2\n3\tl3\n[lines 2-3 of 3]\n" || matched != "1\tl1\n3\tl3\n" {
			t.Errorf("plain = %q numbered = %q matched = %q", plain, numbered, matched)
		}
	})

	t.Run("read cuts a long line, says where it goes on, and reads on from col", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "min.js", strings.Repeat("a", 2500)+"\nshort\n")

		cut, _ := call(t, env, "read", map[string]any{"path": "min.js"})
		rest, _ := call(t, env, "read", map[string]any{"path": "min.js", "limit": 1, "col": 2490, "width": 5})

		if !strings.HasSuffix(cut, " [500 bytes cut; col=2000 continues]\nshort\n") {
			t.Errorf("cut = %q", cut[len(cut)-60:])
		}
		if rest != "aaaaa [5 bytes cut; col=2495 continues]\n[lines 1-1 of 2; offset=1 continues]\n" {
			t.Errorf("rest = %q", rest)
		}
	})

	t.Run("TL-05 read refuses a binary file", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.bin", "ab\x00cd")

		_, err := runRead(ctx, env, args(t, map[string]any{"path": "a.bin"}))

		if err == nil || !strings.Contains(err.Error(), "binary") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestWrite(t *testing.T) {
	t.Run("TL-06 write creates missing parent directories", func(t *testing.T) {
		env := newEnv(t)

		_, err := runWrite(context.Background(), env, args(t, map[string]any{"path": "deep/er/a.txt", "content": "hi"}))

		data, _ := os.ReadFile(filepath.Join(env.WorkDir, "deep/er/a.txt"))
		if err != nil || string(data) != "hi" {
			t.Errorf("content = %q err = %v", data, err)
		}
	})
}

func TestRm(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-07 rm refuses the working directory, home and /", func(t *testing.T) {
		env := newEnv(t)
		home, _ := os.UserHomeDir()
		for _, path := range []string{".", env.WorkDir, "/", home} {
			_, err := runRm(ctx, env, args(t, map[string]any{"path": path, "recursive": true}))
			if err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Errorf("rm %q: err = %v", path, err)
			}
		}
	})

	t.Run("TL-15 rm refuses .., top-level directories and ancestors of home", func(t *testing.T) {
		env := Env{WorkDir: filepath.Join(t.TempDir(), "project")}
		home, _ := os.UserHomeDir()
		for _, path := range []string{"..", "/usr", "/home", filepath.Dir(home)} {
			_, err := runRm(ctx, env, args(t, map[string]any{"path": path, "recursive": true}))
			if err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Errorf("rm %q: err = %v", path, err)
			}
		}
	})

	t.Run("TL-08 rm deletes a directory only with recursive", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "dir/a.txt", "x")

		_, err := runRm(ctx, env, args(t, map[string]any{"path": "dir"}))
		if err == nil {
			t.Fatal("non-recursive rm of a full directory should fail")
		}
		_, err = runRm(ctx, env, args(t, map[string]any{"path": "dir", "recursive": true}))
		if _, statErr := os.Stat(filepath.Join(env.WorkDir, "dir")); err != nil || !os.IsNotExist(statErr) {
			t.Errorf("err = %v, dir still there: %v", err, statErr)
		}
	})
}

func TestGrep(t *testing.T) {
	t.Run("TL-09 grep finds matches, honours glob and skips hidden and vendor", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "main.go", "package main\nfunc Hello() {}\n")
		writeFile(t, env, "notes.md", "Hello there\n")
		writeFile(t, env, ".git/config", "Hello hidden\n")
		writeFile(t, env, "vendor/lib.go", "func Hello() {}\n")

		output, err := runGrep(context.Background(), env, args(t, map[string]any{"pattern": "hello", "ignore_case": true, "glob": "*.go"}))

		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(output) != "main.go\n2: func Hello() {}" {
			t.Errorf("output = %q", output)
		}
	})
}

func TestGrepOutputs(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)
	writeFile(t, env, "a.go", "one\n\tneedle\ntwo\nthree\nfour\nneedle\n")
	writeFile(t, env, "b.go", "needle\n")

	t.Run("grep gives only the files, or a count per file", func(t *testing.T) {
		files, _ := runGrep(ctx, env, args(t, map[string]any{"pattern": "needle", "show": "files"}))
		counts, _ := runGrep(ctx, env, args(t, map[string]any{"pattern": "needle", "show": "count"}))

		if files != "a.go\nb.go\n" || counts != "a.go: 2\nb.go: 1\n" {
			t.Errorf("files = %q counts = %q", files, counts)
		}
	})

	t.Run("grep shows context lines, keeping their indent, apart by --", func(t *testing.T) {
		output, _ := runGrep(ctx, env, args(t, map[string]any{"pattern": "needle", "context": 1, "glob": "a.go"}))

		if output != "a.go\n1- one\n2: \tneedle\n3- two\n--\n5- four\n6: needle\n" {
			t.Errorf("output = %q", output)
		}
	})
}

func TestEditMany(t *testing.T) {
	ctx := context.Background()

	t.Run("edit says the lines it changed, as the file is after, earlier ones moved by later ones", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")

		grew, _ := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "edits": []map[string]any{
			{"old": "five", "new": "FIVE"}, {"old": "two", "new": "2a\n2b\n2c"}}}))
		gone, _ := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "old": "three\n", "new": ""}))
		joined, _ := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "old": "2", "new": "two-", "all": true}))
		data, _ := os.ReadFile(filepath.Join(env.WorkDir, "a.txt"))

		if grew != "made 2 edits, 2 replacement(s), at lines 2-4 and 7" {
			t.Errorf("grew = %q", grew)
		}
		if gone != "replaced 1 occurrence(s), at line 5" || joined != "replaced 3 occurrence(s), at lines 2-4" {
			t.Errorf("gone = %q joined = %q data = %q", gone, joined, data)
		}
	})

	t.Run("edit makes several replacements, or none when one fails", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.txt", "alpha beta gamma\n")

		done, err := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "edits": []map[string]any{{"old": "alpha", "new": "A"}, {"old": "gamma", "new": "G"}}}))
		_, failed := runEdit(ctx, env, args(t, map[string]any{"path": "a.txt", "edits": []map[string]any{{"old": "beta", "new": "B"}, {"old": "missing", "new": "x"}}}))
		data, _ := os.ReadFile(filepath.Join(env.WorkDir, "a.txt"))

		if err != nil || done != "made 2 edits, 2 replacement(s), at line 1" || string(data) != "A beta G\n" {
			t.Errorf("done = %q err = %v data = %q", done, err, data)
		}
		if failed == nil || !strings.Contains(failed.Error(), "edit 2: old text not found") {
			t.Errorf("failed = %v", failed)
		}
	})
}

func TestBash(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-10 bash returns output and reports a non-zero exit as an error", func(t *testing.T) {
		env := newEnv(t)

		output, err := runBash(ctx, env, args(t, map[string]any{"command": "pwd"}))
		if err != nil || strings.TrimSpace(output) != env.WorkDir {
			t.Errorf("output = %q err = %v", output, err)
		}
		output, err = runBash(ctx, env, args(t, map[string]any{"command": "echo oops; exit 3"}))
		if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(output, "oops") {
			t.Errorf("output = %q err = %v", output, err)
		}
	})

	t.Run("TL-11 bash stops a command at its timeout", func(t *testing.T) {
		_, err := runBash(ctx, newEnv(t), args(t, map[string]any{"command": "sleep 5", "timeout": 1}))

		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestTruncate(t *testing.T) {
	t.Run("TL-12 Truncate keeps head and tail without splitting a character", func(t *testing.T) {
		text := strings.Repeat("ñ", 100)

		cut := Truncate(text, 51)

		if !utf8.ValidString(cut) || !strings.Contains(cut, "bytes cut") {
			t.Errorf("cut = %q", cut)
		}
		if !strings.HasPrefix(cut, "ñ") || !strings.HasSuffix(cut, "ñ") {
			t.Errorf("head or tail lost: %q", cut)
		}
	})
}

func TestRegistry(t *testing.T) {
	t.Run("TL-13 Registry drops disabled tools and truncates output, but what read was asked for", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "big.txt", strings.Repeat("x", 500))
		loud := Tool{Name: "loud", Schema: json.RawMessage(`{"type":"object","properties":{}}`), Run: func(context.Context, Env, json.RawMessage) (string, error) {
			return strings.Repeat("y", 500), nil
		}}
		registry := NewRegistry(env, Options{MaxOutput: 100, Disabled: []string{"bash", "rm"}, Extra: []Tool{loud}})

		for _, spec := range registry.Specs(nil) {
			if spec.Name == "bash" || spec.Name == "rm" {
				t.Errorf("%s should be disabled", spec.Name)
			}
		}
		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "loud", Args: json.RawMessage(`{}`)})
		if isError || len(output) > 150 {
			t.Errorf("isError = %v len = %d", isError, len(output))
		}
		whole, _ := registry.Run(context.Background(), llm.ToolCall{Name: "read", Args: args(t, map[string]any{"path": "big.txt"})})
		if len(whole) != 501 {
			t.Errorf("read was cut to %d bytes", len(whole))
		}
	})

	t.Run("TL-14 Registry reports an unknown tool as an error result", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Disabled: []string{"bash"}})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: json.RawMessage(`{}`)})

		if !isError || !strings.Contains(output, "unknown tool") {
			t.Errorf("output = %q isError = %v", output, isError)
		}
	})
}

func TestWarning(t *testing.T) {
	t.Run("TL-16 Warning flags only changing tools outside the workdir", func(t *testing.T) {
		env := Env{WorkDir: "/work/project"}
		registry := NewRegistry(env, Options{})
		cases := []struct {
			tool, path string
			outside    bool
		}{
			{"write", "src/a.go", false},
			{"write", "/work/project/a.go", false},
			{"write", "../other/a.go", true},
			{"edit", "/etc/hosts", true},
			{"rm", "~/notes", true},
			{"mkdir", "/work/project-evil/x", true},
			{"read", "/etc/passwd", false},
		}
		for _, c := range cases {
			call := llm.ToolCall{Name: c.tool, Args: args(t, map[string]any{"path": c.path})}
			if got := registry.Warning(call) == warningOutsideWorkDir; got != c.outside {
				t.Errorf("%s %s: outside = %v", c.tool, c.path, got)
			}
		}
	})
}

func TestBashProcesses(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-17 bash with a background process returns as a success", func(t *testing.T) {
		output, err := runBash(ctx, newEnv(t), args(t, map[string]any{"command": "echo started; sleep 3 &"}))

		if err != nil || !strings.Contains(output, "started") {
			t.Errorf("output = %q err = %v", output, err)
		}
	})

	t.Run("TL-18 bash keeps head and tail of huge output within its limit", func(t *testing.T) {
		output, err := runBash(ctx, newEnv(t), args(t, map[string]any{"command": "echo first; head -c 5000000 /dev/zero | tr '\\0' x; echo; echo last"}))

		if err != nil {
			t.Fatal(err)
		}
		if len(output) > bashKeptOutput+100 || !strings.HasPrefix(output, "first") || !strings.HasSuffix(strings.TrimSpace(output), "last") {
			t.Errorf("len = %d head = %q", len(output), output[:20])
		}
	})

	t.Run("TL-20 bash has no terminal, so a tty prompt fails fast", func(t *testing.T) {
		output, err := runBash(ctx, newEnv(t), args(t, map[string]any{"command": "cat /dev/tty", "timeout": 5}))

		if err == nil || strings.Contains(err.Error(), "timed out") {
			t.Errorf("output = %q err = %v", output, err)
		}
	})
}

func TestSpecialFiles(t *testing.T) {
	t.Run("TL-19 read refuses and grep skips a FIFO instead of hanging", func(t *testing.T) {
		env := newEnv(t)
		if err := makeFIFO(filepath.Join(env.WorkDir, "pipe")); err != nil {
			t.Skip(err)
		}
		writeFile(t, env, "a.txt", "needle\n")

		_, err := runRead(context.Background(), env, args(t, map[string]any{"path": "pipe"}))
		if err == nil || !strings.Contains(err.Error(), "regular") {
			t.Errorf("read err = %v", err)
		}
		output, err := runGrep(context.Background(), env, args(t, map[string]any{"pattern": "needle"}))
		if err != nil || strings.TrimSpace(output) != "a.txt\n1: needle" {
			t.Errorf("grep output = %q err = %v", output, err)
		}
	})
}

func TestGuards(t *testing.T) {
	t.Run("TL-21 an argument name in another case is refused before running", func(t *testing.T) {
		env := newEnv(t)
		target := filepath.Join(t.TempDir(), "victim.txt")
		registry := NewRegistry(env, Options{})
		call := llm.ToolCall{Name: "write", Args: args(t, map[string]any{"path": "notes.txt", "PATH": target, "content": "x"})}

		output, isError := registry.Run(context.Background(), call)

		if !isError || !strings.Contains(output, "unknown argument") {
			t.Errorf("output = %q isError = %v", output, isError)
		}
		if _, err := os.Stat(target); err == nil {
			t.Error("the file was written")
		}
	})

	t.Run("TL-22 a symlink inside the workdir that points outside is flagged", func(t *testing.T) {
		env := newEnv(t)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(env.WorkDir, "r")); err != nil {
			t.Fatal(err)
		}
		registry := NewRegistry(env, Options{})

		for _, tool := range []string{"write", "edit", "mkdir", "rm"} {
			call := llm.ToolCall{Name: tool, Args: args(t, map[string]any{"path": "r/new/file"})}
			if warning := registry.Warning(call); warning != warningOutsideWorkDir {
				t.Errorf("%s: warning = %q", tool, warning)
			}
		}
		removeLink := llm.ToolCall{Name: "rm", Args: args(t, map[string]any{"path": "r"})}
		if warning := registry.Warning(removeLink); warning != "" {
			t.Errorf("removing the link itself: warning = %q", warning)
		}
	})

	t.Run("TL-23 mo config, .git and hidden home entries are flagged", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip(err)
		}
		env := Env{WorkDir: home, ProtectedPaths: []string{filepath.Join(home, ".config", "mo"), filepath.Join(home, ".mo.toml")}}
		registry := NewRegistry(env, Options{})

		for _, path := range []string{"~/.config/mo/config.toml", "~/.mo.toml", "~/.bashrc", "~/.ssh/authorized_keys", "~/proj/.git/hooks/pre-commit"} {
			call := llm.ToolCall{Name: "write", Args: args(t, map[string]any{"path": path})}
			if warning := registry.Warning(call); warning != warningProtectedPath {
				t.Errorf("%s: warning = %q", path, warning)
			}
		}
		plain := llm.ToolCall{Name: "write", Args: args(t, map[string]any{"path": "~/proj/main.go"})}
		if warning := registry.Warning(plain); warning != "" {
			t.Errorf("plain file: warning = %q", warning)
		}
	})

	t.Run("TL-24 rm through a symlinked directory still protects the workdir", func(t *testing.T) {
		parent := t.TempDir()
		env := Env{WorkDir: filepath.Join(parent, "project")}
		if err := os.MkdirAll(env.WorkDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(parent, filepath.Join(env.WorkDir, "up")); err != nil {
			t.Fatal(err)
		}

		_, err := runRm(context.Background(), env, args(t, map[string]any{"path": "up/project", "recursive": true}))

		if err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestReadImage(t *testing.T) {
	t.Run("TL-25 read of an image answers its format, size and a marker", func(t *testing.T) {
		env := newEnv(t)
		img := image.NewRGBA(image.Rect(0, 0, 5, 3))
		var encoded bytes.Buffer
		png.Encode(&encoded, img)
		writeFile(t, env, "pic.png", encoded.String())

		output, err := runRead(context.Background(), env, args(t, map[string]any{"path": "pic.png"}))

		if err != nil || !strings.HasPrefix(output, "[image png 5x3: /") || len(render.MarkedImages(output)) != 1 {
			t.Errorf("output = %q err = %v", output, err)
		}
	})
}

func TestTitleTool(t *testing.T) {
	t.Run("TL-26 the title tool hands the title over, and only exists where it can", func(t *testing.T) {
		env := newEnv(t)
		var got string
		env.Titles.OnSet(func(title string) { got = title })
		registry := NewRegistry(env, Options{})
		bare := NewRegistry(Env{WorkDir: env.WorkDir}, Options{})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "title", Args: args(t, map[string]any{"title": "  Arreglar\n el login "})})
		_, missing := bare.find("title")

		if isError || got != "Arreglar el login" || !strings.Contains(output, "Arreglar el login") || missing {
			t.Errorf("output %q got %q in bare registry %v", output, got, missing)
		}
	})
}

func TestHelpAndView(t *testing.T) {
	t.Run("help lists every action in a line, explains one, and names them for an unknown one", func(t *testing.T) {
		env := newEnv(t)

		all, _ := call(t, env, "git", map[string]any{"action": "help"})
		pull, _ := call(t, env, "git", map[string]any{"action": "help", "topic": "pull"})
		unknown, isError := call(t, env, "memory", map[string]any{"action": "help", "topic": "fly"})

		if !strings.Contains(all, "\npull: remote, name") || !strings.Contains(all, "\ntopic view: offset") || !strings.Contains(all, "\ntopic conflicts: ") {
			t.Errorf("all = %q", all)
		}
		if !strings.HasPrefix(pull, "remote, name: the branch to take") {
			t.Errorf("pull = %q", pull)
		}
		if !isError || !strings.Contains(unknown, "list, read, write, delete, search, view") {
			t.Errorf("unknown = %q", unknown)
		}
	})

	t.Run("the view fields go after a tool's own, also when it has none", func(t *testing.T) {
		some := withViewSchema(json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","enum":["{x}"]}},"required":["path"]}`))
		none := withViewSchema(json.RawMessage(`{"type":"object","properties":{}}`))

		var parsed map[string]any
		if json.Unmarshal(some, &parsed) != nil || json.Unmarshal(none, &parsed) != nil {
			t.Fatalf("some = %s none = %s", some, none)
		}
		if !strings.HasPrefix(string(some), `{"type":"object","properties":{"path"`) || !strings.Contains(string(none), `"numbered"`) {
			t.Errorf("some = %s none = %s", some, none)
		}
	})
}

func TestEditFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-27 edit changes several files in one call, each edit in its path or the call's", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.go", "package a\n\nfunc Old() {}\n")
		writeFile(t, env, "b.go", "package b\n\nvar x = a.Old()\n")

		output, err := runEdit(ctx, env, args(t, map[string]any{"path": "a.go", "edits": []map[string]any{
			{"old": "Old", "new": "New"}, {"path": "b.go", "old": "a.Old", "new": "a.New"}}}))

		a, _ := os.ReadFile(filepath.Join(env.WorkDir, "a.go"))
		b, _ := os.ReadFile(filepath.Join(env.WorkDir, "b.go"))
		if err != nil || !strings.Contains(string(a), "func New()") || !strings.Contains(string(b), "a.New()") {
			t.Fatalf("err %v a %q b %q", err, a, b)
		}
		if output != "edited 2 files\na.go: replaced 1 occurrence(s), at line 3\nb.go: replaced 1 occurrence(s), at line 3" {
			t.Errorf("output %q", output)
		}
		if summary := editTool().Summary(args(t, map[string]any{"edits": []map[string]any{{"path": "a.go"}, {"path": "b.go"}, {"path": "a.go"}}})); summary != "a.go, b.go" {
			t.Errorf("summary %q", summary)
		}
	})

	t.Run("TL-28 edit fails whole across files: one edit that does not apply changes none", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.go", "alpha\n")
		writeFile(t, env, "b.go", "beta\n")

		_, err := runEdit(ctx, env, args(t, map[string]any{"edits": []map[string]any{
			{"path": "a.go", "old": "alpha", "new": "A"}, {"path": "b.go", "old": "missing", "new": "x"}}}))

		a, _ := os.ReadFile(filepath.Join(env.WorkDir, "a.go"))
		if err == nil || err.Error() != "edit 2: old text not found in b.go" || string(a) != "alpha\n" {
			t.Errorf("err %v a %q", err, a)
		}
	})

	t.Run("TL-29 Warning checks every file an edit changes, not only the call's path", func(t *testing.T) {
		registry := NewRegistry(Env{WorkDir: "/work/project"}, Options{})
		call := func(fields map[string]any) llm.ToolCall {
			return llm.ToolCall{Name: "edit", Args: args(t, fields)}
		}

		inside := registry.Warning(call(map[string]any{"path": "a.go", "edits": []map[string]any{{"old": "x", "new": "y"}, {"path": "b.go", "old": "x", "new": "y"}}}))
		outside := registry.Warning(call(map[string]any{"path": "a.go", "edits": []map[string]any{{"old": "x", "new": "y"}, {"path": "/etc/hosts", "old": "x", "new": "y"}}}))
		protected := registry.Warning(call(map[string]any{"edits": []map[string]any{{"path": ".git/config", "old": "x", "new": "y"}}}))

		if inside != "" || outside != warningOutsideWorkDir || protected != warningProtectedPath {
			t.Errorf("inside %q outside %q protected %q", inside, outside, protected)
		}
	})

	t.Run("TL-30 an edit with no path, for the call or for itself, says so", func(t *testing.T) {
		env := newEnv(t)

		_, err := runEdit(ctx, env, args(t, map[string]any{"edits": []map[string]any{{"old": "x", "new": "y"}}}))

		if err == nil || !strings.Contains(err.Error(), "give path") {
			t.Errorf("err %v", err)
		}
	})
}
