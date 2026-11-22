package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// inputRowAt is the row of the input that row y of the screen shows, if it
// shows one.
func (m model) inputRowAt(y int) (int, bool) {
	screen, _ := m.screenLayout()
	status, _ := m.statusLayout()
	input := m.shellInputView(m.input.View())
	top := ansiHeight(screen) - ansiHeight(status) - ansiHeight(input)
	if y < top || y >= top+ansiHeight(input) {
		return 0, false
	}
	return y - top, true
}

func ansiHeight(text string) int {
	return strings.Count(text, "\n") + 1
}

// placeCursor puts the input's cursor where row and column x of the input
// show, or at the end of the text on that row when x is past it.
func (m *model) placeCursor(row, x int) {
	target := m.input.ScrollYOffset() + row
	m.input.MoveToBegin()
	for range target {
		line, offset := m.input.Line(), m.input.LineInfo().RowOffset
		m.input.CursorDown()
		if m.input.Line() == line && m.input.LineInfo().RowOffset == offset {
			break
		}
	}
	info := m.input.LineInfo()
	text := []rune(strings.Split(m.input.Value(), "\n")[m.input.Line()])
	end := min(info.StartColumn+info.Width, len(text))
	if info.RowOffset+1 < info.Height {
		end--
	}
	column := info.StartColumn
	room := x - ansi.StringWidth(m.input.Prompt)
	for column < end && room > 0 {
		room -= ansi.StringWidth(string(text[column]))
		if room >= 0 {
			column++
		}
	}
	m.input.SetCursorColumn(column)
}
