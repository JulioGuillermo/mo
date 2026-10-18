package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// maxCodeLines is how many lines of a file read shows coloured; the rest is
// said, not drawn, so a very long file does not slow the screen.
const maxCodeLines = 3000

// readTrailer is the line read ends a part of a file with, saying where it
// stopped.
var readTrailer = regexp.MustCompile(`^\[(matching )?lines \d+-\d+ of \d+.*\]$`)

// readCode is what read gave, coloured by the language of its file and set on
// the shade of a file, with the numbers of the lines when it gave them: the
// first lines folded, all of it open. It tells whether it is code to draw so:
// an error, an empty file, a file whose language is not known, or what read
// says of a part that is not there are shown as any result.
func (p *printer) readCode(run toolRun, open bool) (string, bool) {
	if run.name != "read" || !run.done || run.isError {
		return "", false
	}
	var args struct {
		Path     string
		Numbered bool
		Match    string
	}
	if json.Unmarshal(run.args, &args) != nil {
		return "", false
	}
	language := languageOf(args.Path)
	if language == "" {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(terminalText(run.output), "\n"), "\n")
	trailer := ""
	if last := lines[len(lines)-1]; len(lines) > 1 && readTrailer.MatchString(last) {
		trailer, lines = last, lines[:len(lines)-1]
	}
	if len(lines) == 1 && (lines[0] == "(empty file)" || lines[0] == "" || strings.HasPrefix(lines[0], "offset ") || strings.HasPrefix(lines[0], "no line matches")) {
		return "", false
	}
	numbered := args.Numbered || args.Match != ""
	numbers := make([]string, len(lines))
	if numbered {
		for i, line := range lines {
			number, code, found := strings.Cut(line, "\t")
			if _, err := strconv.Atoi(number); err != nil || !found {
				numbered = false
				break
			}
			numbers[i], lines[i] = number, code
		}
	}

	hidden := 0
	if shown := toolPreviewLines; !open && len(lines) > shown {
		hidden, lines = len(lines)-shown, lines[:shown]
	} else if open && len(lines) > maxCodeLines {
		hidden, lines = len(lines)-maxCodeLines, lines[:maxCodeLines]
	}

	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	colors := changePalette(dark)
	gutter := 0
	if numbered {
		for _, number := range numbers {
			gutter = max(gutter, len(number))
		}
		gutter++
	}
	inner := max(width-9-gutter, 20)

	// Each line is cut into the pieces that fit, so a piece is a row of the
	// screen, and the rows are coloured as one text: what a string or a comment
	// spans on its lines is coloured as it is.
	var rows []string
	var first []bool
	var shownNumbers []string
	for i, line := range lines {
		pieces := strings.Split(ansi.Hardwrap(strings.ReplaceAll(ansi.Strip(line), "\t", "    "), inner, true), "\n")
		for j, piece := range pieces {
			rows, first = append(rows, piece), append(first, j == 0)
			shownNumbers = append(shownNumbers, numbers[i])
		}
	}
	block := lipgloss.NewStyle().Background(colors.block)
	number := block.Foreground(colors.hunk)
	highlighted := highlightLines(strings.Join(rows, "\n"), language, 1<<20, colors.block, dark)
	var out []string
	for i, row := range highlighted {
		if i >= len(rows) {
			break
		}
		lead := block.Render(" ")
		if gutter > 0 {
			label := strings.Repeat(" ", gutter)
			if first[i] {
				label = fmt.Sprintf("%*s ", gutter-1, shownNumbers[i])
			}
			lead += number.Render(label)
		}
		out = append(out, commandEdge.Render("  ▌")+lead+row+codeFill(block, 1+gutter+ansi.StringWidth(row), width))
	}
	if hidden > 0 {
		out = append(out, dimStyle.Render(fmt.Sprintf("    … +%d lines", hidden)))
	}
	if trailer != "" {
		out = append(out, dimStyle.Render("    "+trailer))
	}
	return strings.Join(out, "\n"), true
}

// codeFill is the shade after a line of code, to the width of the block.
func codeFill(shade lipgloss.Style, used, width int) string {
	return shade.Render(strings.Repeat(" ", max(width-6-used, 0)))
}
