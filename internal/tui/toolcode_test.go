package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | BH-01 | Happy    | The command of a bash call is coloured as shell code, folded and open    | P0       |
// | BH-02 | Happy    | The command being written is coloured too                                | P1       |
// | BH-03 | Edge     | What bash shows that is not a command, and other tools, are not coloured | P0       |
// | BH-04 | Edge     | Every line of a coloured command fills the shade to the same width       | P1       |
// | BH-05 | Edge     | A long command folds to its first lines, a long line is cut, as it was   | P1       |
// | BH-06 | Happy    | An edit with several changes shows each as lines added and removed       | P0       |
// | BH-07 | Happy    | An edit with old and new, and a write, show as before                    | P0       |
// | BH-08 | Happy    | An answered ask shows every question with its answer, folded or open      | P0       |

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const (
	keywordColor = "187;154;247" // #bb9af7, a keyword in the dark palette
	stringColor  = "158;206;106" // #9ece6a, a string
	addedBg      = "18;48;31"    // #12301f, a line added
	removedBg    = "58;22;34"    // #3a1622, a line removed
)

const loop = `for f in *.go; do echo "$f"; done`

func bashRun(command, action string) toolRun {
	args, _ := json.Marshal(map[string]any{"command": command})
	if action != "" {
		args, _ = json.Marshal(map[string]any{"action": action, "id": 3})
	}
	return toolRun{name: "bash", args: args, summary: command, output: "ok", done: true}
}

func TestBashHighlight(t *testing.T) {
	t.Run("BH-01 the command of a bash call is coloured as shell code, folded and open", func(t *testing.T) {
		p := newPrinter()
		run := bashRun(loop, "")

		for name, drawn := range map[string]string{"folded": p.Tool(run, false), "open": p.Tool(run, true)} {
			if !strings.Contains(drawn, keywordColor) || !strings.Contains(drawn, stringColor) || !strings.Contains(ansi.Strip(drawn), loop) {
				t.Errorf("%s: %q", name, drawn)
			}
		}
	})

	t.Run("BH-02 the command being written is coloured too", func(t *testing.T) {
		m := conversationModel()

		drawn := m.formingCallView("bash", `{"command":"`+strings.ReplaceAll(loop, `"`, `\"`)+`"}`, markRunning)

		if !strings.Contains(drawn, keywordColor) || !strings.Contains(ansi.Strip(drawn), loop) {
			t.Errorf("%q", drawn)
		}
	})

	t.Run("BH-03 what bash shows that is not a command, and other tools, are not coloured", func(t *testing.T) {
		p := newPrinter()
		output := p.Tool(bashRun("job 3 output", "output"), true)
		other := p.Tool(toolRun{name: "web_search", args: json.RawMessage(`{"query":"x"}`), summary: loop + "\n" + loop, output: "ok", done: true}, true)

		if strings.Contains(output, keywordColor) || strings.Contains(other, keywordColor) {
			t.Errorf("output %q other %q", output, other)
		}
	})

	t.Run("BH-04 every line of a coloured command fills the shade to the same width", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(60)
		command := "go test ./...\n" + strings.Repeat("echo a very long line of a script ", 4)

		for _, open := range []bool{false, true} {
			var widths []int
			for _, line := range strings.Split(p.Tool(bashRun(command, ""), open), "\n") {
				if strings.Contains(line, "▌") {
					widths = append(widths, ansi.StringWidth(line))
				}
			}
			for _, width := range widths {
				if width != widths[0] || width > 60 {
					t.Errorf("open %v: widths %v", open, widths)
					break
				}
			}
			if len(widths) < 2 {
				t.Errorf("open %v: widths %v", open, widths)
			}
		}
	})

	t.Run("BH-05 a long command folds to its first lines, a long line is cut, as it was", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(50)
		var lines []string
		for i := range 10 {
			lines = append(lines, "echo line "+strings.Repeat("x", 5*i))
		}

		folded := ansi.Strip(p.Tool(bashRun(strings.Join(lines, "\n"), ""), false))

		if !strings.Contains(folded, "… +4 lines") || strings.Contains(folded, "echo line xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx") || !strings.Contains(folded, "…") {
			t.Errorf("%s", folded)
		}
	})
}

func TestEditDiff(t *testing.T) {
	t.Run("BH-06 an edit with several changes shows each as lines added and removed", func(t *testing.T) {
		p := newPrinter()
		args := json.RawMessage(`{"path":"a.go","edits":[{"old":"a := 1","new":"a := 2"},{"old":"b := 1\nc := 1","new":"b := 3"}]}`)
		run := toolRun{name: "edit", args: args, summary: "a.go", output: "edited", done: true}

		folded, open := ansi.Strip(p.Tool(run, false)), p.Tool(run, true)

		if !strings.Contains(folded, "+2 −3") || !strings.Contains(folded, "ctrl+o or click") {
			t.Errorf("folded:\n%s", folded)
		}
		for _, want := range []string{"a := 1", "a := 2", "b := 1", "c := 1", "b := 3", addedBg, removedBg} {
			if !strings.Contains(open, want) && !strings.Contains(ansi.Strip(open), want) {
				t.Errorf("open misses %q:\n%s", want, ansi.Strip(open))
			}
		}
	})

	t.Run("BH-07 an edit with old and new, and a write, show as before", func(t *testing.T) {
		p := newPrinter()
		edit := toolRun{name: "edit", args: json.RawMessage(`{"path":"a.go","old":"x := 1","new":"x := 2"}`), summary: "a.go", output: "edited", done: true}
		write := toolRun{name: "write", args: json.RawMessage(`{"path":"a.go","content":"package a\nvar x = 1\n"}`), summary: "a.go", output: "wrote", done: true}

		edited, wrote := p.Tool(edit, true), ansi.Strip(p.Tool(write, true))

		if !strings.Contains(edited, addedBg) || !strings.Contains(edited, removedBg) || !strings.Contains(ansi.Strip(p.Tool(edit, false)), "+1 −1") {
			t.Errorf("edit:\n%s", ansi.Strip(edited))
		}
		if !strings.Contains(wrote, "package a") || !strings.Contains(wrote, "2 lines") {
			t.Errorf("write:\n%s", wrote)
		}
	})

	t.Run("BH-08 an answered ask shows every question with its answer, folded or open", func(t *testing.T) {
		p := newPrinter()
		run := toolRun{name: "ask", done: true, output: "Q: Which db?\nA: Postgres\n\nQ: Port?\nA: 5432"}

		for _, open := range []bool{false, true} {
			shown := ansi.Strip(p.Tool(run, open))

			if !strings.Contains(shown, "? Which db?\n    › Postgres\n  ? Port?\n    › 5432") || strings.Contains(shown, "ctrl+o") {
				t.Errorf("open %v:\n%s", open, shown)
			}
		}
	})
}
