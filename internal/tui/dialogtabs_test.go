package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | DT-01 | Happy    | Tabs that do not fit in a row, or not named whole, go one to a row, the count on the first | P0 |
// | DT-02 | Happy    | A click on a row opens its part                                          | P0       |
// | DT-03 | Edge     | Tabs that fit named whole stay in one row, as before                     | P1       |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/tools"
)

func dialogText(m model) []string {
	text, _ := m.dialogLayout()
	return strings.Split(ansi.Strip(text), "\n")
}

func TestDialogTabs(t *testing.T) {
	long := "Which title do I give the pull request about the monitor column"
	manyQuestions := agent.Interaction{Questions: []tools.Question{{Text: long}, {Text: "What colour for the new badge in the table?"}, {Text: "And the docs?"}}}

	t.Run("DT-01 tabs that do not fit in a row, or not named whole, go one to a row, the count on the first", func(t *testing.T) {
		m, _ := asking(manyQuestions)
		m.width = 100

		lines := dialogText(m)

		if !strings.Contains(lines[0], long) || !strings.Contains(lines[1], "What colour for the new badge") {
			t.Errorf("rows:\n%s", strings.Join(lines[:4], "\n"))
		}
		if !strings.HasPrefix(lines[2], " ? And the docs?") || !strings.Contains(lines[0], "1 of 3 · 3 left") {
			t.Errorf("first row %q last %q", lines[0], lines[2])
		}
		if ansi.StringWidth(strings.TrimRight(lines[1], " ")) > ansi.StringWidth(lines[0]) || ansi.StringWidth(lines[0]) > 100 {
			t.Errorf("rows not as wide, or wider than the screen:\n%s", strings.Join(lines[:3], "\n"))
		}
	})

	t.Run("DT-02 a click on a row opens its part", func(t *testing.T) {
		m, _ := asking(manyQuestions)
		m.width = 100
		_, regions := m.dialogLayout()

		for _, r := range regions {
			if r.y == 2 && r.x0 == 0 {
				next, _ := r.do(m)
				m = next.(model)
			}
		}

		if m.dialog.current != 2 {
			t.Errorf("the click on the third row opened %d", m.dialog.current)
		}
	})

	t.Run("DT-03 tabs that fit named whole stay in one row, as before", func(t *testing.T) {
		m, _ := asking(agent.Interaction{Questions: []tools.Question{{Text: "Name?"}, {Text: "Colour?"}}})
		m.width = 100

		lines := dialogText(m)

		if !strings.Contains(lines[0], "Name?") || !strings.Contains(lines[0], "Colour?") || !strings.Contains(lines[0], "1 of 2") {
			t.Errorf("first row %q", lines[0])
		}
	})
}
