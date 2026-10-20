package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VW-01 | Happy    | @ and /view find Windows paths typed with either slash, in any case       | P0       |
// | VW-02 | Happy    | A pattern with \ or / names files by their Windows paths, shallow first   | P0       |

import (
	"slices"
	"testing"
)

func TestWindowsFilePaths(t *testing.T) {
	files := []string{`src\deep\Main.go`, `src\main.go`, `README.md`}

	t.Run("VW-01 @ and /view find Windows paths typed with either slash, in any case", func(t *testing.T) {
		for _, typed := range []string{"src/main", `src\main`, "SRC/MAIN"} {
			if found := fuzzyFiles(files, typed); len(found) == 0 || found[0] != `src\main.go` {
				t.Errorf("%q: %q", typed, found)
			}
		}
	})

	t.Run("VW-02 a pattern with \\ or / names files by their Windows paths, shallow first", func(t *testing.T) {
		slash := globFiles(files, "src/**/*.go")
		backslash := globFiles(files, `src\**\*.go`)

		want := []string{`src\main.go`, `src\deep\Main.go`}
		if !slices.Equal(slash, want) || !slices.Equal(backslash, want) {
			t.Errorf("slash %q backslash %q", slash, backslash)
		}
	})
}
