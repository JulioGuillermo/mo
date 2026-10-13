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

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/render"
)

func newEnv(t *testing.T) Env {
	t.Helper()
	return Env{WorkDir: t.TempDir()}
}

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

func TestRead(t *testing.T) {
	ctx := context.Background()

	t.Run("TL-04 read pages with offset and limit and says how to continue", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "a.txt", "l1\nl2\nl3\nl4\nl5\n")

		output, err := runRead(ctx, env, args(t, map[string]any{"path": "a.txt", "offset": 2, "limit": 2}))

		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(output, "l2\nl3\n") || !strings.Contains(output, "offset=4") {
			t.Errorf("output = %q", output)
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
		if strings.TrimSpace(output) != "main.go:2: func Hello() {}" {
			t.Errorf("output = %q", output)
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
	t.Run("TL-13 Registry drops disabled tools and truncates output", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "big.txt", strings.Repeat("x", 500))
		registry := NewRegistry(env, Options{MaxOutput: 100, Disabled: []string{"bash", "rm"}})

		for _, spec := range registry.Specs(nil) {
			if spec.Name == "bash" || spec.Name == "rm" {
				t.Errorf("%s should be disabled", spec.Name)
			}
		}
		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "read", Args: args(t, map[string]any{"path": "big.txt"})})
		if isError || len(output) > 150 {
			t.Errorf("isError = %v len = %d", isError, len(output))
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
		if err := syscall.Mkfifo(filepath.Join(env.WorkDir, "pipe"), 0o644); err != nil {
			t.Skip(err)
		}
		writeFile(t, env, "a.txt", "needle\n")

		_, err := runRead(context.Background(), env, args(t, map[string]any{"path": "pipe"}))
		if err == nil || !strings.Contains(err.Error(), "regular") {
			t.Errorf("read err = %v", err)
		}
		output, err := runGrep(context.Background(), env, args(t, map[string]any{"pattern": "needle"}))
		if err != nil || strings.TrimSpace(output) != "a.txt:1: needle" {
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
