package tui

// Test Traceability Table
// | ID    | Category   | Description                                                                  | Priority |
// | QB-01 | Regression | A quote with inline code wraps as one paragraph: every line has its bar       | P0       |
// | QB-02 | Edge       | A quote in a quote has both bars on every line, none wider than the screen    | P1       |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func quotedLines(t *testing.T, markdown string) []string {
	t.Helper()
	p := newPrinter()
	p.setWidth(76)
	var lines []string
	for _, line := range strings.Split(ansi.Strip(p.Markdown(markdown)), "\n") {
		if strings.Trim(line, " │") != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestQuoteBar(t *testing.T) {
	t.Run("QB-01 a quote with inline code wraps as one paragraph: every line has its bar", func(t *testing.T) {
		lines := quotedLines(t, "> Confirmed via API: a rule with antispam + SMTP saves fine, then `PUT /besafe_objects/:id` to port 80 returns 200 and the rule keeps antispam with no mail port. Antispam then filters nothing, and the next unrelated\n> edit of that rule fails with `ANTISPAM_MAIL_SERVICE_REQUIRED`. Same when\n> SMTP is removed from a group.")

		joined := strings.Join(lines, "\n")

		for _, line := range lines {
			if !strings.HasPrefix(line, "  │ ") || ansi.StringWidth(line) > 76 {
				t.Errorf("line without its bar or too wide: %q", line)
			}
		}
		if strings.Contains(joined, "\U000F0000") || !strings.Contains(joined, "next unrelated edit of") {
			t.Errorf("quote:\n%s", joined)
		}
	})

	t.Run("QB-02 a quote in a quote has both bars on every line, none wider than the screen", func(t *testing.T) {
		lines := quotedLines(t, "> > a nested quote with `code` that goes on and on to wrap at the width of the screen, surely, and once more")

		for _, line := range lines {
			if !strings.HasPrefix(line, "  │ │ ") || ansi.StringWidth(line) > 76 {
				t.Errorf("line %q", line)
			}
		}
		if len(lines) < 2 {
			t.Errorf("it should wrap: %q", lines)
		}
	})
}
