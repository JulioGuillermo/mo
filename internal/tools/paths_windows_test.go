package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | WP-01 | Happy    | Git bash's /c/Users and Cygwin's /cygdrive/c become C:\Users             | P0       |
// | WP-02 | Happy    | Windows paths, with either slash, and relative ones stay as they are     | P0       |
// | WP-03 | Happy    | Resolve takes ~\ and /c/ paths, and both slashes                         | P0       |
// | WP-04 | Security | A path outside the working folder written with \ is outside               | P0       |

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsPaths(t *testing.T) {
	t.Run("WP-01 Git bash's /c/Users and Cygwin's /cygdrive/c become C:\\Users", func(t *testing.T) {
		for path, want := range map[string]string{
			"/c/Users/me/a.txt":          `C:\Users\me\a.txt`,
			"/D/work":                    `D:\work`,
			"/c":                         `C:\`,
			"/cygdrive/c/Users/me/a.txt": `C:\Users\me\a.txt`,
		} {
			if got := systemPath(path); got != want {
				t.Errorf("%s: %q want %q", path, got, want)
			}
		}
	})

	t.Run("WP-02 Windows paths, with either slash, and relative ones stay as they are", func(t *testing.T) {
		for _, path := range []string{`C:\Users\me`, `C:/Users/me`, `src\main.go`, `src/main.go`, `/usr/bin`, `/ca/x`} {
			if got := systemPath(path); got != path {
				t.Errorf("%s became %q", path, got)
			}
		}
	})

	t.Run("WP-03 Resolve takes ~\\ and /c/ paths, and both slashes", func(t *testing.T) {
		home, _ := os.UserHomeDir()
		env := Env{WorkDir: `C:\work`}

		if got := env.Resolve(`~\notes.txt`); got != filepath.Join(home, "notes.txt") {
			t.Errorf("~\\: %q", got)
		}
		if got := env.Resolve("/c/work/src/a.go"); got != `C:\work\src\a.go` {
			t.Errorf("/c/: %q", got)
		}
		if got := env.Resolve("src/a.go"); got != `C:\work\src\a.go` {
			t.Errorf("relative: %q", got)
		}
	})

	t.Run("WP-04 a path outside the working folder written with \\ is outside", func(t *testing.T) {
		if isInside(`C:\other\x.txt`, `C:\work`) || isInside(`D:\work\x`, `C:\work`) || !isInside(`C:\WORK\src\x`, `C:\work`) {
			t.Error("isInside got it wrong")
		}
	})
}
