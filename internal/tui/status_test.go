package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | BR-01 | Happy    | The branch and the folder sit at the right end of the status line        | P1       |
// | BR-02 | Edge     | Without room the hint, speeds, provider and tokens go before the branch; never past the width | P0 |
// | BR-04 | Happy    | With no room for a bar the context shows in one cell, in eighths          | P2       |
// | BR-05 | Happy    | The provider shrinks to its first letter, then goes, before the speeds do | P1       |
// | BR-06 | Happy    | The plain style joins the parts with bars, without powerline arrows       | P2       |
// | BR-07 | Happy    | The changes are counted by kind before the branch, Nerd icons or text; a click lists them | P1 |
// | BR-08 | Edge     | One read of the repository at a time; one asked meanwhile runs after it  | P1       |
// | BR-03 | Edge     | The branch shows on master and main too; outside a repository only the folder | P2   |

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
	"github.com/jg/mo/internal/llm"
)

func suffixModel(t *testing.T, branch string) model {
	t.Helper()
	m := conversationModel()
	m.session.WorkDir = filepath.Join(t.TempDir(), "un-proyecto-largo")
	m.session.Model = "Qwen3-Coder-30B-A3B-Instruct"
	updated, _ := m.Update(repoMsg{branch: branch})
	return updated.(model)
}

func TestStatusSuffix(t *testing.T) {
	t.Run("BR-01 the branch and the folder sit at the right end of the status line", func(t *testing.T) {
		m := suffixModel(t, "feature/login")
		m.width = 120

		status := ansi.Strip(m.statusLine())

		if ansi.StringWidth(status) != 120 || !strings.HasSuffix(status, "\ue0b2 \U000f0216 \ue0b2 \ue0a0 feature/login \ue0b2 un-proyecto-largo ") {
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
		if strings.Contains(narrow, "llama-server") || !strings.HasSuffix(narrow, "\ue0a0 feature/login \ue0b2 un-proyecto-largo ") {
			t.Errorf("at 72: %q", narrow)
		}
		m.width = 55
		folderOnly := ansi.Strip(m.statusLine())
		if strings.Contains(folderOnly, "feature/login") || strings.Contains(folderOnly, "llama-server") || !strings.HasSuffix(folderOnly, "un-proyecto-largo ") {
			t.Errorf("at 55: %q", folderOnly)
		}
	})

	t.Run("BR-03 the branch shows on master and main too; outside a repository only the folder", func(t *testing.T) {
		m := suffixModel(t, "master")
		m.width = 120
		none := suffixModel(t, "")
		none.width = 120

		status, outside := ansi.Strip(m.statusLine()), ansi.Strip(none.statusLine())

		if !strings.HasSuffix(status, "\U000f0216 \ue0b2 \ue0a0 master \ue0b2 un-proyecto-largo ") {
			t.Errorf("status = %q", status)
		}
		if !strings.HasSuffix(outside, "  \ue0b2 un-proyecto-largo ") || strings.Contains(outside, "\ue0a0") {
			t.Errorf("outside = %q", outside)
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
		m.session.Config.Render.Icons = iconsText
		m.width = 100

		status := ansi.Strip(m.statusLine())

		if !strings.Contains(status, "llama-server › Qwen3-8B │ tg 40.0 t/s") || strings.ContainsAny(status, "\ue0b0\ue0b2\ue0a0") || !strings.HasSuffix(status, "✓ · dev · un-proyecto-largo") {
			t.Errorf("status = %q", status)
		}
	})

	t.Run("BR-07 the branch counts its changes by kind in their colours; a click lists them", func(t *testing.T) {
		m := suffixModel(t, "dev")
		updated, _ := m.Update(repoMsg{branch: "dev", changes: []gitops.Change{
			{Path: "a.go", Kind: gitops.ChangeModified}, {Path: "b.go", Kind: gitops.ChangeModified},
			{Path: "c.go", Kind: gitops.ChangeAdded}, {Path: "d.go", Kind: gitops.ChangeDeleted}, {Path: "e.go", Kind: gitops.ChangeUntracked},
		}})
		m = updated.(model)
		m.width = 120

		status := ansi.Strip(m.statusLine())
		_, regions := m.statusLayout()
		x := ansi.StringWidth(status[:strings.Index(status, "dev")])
		clicked := m
		for _, r := range regions {
			if r.holds(x, 0) && r.do != nil {
				done, _ := r.do(m)
				clicked = done.(model)
			}
		}

		text := m
		text.session.Config.Render.Icons = iconsText
		if !strings.Contains(status, "\ue0b2 \U000f0752 1 \U000f11e7 2 \U000f1aa1 1 \U000f086f 1 \ue0b2 \ue0a0 dev") {
			t.Errorf("status = %q", status)
		}
		if plain := ansi.Strip(text.statusLine()); !strings.Contains(plain, "\ue0b2 +1 ~2 -1 ?1 \ue0b2 dev") {
			t.Errorf("text icons = %q", plain)
		}
		if listed := shown(clicked); !strings.Contains(listed, "5 changed on dev") || !strings.Contains(listed, "~ a.go") || !strings.Contains(listed, "? e.go") {
			t.Errorf("listed:\n%s", listed)
		}
		m.width = 62
		if narrow := ansi.Strip(m.statusLine()); strings.Contains(narrow, "~2") || !strings.Contains(narrow, "dev") {
			t.Errorf("narrow = %q", narrow)
		}
	})

	t.Run("BR-08 one read of the repository at a time; one asked meanwhile runs after it", func(t *testing.T) {
		m := suffixModel(t, "dev")
		m.repoReading = false

		first := m.readRepo()
		second := m.readRepo()
		again := m.repoRead(repoMsg{branch: "dev"})
		after := m.repoRead(repoMsg{branch: "dev"})

		if first == nil || second != nil || again == nil || after != nil {
			t.Errorf("first %v second %v again %v after %v", first != nil, second != nil, again != nil, after != nil)
		}
	})
}
