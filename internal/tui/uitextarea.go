package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

const (
	uiTextareaWidth  = 48
	uiTextareaHeight = 6
)

// textarea draws a textarea: its text coloured as code of its language,
// wrapped to its width, as many rows as its room asks, 6 unless it does.
// Focused, it shows its last rows and the cursor after the text; else its
// first rows, or its placeholder while empty.
func (r uiRenderer) textarea(n tools.UINode, room uiRoom) uiBox {
	w := min(uiTextareaWidth, room.width)
	if room.fillWidth {
		w = room.width
	}
	rows := uiTextareaHeight
	if room.fillHeight {
		rows = room.height
	}
	face := orDefault(n.BG, "surface")
	value := n.Current()
	focused := r.focus == n.ID

	var lines []string
	switch {
	case value == "" && !focused:
		lines = []string{uiStyle("dim", face, false).Render(ansi.Truncate(n.Placeholder, w, "…"))}
	case value == "":
		lines = []string{""}
	default:
		lines = highlightLines(value, n.Language, w, uiColor(face), true)
		if strings.HasSuffix(value, "\n") {
			lines = append(lines, "")
		}
	}
	if focused {
		cursor := uiStyle("accent", face, false).Render(uiCursor)
		last := len(lines) - 1
		if ansi.StringWidth(lines[last]) < w {
			lines[last] += cursor
		} else {
			lines = append(lines, cursor)
		}
	}
	if len(lines) > rows {
		if focused {
			lines = lines[len(lines)-rows:]
		} else {
			lines = lines[:rows]
		}
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = line + blank(w-ansi.StringWidth(line), face)
	}
	return uiBox{lines: lines, width: w, hits: []uiHit{{x1: w, y1: rows, id: n.ID}}}
}
