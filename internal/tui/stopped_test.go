package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | ST-01 | Happy    | A refused answer shows its text, then the note red on a dark red band with ⊘ | P0     |
// | ST-02 | Happy    | A silent turn's note shows the same way, alone                            | P1       |
// | ST-03 | Edge     | An answer that ended well has no band                                     | P1       |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func TestStoppedAnswers(t *testing.T) {
	colors := changePalette(true)
	band := backgroundOf(colors.removed)

	t.Run("ST-01 a refused answer shows its text, then the note red on a dark red band with ⊘", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Answer("Sure, here\n\n" + llm.RefusedNote)

		plain := ansi.Strip(drawn)
		if !strings.Contains(plain, "Sure, here") || !strings.Contains(plain, "⊘ the provider's safety filter stopped this answer") || strings.Contains(plain, "[") {
			t.Errorf("plain:\n%s", plain)
		}
		if !strings.Contains(drawn, band) {
			t.Errorf("no dark red band: %q", drawn)
		}
	})

	t.Run("ST-02 a silent turn's note shows the same way, alone", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Answer(llm.SilentNote)

		if plain := ansi.Strip(drawn); !strings.HasPrefix(strings.TrimLeft(plain, "\n"), " ⊘ the model ended without answering") || !strings.Contains(drawn, band) {
			t.Errorf("drawn = %q", drawn)
		}
	})

	t.Run("ST-03 an answer that ended well has no band", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Answer("all good")

		if strings.Contains(drawn, band) || strings.Contains(ansi.Strip(drawn), "⊘") {
			t.Errorf("drawn = %q", drawn)
		}
	})
}
