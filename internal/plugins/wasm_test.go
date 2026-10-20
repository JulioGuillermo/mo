package plugins

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | WA-01 | Happy    | mo.wasm loads a module from the lib folder; call runs an export with numbers, i32 and f64 | P0 |
// | WA-02 | Happy    | exports lists each export with its signature                               | P2       |
// | WA-03 | Error    | A wrong count of numbers, an unknown export, a missing file or bad bytes fail with a reason | P1 |
// | WA-04 | Security | A module that never ends stops when its plugin call's time runs out, and the plugin keeps working | P0 |
// | WA-05 | Happy    | run runs a WASI program with input on stdin, args and env, giving stdout, stderr and the exit code | P0 |

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mathModule exports add(i32, i32) -> i32, mul(f64, f64) -> f64 and spin(),
// which loops forever.
var mathModule = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x10, 0x03, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, 0x60, 0x02, 0x7c, 0x7c, 0x01, 0x7c, 0x60, 0x00, 0x00,
	0x03, 0x04, 0x03, 0x00, 0x01, 0x02,
	0x07, 0x14, 0x03,
	0x03, 'a', 'd', 'd', 0x00, 0x00,
	0x03, 'm', 'u', 'l', 0x00, 0x01,
	0x04, 's', 'p', 'i', 'n', 0x00, 0x02,
	0x0a, 0x19, 0x03,
	0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
	0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0xa2, 0x0b,
	0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b,
}

// wasmTree is a config folder with the plugins and the modules in lib.
func wasmTree(t *testing.T, plugin string, modules map[string][]byte) *Set {
	t.Helper()
	root := t.TempDir()
	for name, code := range modules {
		path := filepath.Join(root, "lib", name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, code, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, "plugins"), 0o755)
	if err := os.WriteFile(filepath.Join(root, "plugins", "calc.lua"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	set := Load(filepath.Join(root, "plugins"), t.TempDir(), t.TempDir(), nil)
	t.Cleanup(set.Close)
	if err := set.Status()[0].Err; err != nil {
		t.Fatal(err)
	}
	return set
}

func TestWasm(t *testing.T) {
	t.Run("WA-01 mo.wasm loads a module from the lib folder; call runs an export with numbers, i32 and f64", func(t *testing.T) {
		set := wasmTree(t, `
local math = mo.wasm("math.wasm")
mo.command{ name = "go", run = function()
  return math.call("add", 40, 2) .. " " .. math.call("mul", 1.5, 3) .. " " .. math.call("add", 2147483647, 1)
end }`, map[string][]byte{"math.wasm": mathModule})

		output, err := runCommand(t, set, "go")

		if err != nil || output != "42 4.5 -2147483648" {
			t.Errorf("output = %q err = %v", output, err)
		}
	})

	t.Run("WA-02 exports lists each export with its signature", func(t *testing.T) {
		set := wasmTree(t, `
mo.command{ name = "go", run = function()
  local exports = mo.wasm("math.wasm").exports
  return exports.add .. "|" .. exports.mul .. "|" .. exports.spin
end }`, map[string][]byte{"math.wasm": mathModule})

		output, err := runCommand(t, set, "go")

		if err != nil || output != "(i32, i32) -> (i32)|(f64, f64) -> (f64)|() -> ()" {
			t.Errorf("output = %q err = %v", output, err)
		}
	})

	t.Run("WA-03 a wrong count of numbers, an unknown export, a missing file or bad bytes fail with a reason", func(t *testing.T) {
		set := wasmTree(t, `
mo.command{ name = "count", run = function() return mo.wasm("math.wasm").call("add", 1) end }
mo.command{ name = "unknown", run = function() return mo.wasm("math.wasm").call("sub", 1, 2) end }
mo.command{ name = "missing", run = function() return mo.wasm("none.wasm") end }
mo.command{ name = "bad", run = function() return mo.wasm("bad.wasm") end }`,
			map[string][]byte{"math.wasm": mathModule, "bad.wasm": []byte("not wasm")})

		for name, want := range map[string]string{
			"count":   "add takes 2 numbers: (i32, i32) -> (i32)",
			"unknown": "no exported function sub",
			"missing": "none.wasm",
			"bad":     "mo.wasm bad.wasm",
		} {
			_, err := runCommand(t, set, name)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: err = %v, want %q", name, err, want)
			}
		}
	})

	t.Run("WA-04 a module that never ends stops when its plugin call's time runs out, and the plugin keeps working", func(t *testing.T) {
		set := wasmTree(t, `
local math = mo.wasm("math.wasm")
mo.on("prompt", function(prompt) math.call("spin") return "never" end)
mo.command{ name = "go", run = function() return tostring(math.call("add", 1, 1)) end }`, map[string][]byte{"math.wasm": mathModule})
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		started := time.Now()
		prompt := set.RewritePrompt(ctx, "hello")
		waited := time.Since(started)
		output, err := runCommand(t, set, "go")

		if prompt != "hello" || waited > 2*time.Second || err != nil || output != "2" {
			t.Errorf("prompt = %q after %v; then %q err = %v", prompt, waited, output, err)
		}
	})

	t.Run("WA-05 run runs a WASI program with input on stdin, args and env, giving stdout, stderr and the exit code", func(t *testing.T) {
		program := buildWasiProgram(t, `package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	input, _ := io.ReadAll(os.Stdin)
	fmt.Print(strings.ToUpper(string(input)), " ", strings.Join(os.Args[1:], ","), " ", os.Getenv("MODE"))
	fmt.Fprint(os.Stderr, "done")
	os.Exit(3)
}
`)
		set := wasmTree(t, `
local upper = mo.wasm("upper.wasm")
mo.command{ name = "go", run = function()
  local out, err, code = upper.run("hola mundo", { args = { "a", "b" }, env = { MODE = "fast" } })
  local again = upper.run("otra vez")
  return out .. "|" .. err .. "|" .. code .. "|" .. again
end }`, map[string][]byte{"upper.wasm": program})

		output, err := runCommand(t, set, "go")

		if err != nil || output != "HOLA MUNDO a,b fast|done|3|OTRA VEZ  " {
			t.Errorf("output = %q err = %v", output, err)
		}
	})
}

// buildWasiProgram builds a Go program for WASI, as a plugin's author would
// build theirs.
func buildWasiProgram(t *testing.T, source string) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a WASI program")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module upper\n\ngo 1.22\n"), 0o644)
	out := filepath.Join(dir, "upper.wasm")
	build := exec.Command("go", "build", "-o", out, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	code, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return code
}
