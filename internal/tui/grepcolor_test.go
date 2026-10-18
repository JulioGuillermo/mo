package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | GR-01 | Happy    | A grep result has the file coloured, the numbers dim and the match marked | P0       |
// | GR-02 | Happy    | The lines around a match are dim and the match is not marked in them      | P1       |
// | GR-03 | Edge     | Folded it shows the first lines and counts the rest; open, all of them    | P0       |
// | GR-04 | Edge     | show files and show count colour the files and leave the counts dim       | P1       |
// | GR-05 | Edge     | No matches, an error, and a call still running are shown as any result    | P0       |
// | GR-06 | Edge     | ignore_case marks the match whatever its capitals                         | P1       |
// | GR-07 | Edge     | A long matching line wraps when open, the number on the first row only   | P2       |

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const (
	fileHue  = "122;162;247" // accent, the colour of a file
	matchHue = "224;175;104" // warning, what matches
)

func grepRun(output string, extra map[string]any) toolRun {
	fields := map[string]any{"pattern": "Println"}
	for key, value := range extra {
		fields[key] = value
	}
	args, _ := json.Marshal(fields)
	return toolRun{name: "grep", args: args, summary: "Println", output: output, done: true}
}

const grepOutput = "main.go\n5: fmt.Println(\"a\")\n9: fmt.Println(\"b\")\nutil/log.go\n12: Println(x)\n"

func TestGrepColour(t *testing.T) {
	t.Run("GR-01 a grep result has the file coloured, the numbers dim and the match marked", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Tool(grepRun(grepOutput, nil), true)
		plain := ansi.Strip(drawn)

		if !strings.Contains(plain, "⎿ main.go") || !strings.Contains(plain, "5: fmt.Println(\"a\")") || !strings.Contains(plain, "util/log.go") {
			t.Fatalf("%s", plain)
		}
		var fileRow, matchRow string
		for _, line := range strings.Split(drawn, "\n") {
			switch {
			case strings.Contains(ansi.Strip(line), "util/log.go"):
				fileRow = line
			case strings.Contains(ansi.Strip(line), "12: Println(x)"):
				matchRow = line
			}
		}
		if !strings.Contains(fileRow, fileHue) || strings.Contains(fileRow, matchHue) {
			t.Errorf("file row: %q", fileRow)
		}
		if !strings.Contains(matchRow, "\x1b[1;38;2;"+matchHue+"mPrintln") || strings.Contains(matchRow, fileHue) {
			t.Errorf("match row: %q", matchRow)
		}
	})

	t.Run("GR-02 the lines around a match are dim and the match is not marked in them", func(t *testing.T) {
		p := newPrinter()
		output := "main.go\n4- before Println here\n5: fmt.Println(\"a\")\n6- after\n"

		drawn := p.Tool(grepRun(output, map[string]any{"context": 1}), true)

		for _, line := range strings.Split(drawn, "\n") {
			plain := ansi.Strip(line)
			if strings.Contains(plain, "before Println") && strings.Contains(line, matchHue) {
				t.Errorf("the match was marked in a line around: %q", line)
			}
			if strings.Contains(plain, "5: fmt.Println") && !strings.Contains(line, matchHue) {
				t.Errorf("the match was not marked: %q", line)
			}
		}
		if !strings.Contains(ansi.Strip(drawn), "4- before Println here") || !strings.Contains(ansi.Strip(drawn), "6- after") {
			t.Errorf("%s", ansi.Strip(drawn))
		}
	})

	t.Run("GR-03 folded it shows the first lines and counts the rest; open, all of them", func(t *testing.T) {
		p := newPrinter()
		run := grepRun(grepOutput+"cmd/x.go\n3: Println(y)\n", nil)

		folded, open := ansi.Strip(p.Tool(run, false)), ansi.Strip(p.Tool(run, true))

		if !strings.Contains(folded, "… +3 lines") || strings.Contains(folded, "cmd/x.go") || !strings.Contains(folded, "ctrl+o or click") {
			t.Errorf("folded:\n%s", folded)
		}
		if !strings.Contains(open, "cmd/x.go") || !strings.Contains(open, "3: Println(y)") || strings.Contains(open, "… +") {
			t.Errorf("open:\n%s", open)
		}
	})

	t.Run("GR-04 show files and show count colour the files and leave the counts dim", func(t *testing.T) {
		p := newPrinter()

		files := p.Tool(grepRun("main.go\nutil/log.go\n", map[string]any{"show": "files"}), true)
		count := p.Tool(grepRun("main.go: 2\nutil/log.go: 1\n", map[string]any{"show": "count"}), true)

		for _, drawn := range []string{files, count} {
			for _, line := range strings.Split(drawn, "\n") {
				if strings.Contains(ansi.Strip(line), "util/log.go") && !strings.Contains(line, fileHue) {
					t.Errorf("a file not coloured: %q", line)
				}
			}
		}
		if !strings.Contains(ansi.Strip(count), "main.go: 2") {
			t.Errorf("%s", ansi.Strip(count))
		}
	})

	t.Run("GR-05 no matches, an error, and a call still running are shown as any result", func(t *testing.T) {
		p := newPrinter()
		failed := grepRun("error parsing regexp", nil)
		failed.isError = true
		running := toolRun{name: "grep", args: json.RawMessage(`{"pattern":"x"}`)}

		cases := map[string]struct {
			run  toolRun
			want string
		}{
			"none":    {grepRun("no matches", nil), "⎿ no matches"},
			"error":   {failed, "⎿ error parsing regexp"},
			"running": {running, "⎿ running…"},
		}
		for name, c := range cases {
			drawn := p.Tool(c.run, true)
			if _, isResult := p.grepResult(c.run, true); isResult || !strings.Contains(ansi.Strip(drawn), c.want) || strings.Contains(drawn, matchHue) {
				t.Errorf("%s was drawn as a result: %q", name, drawn)
			}
		}
	})

	t.Run("GR-06 ignore_case marks the match whatever its capitals", func(t *testing.T) {
		p := newPrinter()

		drawn := p.Tool(grepRun("main.go\n5: PRINTLN(x) and println(y)\n", map[string]any{"ignore_case": true}), true)

		if strings.Count(drawn, "\x1b[1;38;2;"+matchHue+"m") != 2 {
			t.Errorf("%q", drawn)
		}
	})

	t.Run("GR-07 a long matching line wraps when open, the number on the first row only", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(40)
		long := strings.Repeat("abcdefghij ", 8) + "Println"

		plain := ansi.Strip(p.Tool(grepRun("main.go\n7: "+long+"\n", nil), true))

		var rows []string
		for _, line := range strings.Split(plain, "\n") {
			if strings.Contains(line, "abcdefghij") {
				rows = append(rows, line)
			}
		}
		if len(rows) < 2 || !strings.Contains(rows[0], "7: ") || strings.Contains(rows[1], "7:") {
			t.Errorf("rows %q", rows)
		}
	})
}
