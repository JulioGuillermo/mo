package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// selectKey picks lines with the keyboard where the viewer takes comments:
// v starts a selection at the cursor, or ends it, and shift+↑↓ grow it as
// the arrows do once it is started. It tells whether the key was one.
func (m model) selectKey(pressed string) bool {
	v := m.viewer
	if !v.annotatable() {
		return false
	}
	switch pressed {
	case "v":
		if v.selecting {
			v.selecting, v.selected = false, false
			return true
		}
		v.selecting, v.selected, v.pickFrom, v.pickTo = true, true, v.cursor, v.cursor
	case "shift+up", "shift+down":
		if !v.selecting && !v.selected {
			v.selected, v.pickFrom = true, v.cursor
		}
		m.extendSelection(map[string]int{"shift+up": -1, "shift+down": 1}[pressed])
	case "up", "k", "down", "j":
		if !v.selecting {
			v.selected = false
			return false
		}
		m.extendSelection(map[string]int{"up": -1, "k": -1, "down": 1, "j": 1}[pressed])
	case "esc":
		if !v.selecting && !v.selected {
			return false
		}
		v.selecting, v.selected = false, false
	default:
		return false
	}
	return true
}

func (m model) extendSelection(by int) {
	v := m.viewer
	m.moveCursor(by)
	v.pickTo = v.cursor
}

// selectedLines are the first and last lines selected, by keys or dragged
// over.
func (v *outputViewer) selectedLines() (from, to int, ok bool) {
	if !v.selected && !v.picking {
		return 0, 0, false
	}
	return min(v.pickFrom, v.pickTo), max(v.pickFrom, v.pickTo), true
}

// copySelection copies the lines selected, or every line without them.
func (m model) copySelection() tea.Cmd {
	v := m.viewer
	from, to, ok := v.selectedLines()
	if !ok || len(v.lines) == 0 {
		return m.copyViewer(strings.Join(v.lines, "\n"), fmt.Sprintf("copied all %d lines", len(v.lines)))
	}
	to = min(to, len(v.lines)-1)
	return m.copyViewer(strings.Join(v.lines[from:to+1], "\n"), fmt.Sprintf("copied lines %d-%d", from+1, to+1))
}
