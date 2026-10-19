package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	popupMaxWidth       = 96
	popupMinWidth       = 24
	popupDescriptionMax = 12
)

var (
	popupBorder   = lipgloss.Color("#7aa2f7")
	popupError    = lipgloss.Color("#f7768e")
	popupSelected = lipgloss.NewStyle().Background(lipgloss.Color("#7aa2f7")).Foreground(lipgloss.Color("#1a1b26")).Bold(true)
)

// popup is a small menu drawn over the screen, where a right click
// opened it or in the middle: what can be done with something, the
// screen staying as it was under it.
type popup struct {
	title       string
	description string
	options     []huh.Option[string]
	do          func(m model, choice string) (tea.Model, tea.Cmd)
	selected    int
	// top is the first option shown when they do not all fit.
	top int
	// at is where it was opened from; anchored is unset to center it.
	at       point
	anchored bool
	// border is the frame's colour, popupBorder when unset; config is set
	// on the popups about the settings files, closed when the files change.
	border color.Color
	config bool
	// peer is set on the question about a peer's message.
	peer bool
	// dismiss, when set, runs when the popup closes without a choice.
	dismiss func(m model) (tea.Model, tea.Cmd)
}

// popupBox is a popup laid out: its lines, where it goes, and which row
// of it holds which option.
type popupBox struct {
	lines   []string
	x, y    int
	options map[int]int
}

// openPopup shows a menu over the screen, at the last right click if one
// opened it.
func (m model) openPopup(title, description string, options []huh.Option[string], do func(m model, choice string) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	m.popup = &popup{title: title, description: description, options: options, do: do, at: m.clickedAt, anchored: m.clicked}
	m.clicked = false
	return m, nil
}

// popupLayout draws the popup and places it inside the screen: below and
// right of where it was opened, moved up or left when it would not fit.
func (m model) popupLayout() popupBox {
	p := m.popup
	screenWidth, screenHeight := max(m.width, popupMinWidth+2), max(m.height, 6)
	width := ansi.StringWidth(p.title) + 2
	for _, option := range p.options {
		width = max(width, ansi.StringWidth(option.Key)+4)
	}
	width = min(max(width, popupMinWidth), popupMaxWidth, screenWidth-2)

	var body []string
	options := map[int]int{}
	body = append(body, toolName.Render(ansi.Truncate(p.title, width, "…")))
	if p.description != "" {
		described := strings.Split(ansi.Wrap(p.description, width, " "), "\n")
		room := max(screenHeight-len(p.options)-5, 1)
		if len(described) > min(popupDescriptionMax, room) {
			described = described[len(described)-min(popupDescriptionMax, room):]
		}
		for _, line := range described {
			body = append(body, dimStyle.Render(ansi.Truncate(line, width, "…")))
		}
	}
	body = append(body, "")
	// As many options as fit, scrolled to keep the one selected in view.
	room := len(p.options)
	if fits := screenHeight - len(body) - 2; room > fits {
		room = max(fits-2, 1)
	}
	p.top = min(max(p.top, p.selected-room+1, 0), p.selected, max(len(p.options)-room, 0))
	if p.top > 0 {
		body = append(body, dimStyle.Render(fmt.Sprintf(" ↑ %d more", p.top)))
	}
	for i := p.top; i < min(p.top+room, len(p.options)); i++ {
		label := " " + ansi.Truncate(p.options[i].Key, width-2, "…")
		label += strings.Repeat(" ", max(width-ansi.StringWidth(label), 0))
		if i == p.selected {
			label = popupSelected.Render(ansi.Strip(label))
		}
		options[len(body)+1] = i
		body = append(body, label)
	}
	if below := len(p.options) - p.top - room; below > 0 {
		body = append(body, dimStyle.Render(fmt.Sprintf(" ↓ %d more", below)))
	}
	border := p.border
	if border == nil {
		border = popupBorder
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).
		Background(lipgloss.Color("#16161e")).Render(strings.Join(body, "\n"))
	lines := strings.Split(box, "\n")
	boxWidth, boxHeight := ansi.StringWidth(lines[0]), len(lines)

	x, y := (screenWidth-boxWidth)/2, (screenHeight-boxHeight)/2
	if p.anchored {
		x, y = p.at.x, p.at.y+1
		if y+boxHeight > screenHeight {
			y = p.at.y - boxHeight
		}
		if x+boxWidth > screenWidth {
			x = screenWidth - boxWidth
		}
	}
	x, y = min(max(x, 0), max(screenWidth-boxWidth, 0)), min(max(y, 0), max(screenHeight-boxHeight, 0))
	return popupBox{lines: lines, x: x, y: y, options: options}
}

// overPopup draws the popup over the screen.
func (m model) overPopup(screen string) string {
	box := m.popupLayout()
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(strings.Join(box.lines, "\n")).X(box.x).Y(box.y).Z(1),
	).Render()
}

// updatePopup moves through the popup's options and takes one; esc or a
// click outside it closes it.
func (m model) updatePopup(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := m.popup
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k", "shift+tab":
			p.selected = (p.selected + len(p.options) - 1) % len(p.options)
		case "down", "j", "tab":
			p.selected = (p.selected + 1) % len(p.options)
		case "enter", "l", "space":
			return m.choosePopup(p.selected)
		case "esc", "q", "h", "ctrl+c":
			return m.dismissPopup()
		default:
			for i, option := range p.options {
				if key := msg.String(); len(key) == 1 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(ansi.Strip(option.Key))), key) {
					p.selected = i
					break
				}
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		box := m.popupLayout()
		row, inside := msg.Y-box.y, msg.X >= box.x && msg.X < box.x+ansi.StringWidth(box.lines[0]) && msg.Y >= box.y && msg.Y < box.y+len(box.lines)
		if !inside {
			dismissed, cmd := m.dismissPopup()
			m = dismissed.(model)
			if msg.Button == tea.MouseRight {
				clicked, click := m.rightClick(msg.X, msg.Y)
				return clicked, tea.Batch(cmd, click)
			}
			return m, cmd
		}
		if i, ok := box.options[row]; ok && msg.Button == tea.MouseLeft {
			return m.choosePopup(i)
		}
		return m, nil
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			p.selected = max(p.selected-1, 0)
		} else {
			p.selected = min(p.selected+1, len(p.options)-1)
		}
		return m, nil
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return m, nil
	case tea.WindowSizeMsg:
		return m, m.resize(msg)
	}
	return m, nil
}

// dismissPopup closes the popup without a choice.
func (m model) dismissPopup() (tea.Model, tea.Cmd) {
	p := m.popup
	m.popup = nil
	if p.dismiss != nil {
		return p.dismiss(m)
	}
	return m, nil
}

func (m model) choosePopup(i int) (tea.Model, tea.Cmd) {
	p := m.popup
	m.popup = nil
	if i < 0 || i >= len(p.options) {
		return m, nil
	}
	return p.do(m, p.options[i].Value)
}
