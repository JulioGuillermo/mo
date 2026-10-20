package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

const (
	uiCanvasWidth  = 40
	uiCanvasHeight = 10
)

// canvas draws a plugin's canvas: the lines its code gave for this size,
// each cut to the width and the rows filled to the height. Before any are
// drawn, it is blank.
func (r uiRenderer) canvas(n tools.UINode, room uiRoom, bg string) uiBox {
	width, height := min(uiCanvasWidth, room.width), uiCanvasHeight
	if room.fillWidth {
		width = room.width
	}
	if room.fillHeight {
		height = room.height
	}
	var drawn []string
	if r.canvasLines != nil {
		drawn = r.canvasLines(n.ID, width, height)
	}
	lines := make([]string, height)
	for i := range lines {
		line := ""
		if i < len(drawn) {
			line = ansi.Truncate(drawn[i], width, "")
		}
		lines[i] = line + blank(width-ansi.StringWidth(line), bg)
	}
	return uiBox{lines: lines, width: width, hits: []uiHit{{x1: width, y1: height, id: n.ID}}}
}

// canvasKey gives a key to the focused canvas: every key but ctrl+c, and
// esc only when it keeps esc.
func (m model) canvasKey(ui tools.UI, node tools.UINode, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" || key == "esc" && !node.KeepEsc {
		return m.hideUI(ui)
	}
	m.dialogUIs().Send(tools.UIEvent{UI: ui.ID, Element: node.ID, Event: "key", Value: key, Text: msg.Text})
	return m, nil
}

// canvasMouse gives a canvas a click or the wheel where it happened, in
// its own cells counted from 0.
func (m model) canvasMouse(id int, node tools.UINode, hit uiHit, event, value string, x, y int) (tea.Model, tea.Cmd) {
	m.dialogUIs().Send(tools.UIEvent{
		UI: id, Element: node.ID, Event: event, Value: value,
		X: float64(x - hit.x0), Y: float64(y - hit.y0), At: true,
	})
	return m, nil
}

// canvasFocus tells the canvases the focus leaves and reaches.
func (m model) canvasFocus(ui tools.UI, from, to string) {
	shown := ui.Shown()
	for event, id := range map[string]string{"blur": from, "focus": to} {
		if node, ok := findNode(shown, id); ok && node.Type == tools.UICanvas {
			m.dialogUIs().Send(tools.UIEvent{UI: ui.ID, Element: id, Event: event})
		}
	}
}
