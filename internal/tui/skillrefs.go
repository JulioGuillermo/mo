package tui

import (
	"regexp"
	"strings"

	"github.com/jg/mo/internal/skills"
)

var skillRefPattern = regexp.MustCompile(`\$[A-Za-z0-9_-]+(?:[.:][A-Za-z0-9_-]+)*`)

// skillRef is a $name in a prompt that names a skill, at start:end.
type skillRef struct {
	name       string
	start, end int
}

// skillRefs finds the $name of skills in text: a whole word naming a skill
// found, outside code between backticks. $HOME, $5 or $x inside `code`
// are not.
func skillRefs(text string, found []skills.Skill) []skillRef {
	code := codeSpans(text)
	var refs []skillRef
	for _, match := range skillRefPattern.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]
		name := text[start+1 : end]
		if startsMidWord(text, start) || insideAny(start, code) {
			continue
		}
		if _, ok := skills.Named(found, name); ok {
			refs = append(refs, skillRef{name: name, start: start, end: end})
		}
	}
	return refs
}

// startsMidWord tells whether the $ at index follows a letter, digit or
// another $, as in a$b or $$x.
func startsMidWord(text string, index int) bool {
	if index == 0 {
		return false
	}
	before := text[index-1]
	isWord := before >= 'a' && before <= 'z' || before >= 'A' && before <= 'Z' || before >= '0' && before <= '9'
	return isWord || before == '_' || before == '$'
}

func insideAny(index int, spans [][]int) bool {
	for _, span := range spans {
		if index >= span[0] && index < span[1] {
			return true
		}
	}
	return false
}

// codeSpans are where text has code between backticks, ``` blocks and
// `inline` code, as start and end. A block left open runs to the end of
// the text, inline code left open to the end of its line.
func codeSpans(text string) [][]int {
	var spans [][]int
	at := 0
	for at < len(text) {
		if text[at] != '`' {
			at++
			continue
		}
		if strings.HasPrefix(text[at:], "```") {
			end := strings.Index(text[at+3:], "```")
			if end < 0 {
				return append(spans, []int{at, len(text) + 1})
			}
			next := at + 3 + end + 3
			spans = append(spans, []int{at, next})
			at = next
			continue
		}
		line := text[at+1:]
		if newline := strings.IndexByte(line, '\n'); newline >= 0 {
			line = line[:newline]
		}
		end := strings.IndexByte(line, '`')
		if end < 0 {
			lineEnd := at + 1 + len(line)
			if lineEnd == len(text) {
				lineEnd++
			}
			spans = append(spans, []int{at, lineEnd})
			at = at + 1 + len(line)
			continue
		}
		next := at + 1 + end + 1
		spans = append(spans, []int{at, next})
		at = next
	}
	return spans
}
