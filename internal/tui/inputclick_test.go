package tui

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | IC-01 | Happy    | A click on the input puts the cursor on the row and column clicked          | P0       |
// | IC-02 | Edge     | A click past the end of a row puts the cursor at its end; a wrapped row counts as one | P1 |
// | MF-01 | Happy    | A code block with no language is drawn as written: prose with apostrophes takes no string colour | P0 |
// | MF-02 | Edge     | Only the fences that open a block and name nothing get text; closing ones and named ones stay | P1 |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// inputModel is a model drawn at 60 columns with text in its input.
func inputModel(t *testing.T, text string) (model, int) {
	t.Helper()
	m := conversationModel()
	resized, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = resized.(model)
	m.input.SetValue(text)
	top, ok := 0, false
	for y := range m.height {
		if row, found := m.inputRowAt(y); found && row == 0 {
			top, ok = y, true
			break
		}
	}
	if !ok {
		t.Fatal("no input on the screen")
	}
	return m, top
}

func TestInputClick(t *testing.T) {
	t.Run("IC-01 a click on the input puts the cursor on the row and column clicked", func(t *testing.T) {
		m, top := inputModel(t, "first line\nsecond line\nthird")

		clicked, _ := m.click(ansi.StringWidth(m.input.Prompt)+3, top+1)

		input := clicked.(model).input
		if input.Line() != 1 || input.Column() != 3 {
			t.Errorf("cursor at line %d column %d", input.Line(), input.Column())
		}
	})

	t.Run("IC-02 a click past the end of a row puts the cursor at its end; a wrapped row counts as one", func(t *testing.T) {
		long := strings.Repeat("word ", 20)
		m, top := inputModel(t, long+"\nshort")

		pastEnd, _ := m.click(50, top+3)
		wrapped, _ := m.click(ansi.StringWidth(m.input.Prompt)+2, top+1)

		if input := pastEnd.(model).input; input.Line() != 1 || input.Column() != len("short") {
			t.Errorf("past the end: line %d column %d", input.Line(), input.Column())
		}
		info := wrapped.(model).input.LineInfo()
		if input := wrapped.(model).input; input.Line() != 0 || info.RowOffset != 1 || input.Column() != info.StartColumn+2 {
			t.Errorf("wrapped: line %d column %d %+v", input.Line(), input.Column(), info)
		}
	})
}

func TestPlainFences(t *testing.T) {
	t.Run("MF-01 a code block with no language is drawn as written: prose with apostrophes takes no string colour", func(t *testing.T) {
		mail := "```\nHonestly, I'm not sure how a whole agent would turn into a set of instructions. To me it feels a bit like packing an IDE into a coding manual. mo is the loop, the tools and the interface itself, so I don't see a way to break it down into skills.\n```"
		p := newPrinter()

		drawn := p.Markdown(mail)
		plain := p.Markdown(strings.Replace(mail, "```\n", "```text\n", 1))

		if drawn != plain {
			t.Errorf("drawn differently from text:\n%q\n%q", drawn, plain)
		}
	})

	t.Run("MF-02 only the fences that open a block and name nothing get text; closing ones and named ones stay", func(t *testing.T) {
		markdown := "```\na\n```\n~~~go\nb\n~~~\n  ```\nc\n  ```"

		got := plainFences(markdown)

		if got != "```text\na\n```\n~~~go\nb\n~~~\n  ```text\nc\n  ```" {
			t.Errorf("got %q", got)
		}
	})
}
