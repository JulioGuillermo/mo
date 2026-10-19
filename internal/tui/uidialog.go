package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

const (
	uiDialogMaxWidth = 100
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

// dialogUIs are the interfaces the open dialog is among: the model's, or
// those of the plugins.
func (m model) dialogUIs() *tools.UIs {
	if m.uiStore != nil {
		return m.uiStore
	}
	return m.uis()
}

// openUI shows an interface of the model as a dialog over the screen.
func (m model) openUI(id int) (tea.Model, tea.Cmd) {
	return m.openUIIn(m.uis(), id)
}

// openPluginDialog shows a dialog a plugin opened.
func (m model) openPluginDialog(id int) (tea.Model, tea.Cmd) {
	return m.openUIIn(m.session.Plugins.Dialogs(), id)
}

func (m model) openUIIn(uis *tools.UIs, id int) (tea.Model, tea.Cmd) {
	if uis == nil {
		return m, nil
	}
	ui, ok := uis.Get(id)
	if !ok {
		return m.emit(m.printer.Note(fmt.Sprintf("UI %d is gone", id)))
	}
	uis.MarkSeen(id)
	m.uiStore = uis
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
	uis := m.dialogUIs()
	if uis == nil {
		return uiFrame{}, false
	}
	ui, ok := uis.Get(m.uiOpen)
	if !ok {
		return uiFrame{}, false
	}
	state := m.live(m.uiOpen)
	most := max(min(m.width-6, uiDialogMaxWidth), 20)
	tree := ui.Shown()
	body := uiRenderer{focus: state.focus, svgs: m.svgs, ui: ui.ID}.render(tree, most-4, "", false)
	room := max(m.height-4, 3)
	state.scroll = min(max(state.scroll, 0), max(body.height()-room, 0))
	shown := body.lines[state.scroll:min(state.scroll+room, body.height())]

	// The title goes in the top edge, unless the interface's own card says
	// it; the number and × at its right; the keys in the bottom edge.
	title := ui.Title
	if tree.Type == tools.UICard && strings.TrimSpace(tree.Title) == strings.TrimSpace(ui.Title) {
		title = ""
	}
	tag := fmt.Sprintf(" UI %d · ", ui.ID)
	if ui.Owner != "" {
		tag = " " + ui.Owner + " · "
	}
	inner := body.width
	if title != "" {
		inner = max(inner, min(ansi.StringWidth(title)+ansi.StringWidth(tag)+6, most-4))
	}
	inner = max(inner, ansi.StringWidth(tag)+4)
	keys, short := uiKeys(tree)
	inner = max(inner, min(ansi.StringWidth(short)+2, most-4))
	failure := ""
	if ui.Error != "" {
		failure = "script: " + ui.Error
		inner = max(inner, min(ansi.StringWidth(failure), most-4))
	}
	// The interface sits in the middle when the edges make the dialog wider.
	indent := (inner - body.width) / 2
	edge := lipgloss.NewStyle().Foreground(popupBorder)
	pad := func(line string, indent int) string {
		line = strings.Repeat(" ", indent) + line
		return edge.Render("│") + " " + line + strings.Repeat(" ", max(inner-ansi.StringWidth(line), 0)) + " " + edge.Render("│")
	}
	head := edge.Render("╭─")
	used := 2
	if title != "" {
		name := ansi.Truncate(title, max(inner-ansi.StringWidth(tag)-4, 4), "…")
		head += " " + toolName.Render(name) + " "
		used += ansi.StringWidth(name) + 2
	}
	right := dimStyle.Render(tag) + toolName.Render("×") + edge.Render(" ─╮")
	rightWidth := ansi.StringWidth(tag) + 1 + 3
	fill := max(inner+4-used-rightWidth, 1)
	head += edge.Render(strings.Repeat("─", fill)) + right
	closeAt := used + fill + ansi.StringWidth(tag)
	lines := []string{head}
	for _, line := range shown {
		lines = append(lines, pad(line, indent))
	}
	if failure != "" {
		lines = append(lines, pad(errorStyle.Render(ansi.Truncate(failure, inner, "…")), 0))
	}
	if ansi.StringWidth(keys) > inner-2 {
		keys = short
	}
	keys = ansi.Truncate(keys, max(inner-2, 1), "…")
	lines = append(lines, edge.Render("╰─ ")+dimStyle.Render(keys)+" "+edge.Render(strings.Repeat("─", max(inner-ansi.StringWidth(keys)-1, 0))+"╯"))

	x := max((m.width-ansi.StringWidth(lines[0]))/2, 0)
	y := max((m.height-len(lines))/2, 0)
	hits := []uiHit{{x0: x + closeAt, x1: x + closeAt + 1, y0: y, y1: y + 1, id: uiCloseHit}}
	for _, hit := range body.hits {
		top := hit.y0 - state.scroll
		if top < 0 || top >= len(shown) {
			continue
		}
		// The top edge comes before the body, the edge and a space beside it.
		hits = append(hits, hit.moved(x+2+indent, y+1-state.scroll))
	}
	return uiFrame{lines: lines, x: x, y: y, hits: hits}, true
}

// uiCloseHit is the id of the × in the dialog's top edge.
const uiCloseHit = "\x00close"

// uiKeys are the keys the interface takes, as its controls ask for, said
// whole and, for a narrow dialog, the keys alone.
func uiKeys(tree tools.UINode) (string, string) {
	var buttons, inputs, changes, ticks bool
	var walk func(n tools.UINode)
	walk = func(n tools.UINode) {
		if !n.Shown() {
			return
		}
		switch n.Type {
		case tools.UIButton:
			buttons = true
		case tools.UIInput:
			inputs = true
		case tools.UISelect, tools.UISlider, tools.UIRadio:
			changes = true
		case tools.UICheckbox, tools.UISwitch:
			ticks = true
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(tree)
	var keys, short []string
	add := func(key, what string) {
		keys, short = append(keys, key+" "+what), append(short, key)
	}
	if len(focusable(tree)) > 1 {
		add("tab", "move")
	}
	if inputs {
		keys, short = append(keys, "type"), append(short, "type")
	}
	if buttons {
		add("enter", "press")
	}
	if changes {
		add("←→", "change")
	}
	if ticks {
		add("space", "tick")
	}
	add("esc", "hides")
	return strings.Join(keys, " · "), strings.Join(short, " · ")
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
	uis := m.dialogUIs()
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
				if hit.id == uiCloseHit {
					m.uiOpen = 0
					return m, nil
				}
				node, _ := findNode(ui.Shown(), hit.id)
				if node.Disabled {
					return m, nil
				}
				previous := state.focus
				switch node.Type {
				case tools.UIButton:
					state.focus = hit.id
					return m.uiFire(ui.ID, hit.id, "click")
				case tools.UISVG:
					return m.clickSVG(ui.ID, node, hit, msg.X, msg.Y)
				case tools.UIInput:
					return m.leaveInput(ui, previous, hit.id)
				case tools.UISelect:
					if hit.part == uiSelectList {
						left, cmd := m.leaveInput(ui, previous, hit.id)
						after := left.(model)
						after.clickedAt, after.clicked = point{msg.X, msg.Y}, true
						listed, more := after.selectList(ui.ID, node)
						return listed, tea.Batch(cmd, more)
					}
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
		case tools.UISelect:
			return m.selectList(ui.ID, node)
		case tools.UIRadio:
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
	m.dialogUIs().SetValue(ui.ID, node.ID, value)
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
	m.dialogUIs().SetValue(id, node.ID, value)
	return m.uiFire(id, node.ID, "change")
}

// selectList opens the list of a select's options over the interface,
// the one it holds marked and selected; taking one sets it.
func (m model) selectList(id int, node tools.UINode) (tea.Model, tea.Cmd) {
	current := node.Current()
	options := make([]huh.Option[string], len(node.Options))
	at := 0
	for i, option := range node.Options {
		label := option
		if option == current {
			label, at = option+"  ●", i
		}
		options[i] = huh.NewOption(label, option)
	}
	title := orDefault(node.Label, node.ID)
	updated, cmd := m.openPopup(title, "", options, func(m model, choice string) (tea.Model, tea.Cmd) {
		ui, ok := m.dialogUIs().Get(id)
		if !ok {
			return m, nil
		}
		fresh, found := findNode(ui.Shown(), node.ID)
		if !found {
			return m, nil
		}
		return m.uiChange(id, fresh, choice)
	})
	listed := updated.(model)
	listed.popup.selected = at
	return listed, cmd
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
	ui, ok := m.dialogUIs().Get(id)
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
	return m.uiFollow(id, m.dialogUIs().Fire(id, element, event))
}

// clickSVG tells an svg's on_click where it was clicked, in the svg's own
// units; a click in the margin around its picture is none.
func (m model) clickSVG(id int, node tools.UINode, hit uiHit, x, y int) (tea.Model, tea.Cmd) {
	cols, rows := hit.x1-hit.x0, hit.y1-hit.y0
	fx := (float64(x-hit.x0) + 0.5) / float64(max(cols, 1))
	fy := (float64(y-hit.y0) + 0.5) / float64(max(rows, 1))
	at, down, inside := render.SVGPoint([]byte(node.SVG), cols, rows, m.svgs != nil && m.svgs.kitty, fx, fy)
	if !inside {
		return m, nil
	}
	return m.uiFollow(id, m.dialogUIs().FireAt(id, node.ID, "click", at, down))
}

// uiFollow follows what an event led to.
func (m model) uiFollow(id int, outcome tools.UIOutcome) (tea.Model, tea.Cmd) {
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
