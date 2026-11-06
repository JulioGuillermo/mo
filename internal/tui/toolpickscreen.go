package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

const (
	toolPickDone   = "\x00done"
	toolPickServer = "\x00server:"
	toolPickHelp   = "space tick · enter on a server: its tools · esc cancel"
	serverToolHelp = "space tick · ctrl+a all · / filter · enter keep · esc back"
)

// toolPickScreen is a tool pick shown in the settings: keep goes back with
// what is ticked, cancel puts what was ticked before back.
type toolPickScreen struct {
	title  string
	note   string
	pick   *toolPick
	keep   func(m model) (tea.Model, tea.Cmd)
	cancel func(m model) (tea.Model, tea.Cmd)
}

// showToolPick lists the tools of mo and the plugins one by one, then each
// MCP server as one row, with the cursor on at.
func (m model) showToolPick(screen *toolPickScreen, at string) (tea.Model, tea.Cmd) {
	pick := screen.pick
	width := 0
	for _, server := range pick.servers {
		width = max(width, len(server.name))
	}
	var options []huh.Option[string]
	for _, name := range pick.own {
		options = append(options, huh.NewOption(tickMark(pick.ticked(name))+" "+name, name))
	}
	for _, server := range pick.servers {
		label := fmt.Sprintf("%s %-*s   %s ›", serverMark(pick, server), width, server.name, dimStyle.Render(pick.serverState(server)))
		options = append(options, huh.NewOption(label, toolPickServer+server.name))
	}
	options = append(options, huh.NewOption(keyStyle().Render("‹ done"), toolPickDone))
	if at == "" {
		at = options[0].Value
	}
	list := huh.NewSelect[string]().Title(screen.title).Description(screen.note).Options(options...).Value(&at)
	if m.height > 0 {
		list.Height(min(len(options)+2+lipgloss.Height(screen.note), max(m.height-4, settingsMinHeight)))
	}
	again := func(m model, row string) (tea.Model, tea.Cmd) { return m.showToolPick(screen, row) }
	next := func(m model) (tea.Model, tea.Cmd) {
		if at == toolPickDone {
			return screen.keep(m)
		}
		if name, isServer := strings.CutPrefix(at, toolPickServer); isServer {
			return m.showServerTools(screen, pick.server(name))
		}
		pick.toggle(at)
		return again(m, at)
	}
	toggle := func(m model) (tea.Model, tea.Cmd) {
		row, ok := list.Hovered()
		if !ok || row == toolPickDone {
			return again(m, row)
		}
		if name, isServer := strings.CutPrefix(row, toolPickServer); isServer {
			pick.toggleServer(pick.server(name))
		} else {
			pick.toggle(row)
		}
		return again(m, row)
	}
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: screen.cancel, help: toolPickHelp, toggle: toggle})
}

// showServerTools ticks the tools of one MCP server; enter keeps them,
// esc leaves them as they were.
func (m model) showServerTools(screen *toolPickScreen, server toolServer) (tea.Model, tea.Cmd) {
	pick := screen.pick
	back := func(m model) (tea.Model, tea.Cmd) { return m.showToolPick(screen, toolPickServer+server.name) }
	if len(server.tools) == 0 {
		pick.toggleServer(server)
		return back(m)
	}
	ticked := pick.tickedOf(server)
	var options []huh.Option[string]
	for _, name := range server.tools {
		options = append(options, huh.NewOption(strings.TrimPrefix(name, mcpToolPrefix(server.name)), name))
	}
	list := huh.NewMultiSelect[string]().Title(screen.title + " › " + server.name).
		Description("all ticked: the whole server, the tools it adds later too").
		Options(options...).Filterable(true).Height(min(len(options)+3, maxToolChoiceHeight)).Value(&ticked)
	keep := func(m model) (tea.Model, tea.Cmd) {
		pick.setServer(server, ticked)
		return back(m)
	}
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: keep, back: back, help: serverToolHelp})
}

func tickMark(ticked bool) string {
	if ticked {
		return "[x]"
	}
	return "[ ]"
}

// serverMark is [x] for all of a server, [-] for some of it.
func serverMark(pick *toolPick, server toolServer) string {
	if !pick.allOf(server) && len(pick.tickedOf(server)) > 0 {
		return "[-]"
	}
	return tickMark(pick.allOf(server))
}
