package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const btwBoxHelp = "↑↓ pgup pgdn scroll · y copy · esc closes"

// btwBox shows a side question's answer over the screen, live while it
// streams: top is the first row shown, and follow keeps the last in view.
type btwBox struct {
	answer *btwAnswer
	top    int
	follow bool
}

func (m model) openBtwBox(answer *btwAnswer) (tea.Model, tea.Cmd) {
	m.btwBox = &btwBox{answer: answer, follow: true}
	m.popup = nil
	return m, nil
}

// openLastBtw opens the box of the side question asked last.
func (m model) openLastBtw() (tea.Model, tea.Cmd) {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].btw != nil {
			return m.openBtwBox(m.blocks[i].btw)
		}
	}
	return m.emit(m.printer.Note("no /btw yet in this conversation"))
}

// btwBoxRows is how many rows of the answer the box shows.
func (m model) btwBoxRows() int {
	return max(m.height-6, 3)
}

// btwBoxLines are the rows of the answer shown now, scrolled.
func (m model) btwBoxLines() []string {
	box := m.btwBox
	body := m.printer.btwBody(box.answer)
	body = append(body, "", dimStyle.Render("  "+btwFooter(box.answer)))
	rows := m.btwBoxRows()
	last := max(len(body)-rows, 0)
	if box.follow {
		box.top = last
	}
	box.top = min(max(box.top, 0), last)
	return body[box.top:min(box.top+rows, len(body))]
}

// overBtwBox draws the box over the screen, in its middle.
func (m model) overBtwBox(screen string) string {
	box := m.btwBox
	width := max(min(m.printer.width+4, m.width-2), 20)
	inner := width - 4
	title := btwPill().Render(" "+btwWord(box.answer)+" ") + " " + ansi.Truncate(strings.Join(strings.Fields(box.answer.question), " "), inner-8, "…")
	if !box.answer.done {
		title += dimStyle.Render(" · answering…")
	}
	lines := []string{title, ""}
	for _, line := range m.btwBoxLines() {
		lines = append(lines, ansi.Truncate(line, inner, "…"))
	}
	lines = append(lines, "", dimStyle.Render(btwBoxHelp))
	drawn := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent()).Padding(0, 1).Width(width).
		Background(col(uiNow().popup)).Render(strings.Join(lines, "\n"))
	x, y := max((m.width-lipgloss.Width(drawn))/2, 0), max((m.height-lipgloss.Height(drawn))/2, 0)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(drawn).X(x).Y(y).Z(1),
	).Render()
}

// updateBtwBox scrolls the box, copies its answer or closes it; a click
// outside it closes it too.
func (m model) updateBtwBox(msg tea.Msg) (tea.Model, tea.Cmd) {
	box := m.btwBox
	page := max(m.btwBoxRows()-2, 1)
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			m.btwBox = nil
		case "up", "k":
			m.scrollBtwBox(-1)
		case "down", "j":
			m.scrollBtwBox(1)
		case "pgup", "b":
			m.scrollBtwBox(-page)
		case "pgdown", "space", "f":
			m.scrollBtwBox(page)
		case "home", "g":
			box.follow, box.top = false, 0
		case "end", "G":
			box.follow = true
		case "y":
			return m.flashCopied(box.answer.text, fmt.Sprintf("copied the answer, %s", btwLines(box.answer)))
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.scrollBtwBox(-3)
		} else {
			m.scrollBtwBox(3)
		}
	case tea.MouseClickMsg:
		m.btwBox = nil
	case tea.WindowSizeMsg:
		return m, m.resize(msg)
	}
	return m, nil
}

// scrollBtwBox moves the box by rows; reaching the end follows it again.
func (m model) scrollBtwBox(rows int) {
	box := m.btwBox
	shown := len(m.printer.btwBody(box.answer)) + 2
	last := max(shown-m.btwBoxRows(), 0)
	if box.follow {
		box.top = last
	}
	box.top = min(max(box.top+rows, 0), last)
	box.follow = box.top == last
}
