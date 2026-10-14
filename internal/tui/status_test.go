package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | BR-01 | Happy    | The branch and the folder sit at the right end of the status line        | P1       |
// | BR-02 | Edge     | Without room the hint, speeds, provider and tokens go before the branch; never past the width | P0 |
// | BR-04 | Happy    | With no room for a bar the context shows in one cell, in eighths          | P2       |
// | BR-05 | Happy    | The provider shrinks to its first letter, then goes, before the speeds do | P1       |
// | BR-06 | Happy    | The plain style joins the parts with bars, without powerline arrows       | P2       |
// | BR-03 | Edge     | On master or main only the folder shows, with no separator left alone    | P2       |

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
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
	t.Run("BR-01 the branch and the folder sit at the right end of the status line", func(t *testing.T) {
		m := suffixModel(t, "feature/login")
		m.width = 120

		status := ansi.Strip(m.statusLine())

		if ansi.StringWidth(status) != 120 || !strings.HasSuffix(status, "\ue0b2 \ue0a0 feature/login \ue0b2 un-proyecto-largo ") {
			t.Errorf("status = %q", status)
		}
	})

	t.Run("BR-02 without room the hint, speeds, provider and tokens go before the branch; never past the width", func(t *testing.T) {
		m := suffixModel(t, "feature/login")
		for width := 20; width <= 100; width++ {
			m.width = width
			if status := ansi.Strip(m.statusLine()); ansi.StringWidth(status) > width {
				t.Fatalf("width %d: %d columns: %q", width, ansi.StringWidth(status), status)
			}
		}
		m.session.Provider = "llama-server"
		m.width = 72
		narrow := ansi.Strip(m.statusLine())
		if strings.Contains(narrow, "llama-server") || !strings.HasSuffix(narrow, "feature/login \ue0b2 un-proyecto-largo ") {
			t.Errorf("at 72: %q", narrow)
		}
		m.width = 55
		folderOnly := ansi.Strip(m.statusLine())
		if strings.Contains(folderOnly, "feature/login") || strings.Contains(folderOnly, "llama-server") || !strings.HasSuffix(folderOnly, "un-proyecto-largo ") {
			t.Errorf("at 55: %q", folderOnly)
		}
	})

	t.Run("BR-03 on master or main only the folder shows, with no separator left alone", func(t *testing.T) {
		m := suffixModel(t, "main")
		m.width = 120

		status := ansi.Strip(m.statusLine())

		if !strings.HasSuffix(status, "  \ue0b2 un-proyecto-largo ") || strings.Contains(status, "main") {
			t.Errorf("status = %q", status)
		}
	})

	t.Run("BR-04 with no room for a bar the context shows in one cell, in eighths", func(t *testing.T) {
		if contextGauge(0) != "▁" || contextGauge(0.5) != "▅" || contextGauge(1) != "█" {
			t.Errorf("gauges %q %q %q", contextGauge(0), contextGauge(0.5), contextGauge(1))
		}
	})

	t.Run("BR-05 the provider shrinks to its first letter, then goes, before the speeds do", func(t *testing.T) {
		m := suffixModel(t, "dev")
		m.session.Provider, m.session.Model = "llama-server", "Qwen3-8B"
		m.speed = llm.Usage{PromptRate: 900, GenerationRate: 40}
		var seen []string
		for width := 120; width >= 30; width-- {
			m.width = width
			status := ansi.Strip(m.statusLine())
			stage := "full"
			switch {
			case strings.Contains(status, "llama-server"):
			case strings.Contains(status, " l › "):
				stage = "letter"
			case strings.Contains(status, "pp 900"):
				stage = "no provider"
			case strings.Contains(status, "tg 40"):
				stage = "no pp"
			default:
				stage = "no speeds"
			}
			if len(seen) == 0 || seen[len(seen)-1] != stage {
				seen = append(seen, stage)
			}
		}
		if fmt.Sprint(seen) != "[full letter no provider no pp no speeds]" {
			t.Errorf("stages: %v", seen)
		}
	})

	t.Run("BR-06 the plain style joins the parts with bars, without powerline arrows", func(t *testing.T) {
		m := suffixModel(t, "dev")
		m.session.Provider, m.session.Model = "llama-server", "Qwen3-8B"
		m.speed = llm.Usage{GenerationRate: 40}
		m.session.Config.Render.Status = statusPlain
		m.width = 100

		status := ansi.Strip(m.statusLine())

		if !strings.Contains(status, "llama-server › Qwen3-8B │ tg 40.0 t/s") || strings.ContainsAny(status, "\ue0b0\ue0b2\ue0a0") || !strings.HasSuffix(status, "dev · un-proyecto-largo") {
			t.Errorf("status = %q", status)
		}
	})
}
