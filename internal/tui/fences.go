package tui

import (
	"regexp"
	"strings"
)

// fenceLine is a line that opens or closes a code block: its indent, its
// fence of backticks or tildes, and what follows it.
var fenceLine = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")

// plainFences names the code blocks that name no language as text, so the
// highlighter leaves them as written instead of guessing a language.
func plainFences(markdown string) string {
	lines := strings.Split(markdown, "\n")
	open := ""
	for i, line := range lines {
		found := fenceLine.FindStringSubmatch(line)
		if found == nil {
			continue
		}
		indent, fence, rest := found[1], found[2], found[3]
		if open != "" {
			if fence[0] == open[0] && len(fence) >= len(open) && strings.TrimSpace(rest) == "" {
				open = ""
			}
			continue
		}
		open = fence
		if strings.TrimSpace(rest) == "" {
			lines[i] = indent + fence + "text"
		}
	}
	return strings.Join(lines, "\n")
}
