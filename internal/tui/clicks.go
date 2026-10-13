package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// region is a part of a drawn block that does something when clicked: a
// row, or the columns from x0 up to x1 of it.
type region struct {
	y, x0, x1 int
	do        func(m model) (tea.Model, tea.Cmd)
}

func row(y int, do func(m model) (tea.Model, tea.Cmd)) region {
	return region{y: y, x0: 0, x1: 1 << 30, do: do}
}

func (r region) holds(x, y int) bool {
	return y == r.y && x >= r.x0 && x < r.x1
}

// shifted moves regions down by lines, for a block drawn under others.
func shifted(regions []region, lines int) []region {
	moved := make([]region, len(regions))
	for i, r := range regions {
		r.y += lines
		moved[i] = r
	}
	return moved
}

// click does what is under the pointer: something in the bottom rows, a job
// in the side panel, or opening a folded block of the conversation.
func (m model) click(x, y int) (tea.Model, tea.Cmd) {
	_, regions := m.screenLayout()
	for _, r := range regions {
		if r.holds(x, y) {
			return r.do(m)
		}
	}
	if y < m.conversationHeight() && x < m.mainWidth() && m.dialog == nil {
		m.clickDetails(y)
	}
	return m, nil
}

// clickSettings picks the option of a settings list that was clicked: it
// moves huh's cursor there and presses enter, as huh takes no mouse.
func (m model) clickSettings(y int) (tea.Model, tea.Cmd) {
	if _, isList := m.settings.form.GetFocusedField().(*huh.Select[string]); !isList {
		return m, nil
	}
	lines := strings.Split(ansi.Strip(m.settings.form.View()), "\n")
	if y < 0 || y >= len(lines) || !isOptionLine(lines[y]) {
		return m, nil
	}
	cursor := -1
	for i, line := range lines {
		if isOptionLine(line) && strings.HasPrefix(strings.TrimLeft(line, "┃│ "), ">") {
			cursor = i
		}
	}
	if cursor < 0 {
		return m, nil
	}
	key := tea.KeyPressMsg{Code: tea.KeyDown}
	if y < cursor {
		key = tea.KeyPressMsg{Code: tea.KeyUp}
	}
	var current tea.Model = m
	for range abs(y - cursor) {
		current, _ = current.(model).updateSettings(key)
	}
	return current.(model).updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// isOptionLine tells a list option from a title or a description: options
// start after the border with the cursor or with blank room for it.
func isOptionLine(line string) bool {
	rest, bordered := strings.CutPrefix(line, "┃")
	if !bordered {
		rest, bordered = strings.CutPrefix(line, "│")
	}
	if !bordered {
		return false
	}
	return strings.HasPrefix(rest, " > ") || (strings.HasPrefix(rest, "   ") && strings.TrimSpace(rest) != "")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
