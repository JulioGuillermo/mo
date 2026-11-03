package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	keysDialogMaxWidth = 84
	keysColumnMost     = 26
	keysDialogHelp     = "type to search · ↑↓ pgup pgdown scroll · esc close"
)

// keysDialog is the keys of the view it was opened over, in a box at the
// middle of the screen: typing narrows them, the arrows scroll them.
type keysDialog struct {
	title  string
	hints  []keyHint
	filter string
	top    int
}

// keysBox is the dialog drawn, and where it goes on the screen.
type keysBox struct {
	lines []string
	x, y  int
	// rows is how many lines of keys fit, total how many there are.
	rows, total int
}

// opensKeys tells whether a key opens the keys of the view: f1 always, ?
// when it would not be typed.
func (m model) opensKeys(msg tea.KeyPressMsg) bool {
	if m.settings != nil || m.popup != nil || m.uiOpen != 0 || m.keys != nil {
		return false
	}
	switch msg.String() {
	case "f1":
		return true
	case "?":
		return !m.writing()
	}
	return false
}

// writing tells whether the keys go to some text being typed.
func (m model) writing() bool {
	if v := m.viewer; v != nil {
		switch {
		case v.commenting, v.editing, v.typing:
			return true
		case m.treeShown() && v.treeFocus:
			return v.tree.typing
		case m.commitListShown() && v.history.focus:
			return v.history.typing
		}
		return false
	}
	if m.agentView == nil && m.leftTreeFocused() {
		return m.activeLeftHalf().tree.typing
	}
	return true
}

func (m model) openKeys() (tea.Model, tea.Cmd) {
	title, hints := m.shownKeys()
	m.keys = &keysDialog{title: title, hints: hints}
	return m, nil
}

// updateKeys is the dialog taking keys and the mouse.
func (m model) updateKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	k := m.keys
	box := m.keysLayout()
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			if k.filter != "" {
				k.filter, k.top = "", 0
				return m, nil
			}
			m.keys = nil
		case "f1", "ctrl+c":
			m.keys = nil
		case "up":
			k.scroll(-1, box)
		case "down":
			k.scroll(1, box)
		case "pgup":
			k.scroll(-box.rows, box)
		case "pgdown":
			k.scroll(box.rows, box)
		case "home":
			k.top = 0
		case "end":
			k.scroll(box.total, box)
		case "backspace":
			if runes := []rune(k.filter); len(runes) > 0 {
				k.filter, k.top = string(runes[:len(runes)-1]), 0
			}
		default:
			if msg.Text != "" {
				k.filter, k.top = k.filter+msg.Text, 0
			}
		}
	case tea.MouseWheelMsg:
		by := 3
		if msg.Button == tea.MouseWheelUp {
			by = -3
		}
		k.scroll(by, box)
	case tea.MouseClickMsg:
		inside := msg.X >= box.x && msg.X < box.x+ansi.StringWidth(box.lines[0]) && msg.Y >= box.y && msg.Y < box.y+len(box.lines)
		if !inside {
			m.keys = nil
		}
	}
	return m, nil
}

// scroll moves the lines in view by some, within those the box has.
func (k *keysDialog) scroll(by int, box keysBox) {
	k.top = min(max(k.top+by, 0), max(box.total-box.rows, 0))
}

// matching are the hints that hold the search in their keys or in what
// they do, capitals not mattering.
func (k *keysDialog) matching() []keyHint {
	search := strings.ToLower(strings.TrimSpace(k.filter))
	if search == "" {
		return k.hints
	}
	var found []keyHint
	for _, hint := range k.hints {
		if strings.Contains(strings.ToLower(hint.keys+" "+hint.does), search) {
			found = append(found, hint)
		}
	}
	return found
}

// lines are the hints that match, width wide: the keys in a column, what
// they do beside them, wrapped.
func (k *keysDialog) lines(width int) []string {
	hints := k.matching()
	column := 0
	for _, hint := range hints {
		column = max(column, ansi.StringWidth(hint.keys))
	}
	column = min(column, keysColumnMost, width/2)
	room := max(width-column-2, 10)
	var out []string
	for _, hint := range hints {
		keys := strings.Split(ansi.Wrap(hint.keys, column, " ,"), "\n")
		does := strings.Split(ansi.Wrap(hint.does, room, " "), "\n")
		for i := range max(len(keys), len(does)) {
			var key, what string
			if i < len(keys) {
				key = keys[i]
			}
			if i < len(does) {
				what = does[i]
			}
			out = append(out, keyStyle().Render(padTo(key, column))+"  "+what)
		}
	}
	if len(out) == 0 {
		out = append(out, dimStyle.Render("no key matches "+k.filter))
	}
	return out
}

// keysLayout draws the dialog at the middle of the screen, as many lines
// of keys as fit, scrolled as it was left.
func (m model) keysLayout() keysBox {
	k := m.keys
	width := min(keysDialogMaxWidth, max(m.width-4, 20))
	inner := width - 4
	lines := k.lines(inner)
	rows := min(len(lines), max(m.height-2-5, 1))
	top := min(k.top, max(len(lines)-rows, 0))

	search := dimStyle.Render("search…")
	if k.filter != "" {
		search = k.filter + "▏"
	}
	body := []string{
		toolName().Render("Keys") + dimStyle.Render(" · "+k.title),
		keyStyle().Render("/ ") + search,
	}
	body = append(body, lines[top:top+rows]...)
	footer := keysDialogHelp
	if rows < len(lines) {
		footer = fmt.Sprintf("%d-%d of %d · %s", top+1, top+rows, len(lines), footer)
	}
	body = append(body, dimStyle.Render(ansi.Truncate(footer, inner, "…")))
	for i, line := range body {
		body[i] = padTo(line, inner)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(popupBorder()).Padding(0, 1).
		Background(col(uiNow().popup)).Render(strings.Join(body, "\n"))
	drawn := strings.Split(box, "\n")
	x := max((m.width-ansi.StringWidth(drawn[0]))/2, 0)
	y := max((m.height-len(drawn))/2, 0)
	return keysBox{lines: drawn, x: x, y: y, rows: rows, total: len(lines)}
}

// overKeys draws the dialog over the screen.
func (m model) overKeys(screen string) string {
	box := m.keysLayout()
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(strings.Join(box.lines, "\n")).X(box.x).Y(box.y).Z(2),
	).Render()
}
