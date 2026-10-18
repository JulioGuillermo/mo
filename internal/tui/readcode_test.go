package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | RC-01 | Happy    | What read gave of a file whose language is known is coloured, open       | P0       |
// | RC-02 | Happy    | Folded, the first lines are coloured and the rest is counted             | P0       |
// | RC-03 | Edge     | An error, an empty file, an unknown language or a part not there stay plain | P0    |
// | RC-04 | Happy    | The numbers of the lines are a column, apart from the code               | P0       |
// | RC-05 | Edge     | The line that says where a part ended is not coloured as code            | P1       |
// | RC-06 | Edge     | A long line wraps into rows of the same width, the number on the first   | P1       |
// | RC-07 | Edge     | A very long file is coloured up to a limit and the rest is counted       | P2       |
// | RC-08 | Edge     | A tab is four spaces, as in a file written; blank lines stay              | P2       |

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const goFile = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"

func readRun(path, output string, extra map[string]any) toolRun {
	fields := map[string]any{"path": path}
	for key, value := range extra {
		fields[key] = value
	}
	args, _ := json.Marshal(fields)
	return toolRun{name: "read", args: args, summary: path, output: output, done: true}
}

func TestReadCode(t *testing.T) {
	t.Run("RC-01 what read gave of a file whose language is known is coloured, open", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Tool(readRun("main.go", goFile, nil), true)

		if !strings.Contains(drawn, keywordColor) || !strings.Contains(drawn, stringColor) || !strings.Contains(ansi.Strip(drawn), "fmt.Println(\"hello\")") || !strings.Contains(drawn, "▌") {
			t.Errorf("%q", drawn)
		}
	})

	t.Run("RC-02 folded, the first lines are coloured and the rest is counted", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Tool(readRun("main.go", goFile, nil), false)
		plain := ansi.Strip(drawn)

		if !strings.Contains(drawn, keywordColor) || !strings.Contains(plain, "package main") || strings.Contains(plain, "fmt.Println") || !strings.Contains(plain, "… +3 lines") || !strings.Contains(plain, "ctrl+o or click") {
			t.Errorf("%s", plain)
		}
	})

	t.Run("RC-03 an error, an empty file, an unknown language or a part not there stay plain", func(t *testing.T) {
		p := newPrinter()
		failed := readRun("main.go", "open main.go: no such file", nil)
		failed.isError = true
		cases := map[string]toolRun{
			"error":    failed,
			"empty":    readRun("main.go", "(empty file)", nil),
			"unknown":  readRun("notes.zzz", "for x in y do z", nil),
			"past end": readRun("main.go", "offset 50 is past the end: 7 lines", map[string]any{"offset": 50}),
			"no match": readRun("main.go", `no line matches "zzz"`, map[string]any{"match": "zzz"}),
			"running":  {name: "read", args: json.RawMessage(`{"path":"main.go"}`)},
		}
		for name, run := range cases {
			// The path over the result is shaded too: code is what has a ▌ after it.
			drawn := p.Tool(run, true)
			if strings.Contains(drawn, keywordColor) || strings.Count(drawn, "▌") != 1 {
				t.Errorf("%s was drawn as code: %q", name, drawn)
			}
		}
	})

	t.Run("RC-04 the numbers of the lines are a column, apart from the code", func(t *testing.T) {
		p := newPrinter()
		output := "5\tfunc main() {\n6\t\tfmt.Println(\"hello\")\n7\t}\n"

		drawn := p.Tool(readRun("main.go", output, map[string]any{"numbered": true}), true)
		plain := ansi.Strip(drawn)

		if !strings.Contains(plain, " 5 func main() {") || !strings.Contains(plain, " 6     fmt.Println(\"hello\")") || strings.Contains(plain, "5\tfunc") || !strings.Contains(drawn, keywordColor) {
			t.Errorf("%s", plain)
		}
	})

	t.Run("RC-05 the line that says where a part ended is not coloured as code", func(t *testing.T) {
		p := newPrinter()
		output := "1\tpackage main\n2\t\n3\tfunc a() {}\n[lines 1-3 of 90; offset=3 continues]\n"

		drawn := p.Tool(readRun("main.go", output, map[string]any{"numbered": true, "limit": 3}), true)
		plain := ansi.Strip(drawn)

		if !strings.Contains(plain, "[lines 1-3 of 90; offset=3 continues]") || strings.Contains(plain, "▌ [lines") || !strings.Contains(plain, "func a() {}") {
			t.Errorf("%s", plain)
		}
	})

	t.Run("RC-06 a long line wraps into rows of the same width, the number on the first", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(50)
		output := "9\tvar long = \"" + strings.Repeat("abcdefghij", 12) + "\"\n"

		drawn := p.Tool(readRun("main.go", output, map[string]any{"numbered": true}), true)

		var widths []int
		var numbered int
		// The first is the path over the code.
		for _, line := range strings.Split(drawn, "\n")[3:] {
			if strings.Contains(line, "▌") {
				widths = append(widths, ansi.StringWidth(line))
				if strings.Contains(ansi.Strip(line), " 9 ") {
					numbered++
				}
			}
		}
		if len(widths) < 3 || numbered != 1 {
			t.Fatalf("rows %v numbered %d", widths, numbered)
		}
		for _, width := range widths {
			if width != widths[0] || width > 50 {
				t.Errorf("widths %v", widths)
				break
			}
		}
	})

	t.Run("RC-07 a very long file is coloured up to a limit and the rest is counted", func(t *testing.T) {
		p := newPrinter()
		var lines []string
		for i := range maxCodeLines + 50 {
			lines = append(lines, fmt.Sprintf("var v%d = %d", i, i))
		}

		plain := ansi.Strip(p.Tool(readRun("main.go", strings.Join(lines, "\n"), nil), true))

		if !strings.Contains(plain, fmt.Sprintf("… +%d lines", 50)) || !strings.Contains(plain, fmt.Sprintf("var v%d =", maxCodeLines-1)) || strings.Contains(plain, fmt.Sprintf("var v%d =", maxCodeLines)) {
			t.Errorf("tail:\n%s", plain[max(len(plain)-300, 0):])
		}
	})

	t.Run("RC-08 a tab is four spaces, as in a file written; blank lines stay", func(t *testing.T) {
		p := newPrinter()

		plain := ansi.Strip(p.Tool(readRun("main.go", goFile, nil), true))

		if !strings.Contains(plain, "▌     fmt.Println") || strings.Contains(plain, "\t") || strings.Count(plain, "▌") != 8 {
			t.Errorf("%s", plain)
		}
	})
}
