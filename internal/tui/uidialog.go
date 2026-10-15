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
	uiDialogHelp     = "tab move · enter press · esc or a click outside hides it"
)

// uiChangedMsg says the model created, updated or removed an interface.
type uiChangedMsg struct{}

// uiLive is what the user did in an interface: what they typed, where the
// focus is, how far it is scrolled.
type uiLive struct {
	values map[string]string
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
		state = &uiLive{values: map[string]string{}}
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
	if ids := focusable(ui.Tree); len(ids) > 0 && !slices.Contains(ids, state.focus) {
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
	body := uiRenderer{values: state.values, focus: state.focus}.render(ui.Tree, width-4, "", false)
	room := max(m.height-6, 3)
	state.scroll = min(max(state.scroll, 0), max(body.height()-room, 0))
	shown := body.lines[state.scroll:min(state.scroll+room, body.height())]
	content := strings.Join(shown, "\n") + "\n\n" + dimStyle.Render(ansi.Truncate(uiDialogHelp, width-4, "…"))
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
		hits = append(hits, uiHit{hit.x0 + x + 2, top + y + 2, hit.x1 + x + 2, top + y + 3, hit.id})
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
				node, _ := findNode(ui.Tree, hit.id)
				if node.Disabled {
					return m, nil
				}
				previous := state.focus
				state.focus = hit.id
				if node.Type == tools.UIButton {
					return m.uiEvent(ui, node, "click", node.OnClick)
				}
				return m.leaveInput(ui, previous, hit.id)
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
	ids := focusable(ui.Tree)
	at := slices.Index(ids, state.focus)
	move := func(by int) (tea.Model, tea.Cmd) {
		if len(ids) == 0 {
			return m, nil
		}
		next := ids[(max(at, 0)+by+len(ids))%len(ids)]
		return m.leaveInput(ui, state.focus, next)
	}
	node, _ := findNode(ui.Tree, state.focus)
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
			return m.uiEvent(ui, node, "click", node.OnClick)
		case tools.UIInput:
			updated, cmd := m.uiEvent(ui, node, "change", node.OnChange)
			if next := updated.(model); next.uiOpen == ui.ID {
				after, more := next.focusNext(ui)
				return after, tea.Batch(cmd, more)
			}
			return updated, cmd
		}
		return m, nil
	}
	if node.Type != tools.UIInput {
		return m, nil
	}
	value, typed := state.values[node.ID]
	if !typed {
		value = node.Value
	}
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
	state.values[node.ID] = value
	return m.uiEvent(ui, node, "input", node.OnInput)
}

// focusNext moves the focus one place on, as tab does.
func (m model) focusNext(ui tools.UI) (tea.Model, tea.Cmd) {
	state := m.live(ui.ID)
	ids := focusable(ui.Tree)
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
	if node, ok := findNode(ui.Tree, from); ok && node.Type == tools.UIInput && node.OnChange != "" {
		return m.uiEvent(ui, node, "change", node.OnChange)
	}
	return m, nil
}

// uiEvent does what an element's handler says: tell the model, send it
// every value, or close the interface. A button without one tells it.
func (m model) uiEvent(ui tools.UI, node tools.UINode, event, handler string) (tea.Model, tea.Cmd) {
	if handler == "" && event == "click" {
		handler = tools.UINotify
	}
	uis := m.uis()
	state := m.live(ui.ID)
	value := state.values[node.ID]
	switch handler {
	case tools.UINotify:
		uis.Event(tools.UIEvent{UI: ui.ID, Action: tools.UINotify, Event: event, Element: node.ID, Value: value})
	case tools.UISubmit:
		uis.Event(tools.UIEvent{UI: ui.ID, Action: tools.UISubmit, Event: event, Element: node.ID, Values: inputValues(ui.Tree, state.values)})
		m.uiOpen = 0
	case tools.UIClose:
		_ = uis.Remove(ui.ID)
		uis.Event(tools.UIEvent{UI: ui.ID, Title: ui.Title, Action: tools.UIClose, Event: event, Element: node.ID})
		delete(m.uiLives, ui.ID)
		m.uiOpen = 0
	default:
		return m, nil
	}
	if m.running {
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
