package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SB-01 | Happy    | The branch and the folder sit at the right end of the status line        | P1       |
// | SB-02 | Edge     | Without room the branch goes first, then the folder; never past the width | P0      |
// | SB-03 | Edge     | On master or main only the folder shows, with no separator left alone    | P2       |

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func suffixModel(t *testing.T, branch string) model {
	t.Helper()
	m := conversationModel()
	m.session.WorkDir = filepath.Join(t.TempDir(), "un-proyecto-largo")
	m.session.Model = "Qwen3-Coder-30B-A3B-Instruct"
	updated, _ := m.Update(branchMsg(branch))
	return updated.(model)
}

func TestStatusSuffix(t *testing.T) {
	t.Run("SB-01 the branch and the folder sit at the right end of the status line", func(t *testing.T) {
		m := suffixModel(t, "feature/login")
		m.width = 120

		status := ansi.Strip(m.statusLine())

		if ansi.StringWidth(status) != 120 || !strings.HasSuffix(status, " feature/login · un-proyecto-largo") {
			t.Errorf("status = %q", status)
		}
	})

	t.Run("SB-02 without room the branch goes first, then the folder; never past the width", func(t *testing.T) {
		m := suffixModel(t, "feature/login")
		for width := 20; width <= 100; width++ {
			m.width = width
			if status := ansi.Strip(m.statusLine()); ansi.StringWidth(status) > width {
				t.Fatalf("width %d: %d columns: %q", width, ansi.StringWidth(status), status)
			}
		}
		m.width = 70
		folderOnly := ansi.Strip(m.statusLine())
		if strings.Contains(folderOnly, "feature/login") || !strings.HasSuffix(folderOnly, "un-proyecto-largo") {
			t.Errorf("at 70: %q", folderOnly)
		}
	})

	t.Run("SB-03 on master or main only the folder shows, with no separator left alone", func(t *testing.T) {
		m := suffixModel(t, "main")
		m.width = 120

		status := ansi.Strip(m.statusLine())

		if !strings.HasSuffix(status, "  un-proyecto-largo") || strings.Contains(status, "main") || strings.HasSuffix(strings.TrimSuffix(status, "un-proyecto-largo"), "· ") {
			t.Errorf("status = %q", status)
		}
	})
}
