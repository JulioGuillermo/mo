package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// terminalText is a command's output as a terminal would leave it on the
// screen, safe to draw: its own colours and moves gone, \r\n a new line,
// a line a \r went back over its last text, other controls left out but
// tabs.
func terminalText(text string) string {
	if !strings.ContainsFunc(text, func(r rune) bool { return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f }) {
		return text
	}
	text = strings.ReplaceAll(ansi.Strip(text), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.ContainsRune(line, '\r') {
			pieces := strings.Split(line, "\r")
			line = pieces[len(pieces)-1]
			for j := len(pieces) - 1; j >= 0 && line == ""; j-- {
				line = pieces[j]
			}
		}
		lines[i] = strings.Map(func(r rune) rune {
			if (r < 0x20 && r != '\t') || r == 0x7f {
				return -1
			}
			return r
		}, line)
	}
	return strings.Join(lines, "\n")
}
