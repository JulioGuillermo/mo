package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CP-01 | Happy    | The pane's lines are the conversation's, whatever the blocks end with    | P0       |
// | CP-02 | Happy    | It shows height lines from its offset, cut or filled to the width, and follows the end until scrolled | P0 |
// | CP-03 | Happy    | The block at a line is the one the transcript puts there                  | P1       |

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func paneModel(texts ...string) model {
	m := conversationModel()
	for _, text := range texts {
		m.blocks = append(m.blocks, block{text: text, foldable: strings.HasPrefix(text, "fold")})
	}
	return m
}

func TestConversationPane(t *testing.T) {
	t.Run("CP-01 the pane's lines are the conversation's, whatever the blocks end with", func(t *testing.T) {
		shapes := [][]string{
			nil, {""}, {"one"}, {"one", "two\nthree"}, {"a\n", "\n", ""}, {"\n\n"}, {"x", "", "y\n\n"},
			{"fold\nme", "plain\n"}, {"\na\n\nb"},
		}
		for _, shape := range shapes {
			for _, live := range []string{"", "writing\nmore"} {
				m := paneModel(shape...)
				if live != "" {
					m.running = true
					m = streaming(m, llm.Event{Kind: llm.EventText, Text: live})
				}
				want := strings.Split(m.conversationContent(), "\n")
				lines := m.conversationLines()
				got := lines.window(0, lines.total)
				if !slices.Equal(got, want) || lines.total != len(want) {
					t.Errorf("%q live %q:\ngot  %q\nwant %q", shape, live, got, want)
				}
				for from := range len(want) {
					if part := lines.window(from, from+2); !slices.Equal(part, want[from:min(from+2, len(want))]) {
						t.Errorf("%q from %d: %q", shape, from, part)
					}
				}
			}
		}
	})

	t.Run("CP-02 it shows height lines from its offset, cut or filled to the width, and follows the end until scrolled", func(t *testing.T) {
		var texts []string
		for i := range 30 {
			texts = append(texts, fmt.Sprintf("line %d %s", i, strings.Repeat("w", i*5)))
		}
		m := paneModel(texts...)
		m.width = 60

		end := strings.Split(ansi.Strip(m.conversation(5).View()), "\n")
		pane := m.conversation(5)
		pane.ScrollUp(10)
		m.top, m.scrolled = pane.YOffset(), !pane.AtBottom()
		up := strings.Split(ansi.Strip(m.conversation(5).View()), "\n")
		m.blocks = append(m.blocks, block{text: "new"})
		kept := strings.Split(ansi.Strip(m.conversation(5).View()), "\n")

		if len(end) != 5 || !strings.HasPrefix(end[4], "line 29") || !strings.HasPrefix(up[0], "line 15") || !slices.Equal(up, kept) {
			t.Errorf("end %q\nup %q\nkept %q", end, up, kept)
		}
		for _, line := range append(end, up...) {
			if ansi.StringWidth(line) != m.mainWidth() {
				t.Errorf("width %d: %q", ansi.StringWidth(line), line)
			}
		}
	})

	t.Run("CP-03 the block at a line is the one the transcript puts there", func(t *testing.T) {
		m := paneModel("fold\na", "plain", "fold\nb\nc", "tail\n")
		_, foldable := m.transcript()
		lines := m.conversationLines()

		for row := range lines.total {
			i, found := lines.blockAt(row)
			want, folds := foldable[row]
			if folds != (found && m.blocks[i].foldable) || (folds && i != want) {
				t.Errorf("row %d: block %d %v, transcript %d %v", row, i, found, want, folds)
			}
		}
	})
}

func BenchmarkConversationView(b *testing.B) {
	m := conversationModel()
	m.width, m.height = 160, 50
	answer := m.printer.Answer(strings.Repeat("Some **markdown** text to wrap around the screen. ", 12))
	for i := range 5000 {
		m.blocks = append(m.blocks, block{text: m.printer.User(fmt.Sprint("question ", i))}, block{text: answer})
	}
	for b.Loop() {
		_ = m.View()
	}
}
