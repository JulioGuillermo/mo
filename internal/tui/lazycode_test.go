package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | LZ-01 | Happy    | A long write opened is plain in the text and painted in the rows in view | P0       |
// | LZ-02 | Edge     | Lines taken from a text are those of cutting it, whatever the range      | P1       |
// | LZ-03 | Happy    | An edit's diff colours each line by the old or the new text as a whole   | P0       |
// | LZ-04 | Happy    | A bash command being written is coloured by the whole of it, the window cut inside a string | P0 |
// | LZ-05 | Edge     | A write being written shows its last rows coloured, the line in progress too | P1   |
// | LZ-06 | Edge     | Opening a block of many rows takes what the screen shows, not the size   | P0       |
// | LZ-07 | Edge     | A diff row of a line wrapped keeps its background and its mark on the first | P2    |

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func bigWrite(lines int) (string, json.RawMessage) {
	var src strings.Builder
	for i := range lines {
		fmt.Fprintf(&src, "func f%d(a int) string { return fmt.Sprint(\"v\", a) } // %d\n", i, i)
	}
	args, _ := json.Marshal(map[string]any{"path": "big.go", "content": src.String()})
	return src.String(), args
}

func TestLazyCode(t *testing.T) {
	t.Run("LZ-01 a long write opened is plain in the text and painted in the rows in view", func(t *testing.T) {
		m := conversationModel()
		_, args := bigWrite(3000)
		call := llm.ToolCall{ID: "w", Name: "write", Args: args}
		m.addTool(call, "big.go")
		m.finishTool("w", "wrote", false)
		m.toggleDetails()

		block := m.blocks[len(m.blocks)-1]
		if block.lazy == nil || block.lazy.count != 3000 || strings.Contains(block.text, keywordColor) {
			t.Fatalf("lazy %+v, painted in the text: %v", block.lazy, strings.Contains(block.text, keywordColor))
		}
		lines := m.conversationLines()
		from := lines.starts[len(lines.starts)-2] + block.lazy.first + 1500
		window := lines.window(from, from+30)

		if len(window) != 30 {
			t.Fatalf("%d rows", len(window))
		}
		for i, row := range window {
			if !strings.Contains(row, keywordColor) || !strings.Contains(ansi.Strip(row), fmt.Sprintf("func f%d(", 1500+i)) {
				t.Errorf("row %d: %q", i, ansi.Strip(row))
				break
			}
		}
		head := lines.window(lines.starts[len(lines.starts)-2], lines.starts[len(lines.starts)-2]+block.lazy.first)
		if len(head) != block.lazy.first || strings.Contains(strings.Join(head, ""), keywordColor) {
			t.Errorf("the lines over the code: %q", head)
		}
	})

	t.Run("LZ-02 lines taken from a text are those of cutting it, whatever the range", func(t *testing.T) {
		text := "a\nbb\n\nccc\ndddd"
		all := strings.Split(text, "\n")
		for from := 0; from <= 5; from++ {
			for to := from; to <= 6; to++ {
				got := nthLines(text, from, to)
				want := all[min(from, len(all)):min(to, len(all))]
				if strings.Join(got, "|") != strings.Join(want, "|") {
					t.Errorf("lines %d-%d: %q, want %q", from, to, got, want)
				}
			}
		}
	})

	t.Run("LZ-03 an edit's diff colours each line by the old or the new text as a whole", func(t *testing.T) {
		p := newPrinter()
		args := json.RawMessage(`{"path":"a.go","old":"x := ` + "`" + `\nline one\nline two\n` + "`" + `\nz := 1","new":"x := ` + "`" + `\nline one\nline 2\n` + "`" + `\nz := 1"}`)
		run := toolRun{name: "edit", args: args, summary: "a.go", output: "edited", done: true}

		open := p.Tool(run, true)

		for _, want := range []string{"line one", "line two", "line 2"} {
			row := ""
			for _, line := range strings.Split(open, "\n") {
				if strings.Contains(ansi.Strip(line), want) {
					row = line
				}
			}
			// Inside the raw string they are a string; a line read by itself would not be.
			if row == "" || !strings.Contains(row, stringColor) {
				t.Errorf("%q is not coloured as a string: %q", want, row)
			}
		}
		if !strings.Contains(open, removedBg) || !strings.Contains(open, addedBg) {
			t.Error("the lines taken out and put in lost their background")
		}
	})

	t.Run("LZ-04 a bash command being written is coloured by the whole of it, the window cut inside a string", func(t *testing.T) {
		m := conversationModel()
		var script strings.Builder
		script.WriteString("python3 -c \"\nimport os\n")
		for i := range 30 {
			fmt.Fprintf(&script, "value_%d = os.getenv('HOME')\n", i)
		}
		quoted, _ := json.Marshal(script.String())
		// JSON cut short, as it arrives: the string is not closed yet.
		args := `{"command":` + string(quoted[:len(quoted)-1])

		drawn := m.formingCallView("bash", args, markRunning)

		rows := strings.Split(drawn, "\n")
		code := rows[len(rows)-formingCommand:]
		if len(code) != formingCommand {
			t.Fatalf("%d rows", len(rows))
		}
		// The window starts well inside the string: its rows are the string's colour.
		if !strings.Contains(code[0], stringColor) || strings.Contains(code[0], keywordColor) || !strings.Contains(ansi.Strip(code[0]), "value_") {
			t.Errorf("first row of the window: %q", code[0])
		}
	})

	t.Run("LZ-05 a write being written shows its last rows coloured, the line in progress too", func(t *testing.T) {
		m := conversationModel()

		drawn := m.formingWindow("a.go", "package a\n\nfunc one() {}\nfunc two() { return \"par")

		plain := ansi.Strip(drawn)
		if !strings.Contains(plain, "func one()") || !strings.Contains(plain, `func two() { return "par`) || !strings.Contains(drawn, keywordColor) {
			t.Errorf("%s", plain)
		}
		if strings.Count(plain, "▌") != 4 {
			t.Errorf("%d rows: %s", strings.Count(plain, "▌"), plain)
		}
	})

	t.Run("LZ-06 opening a block of many rows takes what the screen shows, not the size", func(t *testing.T) {
		measure := func(lines int) time.Duration {
			_, args := bigWrite(lines)
			run := toolRun{name: "write", args: args, summary: "big.go", output: "ok", done: true}
			p := newPrinter()
			start := time.Now()
			_, lazy := p.toolLazy(run, true)
			lazy.render(lines/2, lines/2+40)
			return time.Since(start)
		}
		measure(2000)
		small, large := measure(2000), measure(40000)

		// The text of rows is built by size, but a few hundred ms for 40000 lines, not the seconds colouring them all took.
		if large > 3*time.Second || large > small*60 {
			t.Errorf("2000 lines %v, 40000 lines %v", small, large)
		}
	})

	t.Run("LZ-07 a diff row of a line wrapped keeps its background and its mark on the first", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(40)
		args := json.RawMessage(`{"path":"a.go","old":"x := 1","new":"x := \"` + strings.Repeat("abcdefghij", 8) + `\""}`)
		run := toolRun{name: "edit", args: args, summary: "a.go", output: "edited", done: true}

		var added []string
		for _, line := range strings.Split(p.Tool(run, true), "\n") {
			if strings.Contains(line, addedBg) {
				added = append(added, ansi.Strip(line))
			}
		}

		if len(added) < 3 || !strings.Contains(added[0], "+ x :=") || strings.Contains(added[1], "+ ") {
			t.Errorf("rows %q", added)
		}
	})
}
