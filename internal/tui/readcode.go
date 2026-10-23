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

// readTrailer is the line read ends a part of a file with, saying where it
// stopped.
var readTrailer = regexp.MustCompile(`^\[(matching )?lines \d+-\d+ of \d+.*\]$`)

// readCode is what read gave, coloured by the language of its file and set on
// the shade of a file, with the numbers of the lines when it gave them: the
// first lines folded, all of it open, painted as the screen needs them when
// there are many. It gives the text, the function that paints rows in view
// when the rows are plain in the text, and how many rows there are; and it
// tells whether it is code to draw so: an error, an empty file, a file whose
// language is not known, or what read says of a part that is not there are
// shown as any result.
func (p *printer) readCode(run toolRun, open bool) (string, func(from, to int) []string, int, bool) {
	if run.name != "read" || !run.done || run.isError {
		return "", nil, 0, false
	}
	var args struct {
		Path     string
		Numbered bool
		Match    string
	}
	if json.Unmarshal(run.args, &args) != nil {
		return "", nil, 0, false
	}
	language := languageOf(args.Path)
	if language == "" {
		return "", nil, 0, false
	}
	lines := strings.Split(strings.TrimRight(terminalText(run.output), "\n"), "\n")
	trailer := ""
	if last := lines[len(lines)-1]; len(lines) > 1 && readTrailer.MatchString(last) {
		trailer, lines = last, lines[:len(lines)-1]
	}
	if len(lines) == 1 && (lines[0] == "(empty file)" || lines[0] == "" || strings.HasPrefix(lines[0], "offset ") || strings.HasPrefix(lines[0], "no line matches")) {
		return "", nil, 0, false
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
	if !open && len(lines) > toolPreviewLines {
		hidden, lines = len(lines)-toolPreviewLines, lines[:toolPreviewLines]
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
	expanded := make([]string, len(lines))
	for i, line := range lines {
		expanded[i] = strings.ReplaceAll(ansi.Strip(line), "\t", "    ")
	}
	code := newCodeLines(expanded, language, inner)

	block := lipgloss.NewStyle().Background(colors.block)
	edge, lead, fill, number := commandEdge().Render("  ▌"), block.Render(" "), newSGR(block), newSGR(block.Foreground(colors.hunk))
	label := func(row int) string {
		if gutter == 0 {
			return ""
		}
		if span := code.rows[row]; span.from == 0 {
			return fmt.Sprintf("%*s ", gutter-1, numbers[span.line])
		}
		return strings.Repeat(" ", gutter)
	}
	text, paint := codeBlock(code, colors.block, dark, func(row int, piece string) string {
		used := 1 + gutter + ansi.StringWidth(piece)
		return edge + lead + number.wrap(label(row)) + piece + fill.wrap(strings.Repeat(" ", max(width-6-used, 0)))
	})
	out := []string{text}
	if hidden > 0 {
		out = append(out, dimStyle.Render(fmt.Sprintf("    … +%d lines", hidden)))
	}
	if trailer != "" {
		out = append(out, dimStyle.Render("    "+trailer))
	}
	return strings.Join(out, "\n"), paint, len(code.rows), true
}
