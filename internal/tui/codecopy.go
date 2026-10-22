package tui

import (
	"slices"
	"strings"
	"unicode"
)

// mostCodeBlocks is how many code blocks of answers the printer keeps, the
// latest, to copy them as written.
const mostCodeBlocks = 500

// keepCodeBlocks keeps the code blocks of an answer as the model wrote them.
func (p *printer) keepCodeBlocks(markdown string) {
	blocks := fencedBlocks(markdown)
	if len(blocks) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, block := range blocks {
		p.codeBlocks = slices.DeleteFunc(p.codeBlocks, func(kept string) bool { return kept == block })
		p.codeBlocks = append(p.codeBlocks, block)
	}
	if extra := len(p.codeBlocks) - mostCodeBlocks; extra > 0 {
		p.codeBlocks = slices.Clone(p.codeBlocks[extra:])
	}
}

// fencedBlocks are the contents of the ``` and ~~~ blocks of markdown, the
// fence's own indent taken off each line, as in a list.
func fencedBlocks(markdown string) []string {
	var blocks, lines []string
	fence, indent := "", ""
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if fence == "" {
			if marker := fenceMarker(trimmed); marker != "" {
				fence, indent, lines = marker, line[:len(line)-len(trimmed)], nil
			}
			continue
		}
		if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(strings.TrimLeft(trimmed, fence[:1])) == "" {
			blocks = append(blocks, strings.Join(lines, "\n"))
			fence = ""
			continue
		}
		lines = append(lines, strings.TrimPrefix(line, indent))
	}
	return blocks
}

// fenceMarker is the run of ` or ~ a fence opens with, three or more.
func fenceMarker(line string) string {
	for _, mark := range []string{"`", "~"} {
		run := len(line) - len(strings.TrimLeft(line, mark))
		if run >= 3 {
			return line[:run]
		}
	}
	return ""
}

// asWritten is the text of a selection as the model wrote it, when it lies
// inside one code block: the screen wraps long lines and indents them, and
// both are taken off. One of several lines, from the start of the first,
// keeps its indent. Anything else is left as the screen shows it.
func (p *printer) asWritten(selected string) string {
	wanted := []rune(withoutSpaces(selected))
	if len(wanted) == 0 {
		return selected
	}
	p.mu.Lock()
	blocks := slices.Clone(p.codeBlocks)
	p.mu.Unlock()
	for i := len(blocks) - 1; i >= 0; i-- {
		if found, ok := within(blocks[i], wanted); ok {
			return found
		}
	}
	return selected
}

// within finds wanted, a text without its spaces, in block, and gives that
// part of block with its spaces as written.
func within(block string, wanted []rune) (string, bool) {
	source := []rune(block)
	var compact []rune
	var at []int
	for i, character := range source {
		if !unicode.IsSpace(character) {
			compact = append(compact, character)
			at = append(at, i)
		}
	}
	start := indexRunes(compact, wanted)
	if start < 0 {
		return "", false
	}
	from, to := at[start], at[start+len(wanted)-1]+1
	if !slices.Contains(source[from:to], '\n') {
		return string(source[from:to]), true
	}
	lineStart := from
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	if strings.TrimSpace(string(source[lineStart:from])) == "" {
		from = lineStart
	}
	return string(source[from:to]), true
}

func indexRunes(text, part []rune) int {
	for start := 0; start+len(part) <= len(text); start++ {
		if slices.Equal(text[start:start+len(part)], part) {
			return start
		}
	}
	return -1
}

func withoutSpaces(text string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, text)
}
