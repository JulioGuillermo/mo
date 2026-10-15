package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

const (
	uiDialogMaxWidth = 100
	uiDialogHelp     = "tab move · enter press · ←→ change · esc hides it"
)

// uiChangedMsg says the model created, updated or removed an interface.
type uiChangedMsg struct{}

// uiLive is where the focus is in an interface and how far it is
// scrolled; what was typed is kept with the interface.
type uiLive struct {
	focus  string
	scroll int
}

func (m model) uis() *tools.UIs {
	if m.session.Agent == nil || m.session.Agent.Tools == nil {
		return nil
	}
	return m.session.Agent.Tools.UIs()
}

// live is the state of an interface, made when first opened.
func (m *model) live(id int) *uiLive {
	if m.uiLives == nil {
		m.uiLives = map[int]*uiLive{}
	}
	state, ok := m.uiLives[id]
	if !ok {
		state = &uiLive{}
		m.uiLives[id] = state
	}
	return state
}

// openUI shows an interface as a dialog over the screen.
func (m model) openUI(id int) (tea.Model, tea.Cmd) {
	uis := m.uis()
	if uis == nil {
		return m, nil
	}
	ui, ok := uis.Get(id)
	if !ok {
		return m.emit(m.printer.Note(fmt.Sprintf("UI %d is gone", id)))
	}
	uis.MarkSeen(id)
	state := m.live(id)
	if ids := focusable(ui.Shown()); len(ids) > 0 && !slices.Contains(ids, state.focus) {
		state.focus = ids[0]
	}
	m.uiOpen = id
	return m, nil
}

// uiFrame is the open interface drawn in its dialog, where it goes, and
// its hits moved to the screen.
type uiFrame struct {
	lines []string
	x, y  int
	hits  []uiHit
}

func (m model) uiFrame() (uiFrame, bool) {
	uis := m.uis()
	if uis == nil {
		return uiFrame{}, false
	}
	ui, ok := uis.Get(m.uiOpen)
	if !ok {
		return uiFrame{}, false
	}
	state := m.live(m.uiOpen)
	width := max(min(m.width-6, uiDialogMaxWidth), 20)
	body := uiRenderer{focus: state.focus}.render(ui.Shown(), width-4, "", false)
	room := max(m.height-6, 3)
	state.scroll = min(max(state.scroll, 0), max(body.height()-room, 0))
	shown := body.lines[state.scroll:min(state.scroll+room, body.height())]
	content := strings.Join(shown, "\n") + "\n"
	if ui.Error != "" {
		content += "\n" + errorStyle.Render(ansi.Truncate("script: "+ui.Error, width-4, "…"))
	}
	content += "\n" + dimStyle.Render(ansi.Truncate(uiDialogHelp, width-4, "…"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(popupBorder).Padding(0, 1).
		Render(toolName.Render(fmt.Sprintf("UI %d · %s", ui.ID, ui.Title)) + "\n" + content)
	lines := strings.Split(box, "\n")
	x := max((m.width-ansi.StringWidth(lines[0]))/2, 0)
	y := max((m.height-len(lines))/2, 0)
	var hits []uiHit
	for _, hit := range body.hits {
		top := hit.y0 - state.scroll
		if top < 0 || top >= len(shown) {
			continue
		}
		// border, padding and the title line come before the body.
		hits = append(hits, hit.moved(x+2, y+2-state.scroll))
	}
	return uiFrame{lines: lines, x: x, y: y, hits: hits}, true
}

// overUI draws the open interface over the screen.
func (m model) overUI(screen string) string {
	frame, ok := m.uiFrame()
	if !ok {
		return screen
	}
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(strings.Join(frame.lines, "\n")).X(frame.x).Y(frame.y).Z(1),
	).Render()
}

// updateUI is the open dialog taking keys and clicks.
func (m model) updateUI(msg tea.Msg) (tea.Model, tea.Cmd) {
	uis := m.uis()
	ui, ok := uis.Get(m.uiOpen)
	if !ok {
		m.uiOpen = 0
		return m, nil
	}
	state := m.live(ui.ID)
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.uiKey(ui, state, msg)
	case tea.MouseClickMsg:
		frame, _ := m.uiFrame()
		width := ansi.StringWidth(frame.lines[0])
		if msg.X < frame.x || msg.X >= frame.x+width || msg.Y < frame.y || msg.Y >= frame.y+len(frame.lines) {
			m.uiOpen = 0
			return m, nil
		}
		for _, hit := range frame.hits {
			if msg.X >= hit.x0 && msg.X < hit.x1 && msg.Y >= hit.y0 && msg.Y < hit.y1 {
				node, _ := findNode(ui.Shown(), hit.id)
				if node.Disabled {
					return m, nil
				}
				previous := state.focus
				switch node.Type {
				case tools.UIButton:
					state.focus = hit.id
					return m.uiFire(ui.ID, hit.id, "click")
				case tools.UIInput:
					return m.leaveInput(ui, previous, hit.id)
				}
				value := clickedValue(node, hit, msg.X)
				left, cmd := m.leaveInput(ui, previous, hit.id)
				if after := left.(model); after.uiOpen == ui.ID {
					changed, more := after.uiChange(ui.ID, node, value)
					return changed, tea.Batch(cmd, more)
				}
				return left, cmd
			}
		}
		return m, nil
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			state.scroll -= 3
		} else {
			state.scroll += 3
		}
		return m, nil
	case tea.WindowSizeMsg:
		return m, m.resize(msg)
	}
	return m, nil
}

func (m model) uiKey(ui tools.UI, state *uiLive, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	shown := ui.Shown()
	ids := focusable(shown)
	at := slices.Index(ids, state.focus)
	move := func(by int) (tea.Model, tea.Cmd) {
		if len(ids) == 0 {
			return m, nil
		}
		next := ids[(max(at, 0)+by+len(ids))%len(ids)]
		return m.leaveInput(ui, state.focus, next)
	}
	node, _ := findNode(shown, state.focus)
	switch msg.String() {
	case "esc", "ctrl+c":
		m.uiOpen = 0
		return m, nil
	case "tab", "down":
		return move(1)
	case "shift+tab", "up":
		return move(-1)
	case "pgup":
		state.scroll -= max(m.height-8, 1)
		return m, nil
	case "pgdown":
		state.scroll += max(m.height-8, 1)
		return m, nil
	case "enter":
		switch node.Type {
		case tools.UIButton:
			return m.uiFire(ui.ID, node.ID, "click")
		case tools.UICheckbox, tools.UISwitch:
			return m.uiChange(ui.ID, node, node.Toggled())
		case tools.UISelect, tools.UIRadio:
			return m.uiChange(ui.ID, node, node.Moved(1))
		case tools.UIInput:
			updated, cmd := m.uiFire(ui.ID, node.ID, "change")
			if next := updated.(model); next.uiOpen == ui.ID {
				after, more := next.focusNext(ui.ID)
				return after, tea.Batch(cmd, more)
			}
			return updated, cmd
		}
		return m, nil
	}
	if node.HasValue() && node.Type != tools.UIInput {
		switch msg.String() {
		case "left":
			return m.uiChange(ui.ID, node, node.Moved(-1))
		case "right":
			return m.uiChange(ui.ID, node, node.Moved(1))
		case "home":
			return m.uiChange(ui.ID, node, node.AtFraction(0))
		case "end":
			return m.uiChange(ui.ID, node, node.AtFraction(1))
		case "space":
			if node.Type == tools.UICheckbox || node.Type == tools.UISwitch {
				return m.uiChange(ui.ID, node, node.Toggled())
			}
			return m.uiChange(ui.ID, node, node.Moved(1))
		}
		return m, nil
	}
	if node.Type != tools.UIInput {
		return m, nil
	}
	value := node.Value
	switch msg.String() {
	case "backspace":
		if runes := []rune(value); len(runes) > 0 {
			value = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		value = ""
	default:
		if msg.Text == "" {
			return m, nil
		}
		value += msg.Text
	}
	m.uis().SetValue(ui.ID, node.ID, value)
	return m.uiFire(ui.ID, node.ID, "input")
}

// uiChange keeps a control's new value and tells its on_change, when the
// value is new; a select, radio or slider at its end stays as it was.
func (m model) uiChange(id int, node tools.UINode, value string) (tea.Model, tea.Cmd) {
	if node.Type == tools.UISlider || node.Type == tools.UISelect || node.Type == tools.UIRadio {
		if value == "" {
			return m, nil
		}
	}
	if value == node.Current() {
		return m, nil
	}
	m.uis().SetValue(id, node.ID, value)
	return m.uiFire(id, node.ID, "change")
}

// clickedValue is the value a click on a control gives it: the option
// clicked, the arrow's way, where on a slider, or a tick flipped.
func clickedValue(node tools.UINode, hit uiHit, x int) string {
	switch node.Type {
	case tools.UIRadio:
		if hit.part >= 1 && hit.part <= len(node.Options) {
			return node.Options[hit.part-1]
		}
	case tools.UISelect:
		if hit.part == uiSelectEarlier {
			return node.Moved(-1)
		}
		return node.Moved(1)
	case tools.UISlider:
		return node.AtFraction(float64(x-hit.x0) / float64(max(hit.x1-hit.x0-1, 1)))
	case tools.UICheckbox, tools.UISwitch:
		return node.Toggled()
	}
	return node.Current()
}

// focusNext moves the focus one place on, as tab does.
func (m model) focusNext(id int) (tea.Model, tea.Cmd) {
	ui, ok := m.uis().Get(id)
	if !ok {
		return m, nil
	}
	state := m.live(id)
	ids := focusable(ui.Shown())
	if at := slices.Index(ids, state.focus); at >= 0 && len(ids) > 1 {
		state.focus = ids[(at+1)%len(ids)]
	}
	return m, nil
}

// leaveInput moves the focus, telling a change of the input it leaves.
func (m model) leaveInput(ui tools.UI, from, to string) (tea.Model, tea.Cmd) {
	state := m.live(ui.ID)
	state.focus = to
	if from == to || from == "" {
		return m, nil
	}
	if node, ok := findNode(ui.Shown(), from); ok && node.Type == tools.UIInput && node.OnChange != "" {
		return m.uiFire(ui.ID, from, "change")
	}
	return m, nil
}

// uiFire lets the interface do what an element names for an event, and
// follows: a submitted or closed one hides, a removed one goes, and an
// idle model gets a turn to hear what it was told.
func (m model) uiFire(id int, element, event string) (tea.Model, tea.Cmd) {
	outcome := m.uis().Fire(id, element, event)
	if outcome.Submitted || outcome.Hidden || outcome.Removed {
		m.uiOpen = 0
	}
	if outcome.Removed {
		delete(m.uiLives, id)
	}
	if !outcome.Told || m.running {
		return m, nil
	}
	return m.reactToJobs()
}

// uiMenu is what a right click on an interface in the side panel offers.
func (m model) uiMenu(id int) (tea.Model, tea.Cmd) {
	ui, ok := m.uis().Get(id)
	if !ok {
		return m, nil
	}
	options := []huh.Option[string]{huh.NewOption("open it", "open"), huh.NewOption("remove it", "remove")}
	return m.showChoices(fmt.Sprintf("UI %d", id), ui.Title, options, func(m model, choice string) (tea.Model, tea.Cmd) {
		if choice == "remove" {
			_ = m.uis().Remove(id)
			delete(m.uiLives, id)
			if m.uiOpen == id {
				m.uiOpen = 0
			}
			return m.emit(m.printer.Note(fmt.Sprintf("UI %d removed", id)))
		}
		return m.openUI(id)
	})
}
