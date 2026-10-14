package tui

// Test Traceability Table
// | ID    | Category | Description                                                             | Priority |
// | PP-01 | Happy    | A right click opens a small menu by the pointer, the screen staying under it | P0  |
// | PP-02 | Happy    | A click on an option takes it; a click outside or esc closes the menu    | P0       |
// | PP-03 | Edge     | Near the bottom right the menu moves up and left to fit; from keys it is centred | P1 |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

func popupModel(t *testing.T) model {
	t.Helper()
	m := sidebarModel(t, 140)
	updated, _ := m.emit("a line of the conversation that stays visible")
	return updated.(model)
}

func TestPopup(t *testing.T) {
	options := []huh.Option[string]{huh.NewOption("first thing", "first"), huh.NewOption("second thing", "second")}
	chosen := ""
	do := func(m model, choice string) (tea.Model, tea.Cmd) {
		chosen = choice
		return m, nil
	}

	t.Run("PP-01 a right click opens a small menu by the pointer, the screen staying under it", func(t *testing.T) {
		m := popupModel(t)
		m.clickedAt, m.clicked = point{10, 3}, true
		opened, _ := m.showChoices("Things", "what to do", options, do)
		m = opened.(model)

		screen := ansi.Strip(m.View().Content)
		box := m.popupLayout()
		if m.settings != nil || box.x != 10 || box.y != 4 || !strings.Contains(screen, "first thing") || !strings.Contains(screen, "Jobs") {
			t.Errorf("at %d,%d screen:\n%s", box.x, box.y, screen)
		}
	})

	t.Run("PP-02 a click on an option takes it; a click outside or esc closes the menu", func(t *testing.T) {
		m := popupModel(t)
		m.clickedAt, m.clicked = point{10, 3}, true
		opened, _ := m.showChoices("Things", "", options, do)
		m = opened.(model)
		box := m.popupLayout()
		var second int
		for row, i := range box.options {
			if i == 1 {
				second = row
			}
		}

		taken, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: box.x + 3, Y: box.y + second, Button: tea.MouseLeft}))
		outside, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 130, Y: 1, Button: tea.MouseLeft}))
		escaped, _ := m.Update(keyEsc)

		if chosen != "second" || taken.(model).popup != nil {
			t.Errorf("chosen = %q", chosen)
		}
		if outside.(model).popup != nil || escaped.(model).popup != nil {
			t.Error("a click outside or esc should close it")
		}
	})

	t.Run("PP-03 near the bottom right the menu moves up and left to fit; from keys it is centred", func(t *testing.T) {
		m := popupModel(t)
		m.clickedAt, m.clicked = point{138, 28}, true
		corner, _ := m.showChoices("Things", "", options, do)
		centred, _ := popupModel(t).showChoices("Things", "", options, do)

		box := corner.(model).popupLayout()
		width := ansi.StringWidth(box.lines[0])
		if box.x+width > 140 || box.y+len(box.lines) > 30 || box.y >= 28 {
			t.Errorf("corner box at %d,%d size %dx%d", box.x, box.y, width, len(box.lines))
		}
		middle := centred.(model).popupLayout()
		if middle.x < 40 || middle.y < 5 {
			t.Errorf("centred box at %d,%d", middle.x, middle.y)
		}
	})
}
