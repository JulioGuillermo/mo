package agent

// Test Traceability Table
// | ID    | Category | Description                                                         | Priority |
// | WA-01 | Happy    | A pasted Windows path keeps its backslashes and becomes a file       | P0       |
// | WA-02 | Happy    | Spaces escaped with backticks, as Windows Terminal pastes, are kept  | P0       |
// | WA-03 | Happy    | A quoted path with spaces is one file                               | P1       |

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPastedWindowsPaths(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "shot.png")
	spaced := filepath.Join(dir, "Screenshot 2026-10-21 083225.png")
	for _, path := range []string{plain, spaced} {
		os.WriteFile(path, []byte("x"), 0o600)
	}

	t.Run("WA-01 a pasted Windows path keeps its backslashes and becomes a file", func(t *testing.T) {
		if got := PastedFiles(plain, dir); !slices.Equal(got, []string{plain}) {
			t.Errorf("files = %q", got)
		}
	})

	t.Run("WA-02 spaces escaped with backticks, as Windows Terminal pastes, are kept", func(t *testing.T) {
		pasted := strings.ReplaceAll(spaced, " ", "` ")

		if got := PastedFiles(pasted, dir); !slices.Equal(got, []string{spaced}) {
			t.Errorf("files = %q", got)
		}
	})

	t.Run("WA-03 a quoted path with spaces is one file", func(t *testing.T) {
		if got := PastedFiles(`"`+spaced+`"`, dir); !slices.Equal(got, []string{spaced}) {
			t.Errorf("files = %q", got)
		}
	})
}
