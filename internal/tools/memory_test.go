package tools

// Test Traceability Table
// | ID    | Category | Description                                                       | Priority |
// | ME-01 | Happy    | write, read, list and delete a project memory                     | P0       |
// | ME-02 | Happy    | global and project scopes live in separate folders                | P1       |
// | ME-03 | Security | a name with a path or dots is refused                             | P0       |
// | ME-04 | Happy    | MemoryIndex lists scope/name: first line, stripping # from titles | P1       |
// | ME-05 | Security | write and edit on the memory folder ask, as a protected path      | P0       |
// | ME-06 | Edge     | projects in different folders do not share memories               | P1       |
// | ME-07 | Security | writing or deleting a global memory asks; project ones do not     | P0       |
// | ME-08 | Edge     | /x/a-b and /x/a/b get different project folders                   | P1       |
// | ME-09 | Happy    | list shows every memory; the prompt index stops at the limit      | P2       |
// | ME-10 | Edge     | a project folder with [ ] in its name still lists its memories    | P2       |

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func memoryEnv(t *testing.T) Env {
	t.Helper()
	return Env{WorkDir: t.TempDir(), MemoryDir: t.TempDir()}
}

func memory(t *testing.T, env Env, fields map[string]any) (string, error) {
	t.Helper()
	return runMemory(context.Background(), env, args(t, fields))
}

func TestMemory(t *testing.T) {
	t.Run("ME-01 write, read, list and delete a project memory", func(t *testing.T) {
		env := memoryEnv(t)

		if _, err := memory(t, env, map[string]any{"action": "write", "name": "build", "content": "Build with make\nthen run ./mo"}); err != nil {
			t.Fatal(err)
		}
		content, err := memory(t, env, map[string]any{"action": "read", "name": "build"})
		if err != nil || !strings.HasPrefix(content, "Build with make") {
			t.Errorf("read = %q err = %v", content, err)
		}
		list, _ := memory(t, env, map[string]any{"action": "list"})
		if strings.TrimSpace(list) != "project/build: Build with make" {
			t.Errorf("list = %q", list)
		}
		if _, err := memory(t, env, map[string]any{"action": "delete", "name": "build"}); err != nil {
			t.Fatal(err)
		}
		if list, _ := memory(t, env, map[string]any{"action": "list"}); list != "no memories" {
			t.Errorf("after delete list = %q", list)
		}
	})

	t.Run("ME-02 global and project scopes live in separate folders", func(t *testing.T) {
		env := memoryEnv(t)

		memory(t, env, map[string]any{"action": "write", "name": "style", "content": "tabs", "scope": "global"})
		memory(t, env, map[string]any{"action": "write", "name": "style", "content": "spaces"})

		global, _ := memory(t, env, map[string]any{"action": "read", "name": "style", "scope": "global"})
		project, _ := memory(t, env, map[string]any{"action": "read", "name": "style"})
		if global != "tabs" || project != "spaces" {
			t.Errorf("global = %q project = %q", global, project)
		}
	})

	t.Run("ME-03 a name with a path or dots is refused", func(t *testing.T) {
		env := memoryEnv(t)

		for _, name := range []string{"../escape", "a/b", "..", "UPPER", ""} {
			_, err := memory(t, env, map[string]any{"action": "write", "name": name, "content": "x"})
			if err == nil {
				t.Errorf("name %q was accepted", name)
			}
		}
	})

	t.Run("ME-04 MemoryIndex lists scope/name: first line, stripping # from titles", func(t *testing.T) {
		env := memoryEnv(t)
		memory(t, env, map[string]any{"action": "write", "name": "lab", "content": "# Lab network\nIPs..."})
		memory(t, env, map[string]any{"action": "write", "name": "me", "content": "Prefers Spanish", "scope": "global"})

		index := MemoryIndex(env)

		if index != "project/lab: Lab network\nglobal/me: Prefers Spanish" {
			t.Errorf("index = %q", index)
		}
	})

	t.Run("ME-05 write and edit on the memory folder ask, as a protected path", func(t *testing.T) {
		env := memoryEnv(t)
		env.WorkDir = filepath.Dir(env.MemoryDir)
		env.ProtectedPaths = []string{env.MemoryDir}
		registry := NewRegistry(env, Options{})

		call := llm.ToolCall{Name: "write", Args: args(t, map[string]any{"path": filepath.Join(env.MemoryDir, "global", "x.md")})}

		if warning := registry.Warning(call); warning != warningProtectedPath {
			t.Errorf("warning = %q", warning)
		}
	})

	t.Run("ME-06 projects in different folders do not share memories", func(t *testing.T) {
		first := memoryEnv(t)
		second := Env{WorkDir: t.TempDir(), MemoryDir: first.MemoryDir}
		memory(t, first, map[string]any{"action": "write", "name": "secret", "content": "first only"})

		_, err := memory(t, second, map[string]any{"action": "read", "name": "secret"})

		if !os.IsNotExist(err) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestMemoryGuards(t *testing.T) {
	t.Run("ME-07 writing or deleting a global memory asks; project ones do not", func(t *testing.T) {
		registry := NewRegistry(memoryEnv(t), Options{})
		cases := []struct {
			fields map[string]any
			asks   bool
		}{
			{map[string]any{"action": "write", "name": "x", "content": "y", "scope": "global"}, true},
			{map[string]any{"action": "delete", "name": "x", "scope": "global"}, true},
			{map[string]any{"action": "read", "name": "x", "scope": "global"}, false},
			{map[string]any{"action": "write", "name": "x", "content": "y"}, false},
		}
		for _, c := range cases {
			call := llm.ToolCall{Name: "memory", Args: args(t, c.fields)}
			if asks := registry.Warning(call) != ""; asks != c.asks {
				t.Errorf("%v: asks = %v", c.fields, asks)
			}
		}
	})

	t.Run("ME-08 /x/a-b and /x/a/b get different project folders", func(t *testing.T) {
		if projectSlug("/x/a-b") == projectSlug("/x/a/b") {
			t.Errorf("both are %q", projectSlug("/x/a-b"))
		}
		if !strings.HasPrefix(projectSlug("/home/jg/mo"), "mo-") {
			t.Errorf("slug = %q", projectSlug("/home/jg/mo"))
		}
	})

	t.Run("ME-09 list shows every memory; the prompt index stops at the limit", func(t *testing.T) {
		env := memoryEnv(t)
		for i := range memoryIndexLimit + 3 {
			memory(t, env, map[string]any{"action": "write", "name": fmt.Sprintf("note-%02d", i), "content": "n"})
		}

		list, _ := memory(t, env, map[string]any{"action": "list"})
		index := MemoryIndex(env)

		if strings.Count(list, "\n")+1 != memoryIndexLimit+3 {
			t.Errorf("list has %d lines", strings.Count(list, "\n")+1)
		}
		if !strings.HasSuffix(index, "(3 more; use memory list)") {
			t.Errorf("index ends with %q", index[len(index)-40:])
		}
	})
}

func TestMemoryOddFolderNames(t *testing.T) {
	t.Run("ME-10 a project folder with [ ] in its name still lists its memories", func(t *testing.T) {
		env := Env{WorkDir: filepath.Join(t.TempDir(), "proj[1]"), MemoryDir: t.TempDir()}
		memory(t, env, map[string]any{"action": "write", "name": "note", "content": "kept"})

		if index := MemoryIndex(env); index != "project/note: kept" {
			t.Errorf("index = %q", index)
		}
	})
}
