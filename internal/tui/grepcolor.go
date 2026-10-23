package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
)

// grepLine is a line of what grep gave under show lines: a content line as
// N: the matching text, or N- text around a match, and a file's name over its
// lines.
var grepLine = regexp.MustCompile(`^(\d+)([:-]) (.*)$`)

// grepCount is a line of what grep gave under show count: the file and how
// many lines match in it.
var grepCount = regexp.MustCompile(`^(.+): (\d+)$`)

// grepResult is what grep gave with the files in colour, the numbers of the
// lines dim, the text that matches the pattern marked and the lines around a
// match dim: the first lines folded, all of it wrapped open. It tells whether
// it was a result to draw so; an error or "no matches" are shown as any.
func (p *printer) grepResult(run toolRun, open bool) (string, bool) {
	if run.name != "grep" || !run.done || run.isError {
		return "", false
	}
	var args struct {
		Pattern    string
		IgnoreCase bool `json:"ignore_case"`
		Show       string
	}
	if json.Unmarshal(run.args, &args) != nil || strings.TrimSpace(run.output) == "no matches" {
		return "", false
	}
	pattern := args.Pattern
	if args.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, _ := regexp.Compile(pattern)
	lines := strings.Split(strings.TrimRight(terminalText(run.output), "\n"), "\n")
	hidden := 0
	if !open && len(lines) > toolPreviewLines {
		hidden, lines = len(lines)-toolPreviewLines, lines[:toolPreviewLines]
	}

	file := newSGR(lipgloss.NewStyle().Foreground(accent()).Bold(true))
	dim := newSGR(dimStyle)
	hit := newSGR(lipgloss.NewStyle().Foreground(warning()).Bold(true))
	var out []string
	row := 0
	emit := func(text string) {
		prefix := "    "
		if row == 0 {
			prefix = dim.wrap("  ⎿ ")
		}
		out = append(out, prefix+text)
		row++
	}
	for _, line := range lines {
		switch found := grepLine.FindStringSubmatch(line); {
		case found != nil:
			number, around, text := found[1], found[2], found[3]
			body := func(piece string) string {
				if around == "-" {
					return dim.wrap(piece)
				}
				return markMatches(piece, re, hit)
			}
			pieces := []string{text}
			if open {
				pieces = p.wrapped(text, 6+len(number)+2)
			}
			for i, piece := range pieces {
				lead := dim.wrap(number+around) + " "
				if i > 0 {
					lead = strings.Repeat(" ", len(number)+2)
				}
				emit(lead + body(p.clipTo(piece, open)))
			}
		case line == "--":
			emit(dim.wrap(line))
		case args.Show == "count" && grepCount.MatchString(line):
			found := grepCount.FindStringSubmatch(line)
			emit(file.wrap(found[1]) + dim.wrap(": "+found[2]))
		case strings.HasPrefix(line, "[stopped at"):
			emit(dim.wrap(line))
		default:
			emit(file.wrap(line))
		}
	}
	if hidden > 0 {
		out = append(out, dimStyle.Render(fmt.Sprintf("    … +%d lines", hidden)))
	}
	return strings.Join(out, "\n"), true
}

// clipTo is a piece of text as the folded view shows it, cut to the width, or
// whole when it is open and wrapped already.
func (p *printer) clipTo(piece string, open bool) string {
	if open {
		return piece
	}
	return p.clip(piece)
}

// markMatches is the text with what the pattern matches in the mark, the rest
// as it is.
func markMatches(text string, re *regexp.Regexp, mark sgr) string {
	if re == nil {
		return text
	}
	var out strings.Builder
	last := 0
	for _, found := range re.FindAllStringIndex(text, -1) {
		if found[1] == found[0] {
			continue
		}
		out.WriteString(text[last:found[0]])
		out.WriteString(mark.wrap(text[found[0]:found[1]]))
		last = found[1]
	}
	out.WriteString(text[last:])
	return out.String()
}
