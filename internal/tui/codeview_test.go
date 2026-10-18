package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CV-01 | Happy    | A text painted in rows is what highlightLines painted, row for row        | P0       |
// | CV-02 | Edge     | A chunk never begins inside a docstring, a raw string or a block comment | P0       |
// | CV-03 | Happy    | The tail of a text is the last rows of the whole text painted, a string cut by the window included | P0 |
// | CV-04 | Edge     | What was recognised is kept: the same text again, or at another width, recognises nothing | P0 |
// | CV-05 | Edge     | The line being written is painted by itself, and does not enter the chunks | P1     |
// | CV-06 | Edge     | A line wrapped into rows is cut whole, by columns, wide characters too   | P1       |
// | CV-07 | Edge     | The cost of the tail does not grow with the length of the text           | P0       |
// | CV-08 | Edge     | A language that is not known is plain text; empty text is a row         | P2       |

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var goSnippet = `package main

import "fmt"

// main says hello.
func main() {
	name := "world"
	fmt.Println("hello", name, 42)
}
`

func TestCodeView(t *testing.T) {
	background := panelBackground(true)

	t.Run("CV-01 a text painted in rows is what highlightLines painted, row for row", func(t *testing.T) {
		code := newCodeText(goSnippet, "Go", 1<<20)

		got := code.render(0, len(code.rows), func(int) color.Color { return background }, true)
		want := highlightLines(goSnippet, "Go", 1<<20, background, true)

		if len(got) != len(want) {
			t.Fatalf("%d rows, want %d", len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("row %d:\n got %q\nwant %q", i, got[i], want[i])
			}
		}
	})

	t.Run("CV-02 a chunk never begins inside a docstring, a raw string or a block comment", func(t *testing.T) {
		var lines []string
		for i := range 400 {
			lines = append(lines, fmt.Sprintf("x%d = %d", i, i))
		}
		// A docstring across where a chunk would end, with blank and unindented lines in it.
		lines = append(lines, "", `def f():`, `    """`, "", "not code", "", "still the docstring", `    """`, "", "after = 1")
		for i := range 300 {
			lines = append(lines, fmt.Sprintf("y%d = %d", i, i))
		}
		docStart, docEnd := 402, 407

		for _, start := range chunkStarts(lines, 100, 600) {
			if start > docStart && start <= docEnd {
				t.Errorf("a chunk begins at line %d, inside the docstring %d-%d", start, docStart, docEnd)
			}
		}

		raw := append(append([]string{}, lines[:100]...), "", "var s = `", "", "text", "", "more", "`", "")
		raw = append(raw, lines[:200]...)
		for _, start := range chunkStarts(raw, 100, 600) {
			if start >= 102 && start <= 106 {
				t.Errorf("a chunk begins at line %d, inside the raw string", start)
			}
		}
	})

	t.Run("CV-03 the tail of a text is the last rows of the whole text painted, a string cut by the window included", func(t *testing.T) {
		var script strings.Builder
		script.WriteString("set -e\npython3 -c \"\nimport os\n")
		for i := range 60 {
			fmt.Fprintf(&script, "value_%d = os.getenv('HOME') + 'x'\n", i)
		}
		script.WriteString("print(value_1)\n\" | tail -n 5\n")
		text := script.String()
		whole := newCodeText(text, "bash", 1<<20)
		all := whole.render(0, len(whole.rows), func(int) color.Color { return background }, true)

		tail := codeTail(text, "bash", 8, 1<<20, background, true)

		if len(tail) != 8 {
			t.Fatalf("%d rows", len(tail))
		}
		for i, row := range tail {
			if row != all[len(all)-8+i] {
				t.Errorf("row %d of the tail:\n got %q\nwant %q", i, row, all[len(all)-8+i])
			}
		}
		// The window starts well inside the python: cut alone it would read as shell.
		alone := codeTail(strings.Join(strings.Split(text, "\n")[40:], "\n"), "bash", 8, 1<<20, background, true)
		if alone[0] == tail[0] && strings.Contains(ansi.Strip(tail[0]), "value_") {
			t.Log("the text cut at the window colours the same here")
		}
	})

	t.Run("CV-04 what was recognised is kept: the same text again, or at another width, recognises nothing", func(t *testing.T) {
		text := strings.Repeat(goSnippet, 3) + "// kept for this test " + t.Name() + "\n"
		newCodeText(text, "Go", 80).render(0, 10, func(int) color.Color { return background }, true)
		before := recognised.Load()

		narrow := newCodeText(text, "Go", 30)
		narrow.render(0, 20, func(int) color.Color { return background }, true)
		newCodeText(text, "Go", 80).render(0, 10, func(int) color.Color { return panelBackground(false) }, false)

		if recognised.Load() != before {
			t.Errorf("recognised %d more times", recognised.Load()-before)
		}
	})

	t.Run("CV-05 the line being written is painted by itself, and does not enter the chunks", func(t *testing.T) {
		text := goSnippet + "\tfmt.Println(\"half a li"
		before := recognised.Load()

		first := codeTail(text, "Go", 6, 100, background, true)
		after := recognised.Load()
		again := codeTail(text, "Go", 6, 100, background, true)
		same := recognised.Load()
		longer := codeTail(text+"ne\")", "Go", 6, 100, background, true)

		if len(first) != 6 || !strings.Contains(ansi.Strip(first[5]), `fmt.Println("half a li`) || strings.Contains(ansi.Strip(first[4]), "half") {
			t.Errorf("rows %q", ansi.Strip(strings.Join(first, "|")))
		}
		if after-before > 2 || same != after || len(again) != 6 {
			t.Errorf("recognised %d texts for one tail, %d more for the same text", after-before, same-after)
		}
		// Only the line being written is new: the complete lines are as they were.
		if recognised.Load()-same != 1 || len(longer) != 6 {
			t.Errorf("a longer line recognised %d texts", recognised.Load()-same)
		}
	})

	t.Run("CV-06 a line wrapped into rows is cut whole, by columns, wide characters too", func(t *testing.T) {
		for _, line := range []string{"", "short", strings.Repeat("abcdefghij", 7), "ünïcödé " + strings.Repeat("字", 15) + " end", strings.Repeat("字", 11)} {
			spans := wrapSpans(line, 10)
			var joined strings.Builder
			for _, span := range spans {
				piece := sliceRunes(line, span[0], span[1])
				if ansi.StringWidth(piece) > 10 {
					t.Errorf("%q: a row of %d columns", line, ansi.StringWidth(piece))
				}
				joined.WriteString(piece)
			}
			if joined.String() != line {
				t.Errorf("rows of %q join to %q", line, joined.String())
			}
		}
	})

	t.Run("CV-07 the cost of the tail does not grow with the length of the text", func(t *testing.T) {
		build := func(lines int) string {
			var text strings.Builder
			for i := range lines {
				fmt.Fprintf(&text, "func f%d(a int) int { return a + %d } // %d\n", i, i, i)
			}
			return text.String()
		}
		measure := func(text string) time.Duration {
			start := time.Now()
			for range 3 {
				codeTail(text+"partial", "Go", 12, 100, background, true)
			}
			return time.Since(start) / 3
		}
		short, long := measure(build(300)), measure(build(30000))

		if long > short*4+30*time.Millisecond {
			t.Errorf("300 lines %v, 30000 lines %v", short, long)
		}
	})

	t.Run("CV-08 a language that is not known is plain text; empty text is a row", func(t *testing.T) {
		plain := newCodeText("one\ntwo", "No Such Language", 20)
		empty := newCodeText("", "Go", 20)

		rows := plain.render(0, 2, func(int) color.Color { return background }, true)
		if len(rows) != 2 || ansi.Strip(rows[0]) != "one" || ansi.Strip(rows[1]) != "two" || len(empty.rows) != 1 || empty.plain(0) != "" {
			t.Errorf("rows %q empty %d", rows, len(empty.rows))
		}
	})

	t.Run("CV-09 no lines and one blank line are not the same text to the cache", func(t *testing.T) {
		none := tokeniseLines("Go", nil)
		blank := tokeniseLines("Go", []string{""})

		if len(none) != 0 || len(blank) != 1 {
			t.Errorf("no lines gave %d rows, a blank line %d", len(none), len(blank))
		}
	})
}
