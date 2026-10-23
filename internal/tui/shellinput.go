package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const inputPromptWidth = 2

// typedShell is the command the input holds after ! or !!, and the marks
// before it.
func typedShell(value string) (marks, command string, ok bool) {
	command, ok = strings.CutPrefix(value, "!")
	if !ok {
		return "", "", false
	}
	marks = "!"
	if rest, twice := strings.CutPrefix(command, "!"); twice {
		marks, command = "!!", rest
	}
	return marks, command, true
}

// shellInputView is the input drawn with the command after ! coloured as
// code; anything else, or text being selected, as the input draws it.
func (m model) shellInputView(view string) string {
	marks, command, ok := typedShell(m.input.Value())
	if !ok || m.input.HasSelection() {
		return view
	}
	dark := m.printer == nil || m.printer.isDark()
	background := panel(dark).GetBackground()
	colors := syntaxPalette(dark)
	var styles []lipgloss.Style
	for range marks {
		styles = append(styles, colors.mark.on(background))
	}
	for _, token := range shellTokens(command) {
		for range []rune(token.Value) {
			styles = append(styles, colors.of(token.Type).on(background))
		}
	}
	value := []rune(m.input.Value())
	cursorX, cursorY := -1, -1
	probe := m.input
	probe.SetVirtualCursor(false)
	if cursor := probe.Cursor(); cursor != nil {
		cursorX, cursorY = cursor.X, cursor.Y
	}
	lines := strings.Split(view, "\n")
	next := 0
	for y, line := range lines {
		width := ansi.StringWidth(line)
		text := strings.TrimRight(ansi.Strip(ansi.Cut(line, inputPromptWidth, width)), " ")
		at := runeIndex(value, []rune(text), next)
		if text != "" && at < 0 {
			return view
		}
		var painted strings.Builder
		painted.WriteString(ansi.Cut(line, 0, inputPromptWidth))
		column := inputPromptWidth
		for i, character := range []rune(text) {
			style := styles[min(at+i, len(styles)-1)]
			if y == cursorY && column == cursorX {
				style = style.Reverse(true)
			}
			painted.WriteString(style.Render(string(character)))
			column += max(ansi.StringWidth(string(character)), 1)
		}
		fill := lipgloss.NewStyle().Background(background)
		if y == cursorY && cursorX >= column {
			painted.WriteString(fill.Render(strings.Repeat(" ", cursorX-column)) + fill.Reverse(true).Render(" "))
			column = cursorX + 1
		}
		painted.WriteString(fill.Render(strings.Repeat(" ", max(width-column, 0))))
		lines[y] = painted.String()
		if text != "" {
			next = at + len([]rune(text))
		}
	}
	return strings.Join(lines, "\n")
}

// runeIndex is where part is in text from from on, or -1.
func runeIndex(text, part []rune, from int) int {
	if len(part) == 0 {
		return from
	}
	for start := from; start+len(part) <= len(text); start++ {
		if string(text[start:start+len(part)]) == string(part) {
			return start
		}
	}
	return -1
}
