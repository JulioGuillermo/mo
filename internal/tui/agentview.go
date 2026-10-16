package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/team"
)

const agentViewHelp = "enter sends it a message · esc stops it · ctrl+x or ✕ closes · pgup pgdn scroll"

// agentView is a subagent's conversation over the whole screen, as the
// main one is shown: live while it works, with a title bar naming it and
// where it is in the tree, the subagents under it beside it, and an input
// to talk to it.
type agentView struct {
	id     int
	top    int
	follow bool
	input  textinput.Model
	notice string
	// drawn is its conversation drawn at width, from history messages.
	drawn   []string
	history int
	width   int
}

func (m model) team() *team.Team {
	return m.session.Team
}

// agentInfo is how a subagent stands, if it is still listed.
func (m model) agentInfo(id int) (team.Info, bool) {
	if m.team() == nil {
		return team.Info{}, false
	}
	for _, info := range m.team().List() {
		if info.ID == id {
			return info, true
		}
	}
	return team.Info{}, false
}

func (m model) openAgentView(id int) (tea.Model, tea.Cmd) {
	if _, ok := m.agentInfo(id); !ok {
		return m.emit(m.printer.Note(fmt.Sprintf("no subagent %d", id)))
	}
	input := textinput.New()
	input.Prompt = "› "
	input.Focus()
	m.agentView = &agentView{id: id, follow: true, input: input, history: -1}
	m.popup = nil
	m.refreshAgentView()
	return m, textinput.Blink
}

func (m *model) closeAgentView() {
	m.agentView = nil
}

// refreshAgentView draws the subagent's conversation again when it has
// more of it or the width changed; closes the view when it is gone.
func (m *model) refreshAgentView() {
	view := m.agentView
	if view == nil {
		return
	}
	if _, ok := m.agentInfo(view.id); !ok {
		m.agentView = nil
		return
	}
	history := m.team().History(view.id)
	width := m.mainWidth()
	if len(history) != view.history || width != view.width {
		drawn := *m
		drawn.blocks = nil
		drawn.replay(history)
		text, _ := drawn.transcript()
		view.drawn = strings.Split(strings.TrimRight(text, "\n"), "\n")
		view.history, view.width = len(history), width
	}
}

// agentBody is every line of the view's conversation: what was drawn and
// the reply being written.
func (m model) agentBody() []string {
	view := m.agentView
	lines := view.drawn
	if live := strings.TrimSpace(m.team().Live(view.id)); live != "" {
		lines = append(append([]string(nil), lines...), "")
		lines = append(lines, strings.Split(m.printer.Markdown(live), "\n")...)
	}
	return lines
}

// agentMark is the sign and colour of a subagent's state: a clock while it
// waits, yellow when someone under it waits for the user.
func agentMark(info team.Info, dark bool) (string, color.Color) {
	pick := func(onDark, onLight string) color.Color {
		if dark {
			return lipgloss.Color(onDark)
		}
		return lipgloss.Color(onLight)
	}
	yellow := pick("#e0af68", "#8c6c00")
	switch info.State {
	case team.Queued:
		return "⧗", pick("#737aa2", "#8990b3")
	case team.Waiting:
		if info.AskingBelow > 0 {
			return "⧗", yellow
		}
		return "⧗", pick("#bb9af7", "#7847bd")
	case team.Running:
		return "●", pick("#7aa2f7", "#2e7de9")
	case team.Asking:
		return "?", yellow
	case team.Done:
		return "✓", pick("#9ece6a", "#587539")
	}
	return "⚠", pick("#f7768e", "#f52a65")
}

// agentRows are the rows of a tree of subagents, indented by level from
// below, each opening its view, with its menu on a right click.
func (m model) agentRows(infos []team.Info, below int, width int, base lipgloss.Style, colors sidebarColors, first int) ([]string, []region) {
	dark := m.printer == nil || m.printer.isDark()
	var lines []string
	var regions []region
	for _, info := range infos {
		mark, tint := agentMark(info, dark)
		indent := strings.Repeat("  ", max(info.Depth-below-1, 0))
		line := base.Foreground(tint).Render(indent+mark+" ") + base.Render(oneLine(info.Name, max(width-len(indent)-14, 6)))
		note := string(info.State)
		if info.AskingBelow > 0 && info.State != team.Asking {
			line += base.Foreground(lipgloss.Color("#e0af68")).Render(fmt.Sprintf(" · %d asks", info.AskingBelow))
		} else {
			line += base.Foreground(colors.dim).Render(" · " + note)
		}
		id := info.ID
		open := func(m model) (tea.Model, tea.Cmd) { return m.openAgentView(id) }
		menu := func(m model) (tea.Model, tea.Cmd) { return m.agentMenu(id) }
		regions = append(regions, region{y: first + len(lines), x0: 0, x1: 1 << 30, do: open, menu: menu, item: fmt.Sprint("agent ", id)})
		lines = append(lines, line)
	}
	return lines, regions
}

// agentMenu is what a right click on a subagent offers.
func (m model) agentMenu(id int) (tea.Model, tea.Cmd) {
	info, ok := m.agentInfo(id)
	if !ok {
		return m, nil
	}
	options := []huh.Option[string]{huh.NewOption("see what it does, live, and talk to it", "open")}
	if info.State.Ended() {
		options = append(options, huh.NewOption("remove it from the list, with those under it", "remove"))
	} else {
		options = append(options, huh.NewOption("stop it, with those under it", "stop"))
	}
	description := fmt.Sprintf("level %d · %s · %d steps\n%s", info.Depth, info.State, info.Steps, oneLine(info.Task, max(m.width-8, 20)))
	return m.showChoices("Subagent "+info.Name, description, options, func(m model, choice string) (tea.Model, tea.Cmd) {
		switch choice {
		case "open":
			return m.openAgentView(id)
		case "stop":
			m.team().Stop(id)
			return m.emit(m.printer.Note(fmt.Sprintf("stopping subagent %s and those under it", info.Name)))
		case "remove":
			if err := m.team().Remove(id); err != nil {
				return m.emitError(err)
			}
			if m.agentView != nil && m.agentView.id == id {
				m.closeAgentView()
			}
		}
		return m, nil
	})
}

// agentViewHeight is the rows of the view's conversation.
func (m model) agentViewHeight() int {
	used := 4 // title, blank, input, status
	if m.dialog != nil {
		dialog, _ := m.dialogLayout()
		used += lipgloss.Height(dialog)
	}
	return max(m.height-used, 3)
}

// agentViewLayout draws the view and what can be clicked in it.
func (m model) agentViewLayout() (string, []region) {
	view := m.agentView
	info, _ := m.agentInfo(view.id)
	dark := m.printer == nil || m.printer.isDark()
	var regions []region

	// The title bar: its sign, name, where it is and the ✕ that closes.
	bar := lipgloss.NewStyle().Background(lipgloss.Color("#1f2335")).Foreground(lipgloss.Color("#c0caf5"))
	if !dark {
		bar = lipgloss.NewStyle().Background(lipgloss.Color("#d5d6db")).Foreground(lipgloss.Color("#343b58"))
	}
	mark, tint := agentMark(info, dark)
	title := bar.Foreground(tint).Render(" "+mark+" ") + bar.Bold(true).Render(info.Name) + bar.Render(fmt.Sprintf(" · level %d · ", info.Depth))
	x := ansi.StringWidth(title)
	crumb := func(text string, do func(m model) (tea.Model, tea.Cmd)) {
		part := bar.Underline(do != nil).Render(text)
		if do != nil {
			regions = append(regions, region{y: 0, x0: x, x1: x + ansi.StringWidth(text), do: do})
		}
		title += part
		x += ansi.StringWidth(text)
	}
	crumb("main", func(m model) (tea.Model, tea.Cmd) { m.closeAgentView(); return m, nil })
	for _, step := range m.team().Path(view.id) {
		title += bar.Render(" › ")
		x += 3
		if step.ID == view.id {
			crumb(step.Name, nil)
			continue
		}
		id := step.ID
		crumb(step.Name, func(m model) (tea.Model, tea.Cmd) { return m.openAgentView(id) })
	}
	state := fmt.Sprintf(" · %s · %d steps", info.State, info.Steps)
	if info.Model != "" {
		state += " · " + shortModelName(info.Model)
	}
	title += bar.Render(state)
	closer := " ✕ "
	room := m.width - ansi.StringWidth(closer)
	if ansi.StringWidth(title) > room {
		title = ansi.Truncate(title, room, "…")
	}
	title += bar.Render(strings.Repeat(" ", max(room-ansi.StringWidth(title), 0)))
	regions = append(regions, region{y: 0, x0: room, x1: m.width, do: func(m model) (tea.Model, tea.Cmd) { m.closeAgentView(); return m, nil }})
	title += bar.Bold(true).Render(closer)

	// Its conversation, following the end until scrolled up.
	height := m.agentViewHeight()
	body := m.agentBody()
	top := view.top
	if view.follow {
		top = max(len(body)-height, 0)
	}
	pane := conversationPane{width: m.mainWidth(), height: height, total: len(body), offset: min(top, max(len(body)-height, 0))}
	pane.window = func(from, to int) []string { return body[from:to] }
	left := pane.View()

	// Beside it, the subagents under it.
	if m.sidebarShown() {
		colors := sidebarPalette(dark)
		base := lipgloss.NewStyle().Background(colors.background).Foreground(colors.text)
		width := sidebarWidth - 2
		lines := []string{"", base.Foreground(colors.title).Bold(true).Render("Under it")}
		var under []team.Info
		for _, other := range m.team().List() {
			if m.below(other.ID, view.id) {
				under = append(under, other)
			}
		}
		rows, rowRegions := m.agentRows(under, info.Depth, width, base, colors, len(lines)+1)
		if len(rows) == 0 {
			rows = []string{base.Foreground(colors.dim).Render("no subagents of its own")}
		}
		lines = append(lines, rows...)
		for len(lines) < height {
			lines = append(lines, "")
		}
		pad := base.Render(" ")
		for i, line := range lines[:height] {
			lines[i] = pad + line + base.Render(strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))) + pad
		}
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", strings.Join(lines[:height], "\n"))
		for _, r := range rowRegions {
			r.y, r.x0, r.x1 = r.y+1, m.mainWidth()+1, m.width
			regions = append(regions, r)
		}
	}

	screen := []string{title, left}
	if m.dialog != nil {
		dialog, dialogRegions := m.dialogLayout()
		regions = append(regions, shifted(dialogRegions, 1+height)...)
		screen = append(screen, dialog)
	}
	notice := view.notice
	if notice == "" {
		notice = agentViewHelp
	}
	view.input.Placeholder = "message for " + info.Name
	view.input.SetWidth(max(m.width-4, 10))
	screen = append(screen, "", view.input.View(), dimStyle.Render(m.agentStatus(info)+" · "+notice))
	return strings.Join(screen, "\n"), regions
}

// agentStatus is the line under the view: how much of its window it uses.
func (m model) agentStatus(info team.Info) string {
	used := fmt.Sprintf("ctx %s", formatTokens(info.Usage.InputTokens+info.Usage.OutputTokens))
	if info.Window > 0 {
		used += " / " + formatTokens(info.Window)
	}
	return used
}

// below tells whether id is under ancestor in the tree.
func (m model) below(id, ancestor int) bool {
	for _, step := range m.team().Path(id) {
		if step.ID == ancestor && step.ID != id {
			return true
		}
	}
	return false
}

func (m model) agentViewScreen() string {
	screen, _ := m.agentViewLayout()
	if m.popup != nil {
		screen = m.overPopup(screen)
	}
	return screen
}

// updateAgentView handles what the user does in the view.
func (m model) updateAgentView(msg tea.Msg) (tea.Model, tea.Cmd) {
	view := m.agentView
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// ctrl+x always closes: what is asked stays open in the main view.
		if msg.String() == "ctrl+x" {
			m.closeAgentView()
			return m, nil
		}
		if m.dialog != nil {
			return m.handleDialogKey(msg)
		}
		view.notice = ""
		switch msg.String() {
		case "esc":
			info, _ := m.agentInfo(view.id)
			if info.State.Ended() {
				view.notice = "it is not working · ctrl+x closes"
				return m, nil
			}
			m.team().Stop(view.id)
			view.notice = "stopping it and those under it"
			return m, nil
		case "enter":
			text := strings.TrimSpace(view.input.Value())
			if text == "" {
				return m, nil
			}
			said, err := m.team().Send(view.id, text)
			if err != nil {
				view.notice = err.Error()
				return m, nil
			}
			view.input.Reset()
			view.notice, view.follow = said, true
			return m, nil
		case "pgup", "shift+up", "ctrl+up":
			m.scrollAgentView(true, m.agentPage(msg.String()))
			return m, nil
		case "pgdown", "shift+down", "ctrl+down":
			m.scrollAgentView(false, m.agentPage(msg.String()))
			return m, nil
		case "home":
			view.top, view.follow = 0, false
			return m, nil
		case "end":
			view.follow = true
			return m, nil
		}
		var cmd tea.Cmd
		view.input, cmd = view.input.Update(msg)
		return m, cmd
	case tea.PasteMsg:
		if m.dialog != nil {
			return m.pasteIntoDialog(msg)
		}
		var cmd tea.Cmd
		view.input, cmd = view.input.Update(msg)
		return m, cmd
	case tea.MouseWheelMsg:
		m.scrollAgentView(msg.Button == tea.MouseWheelUp, 3)
		return m, nil
	case tea.MouseClickMsg:
		_, regions := m.agentViewLayout()
		for _, r := range regions {
			if r.y != msg.Y || msg.X < r.x0 || msg.X >= r.x1 {
				continue
			}
			switch {
			case msg.Button == tea.MouseRight && r.menu != nil:
				return r.menu(m)
			case msg.Button == tea.MouseLeft && r.do != nil:
				return r.do(m)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m model) agentPage(key string) int {
	if strings.HasPrefix(key, "shift") || strings.HasPrefix(key, "ctrl") {
		return 3
	}
	return max(m.agentViewHeight()-2, 1)
}

func (m *model) scrollAgentView(up bool, lines int) {
	view := m.agentView
	height := m.agentViewHeight()
	total := len(m.agentBody())
	top := view.top
	if view.follow {
		top = max(total-height, 0)
	}
	if up {
		top -= lines
	} else {
		top += lines
	}
	view.top = min(max(top, 0), max(total-height, 0))
	view.follow = view.top >= total-height
}

// pickAgent opens subagent n's view, or lists them to pick one.
func (m model) pickAgent(argument string) (tea.Model, tea.Cmd) {
	if m.team() == nil || len(m.team().List()) == 0 {
		return m.emit(m.printer.Note("no subagents · agents.enabled in /settings › Subagents lets the model start them"))
	}
	if argument != "" {
		var id int
		if _, err := fmt.Sscan(argument, &id); err != nil {
			return m.emit(m.printer.Note("usage: /agents [n]"))
		}
		return m.openAgentView(id)
	}
	var options []huh.Option[string]
	for _, info := range m.team().List() {
		mark, _ := agentMark(info, true)
		label := fmt.Sprintf("%s%s %d %s · %s", strings.Repeat("  ", info.Depth-1), mark, info.ID, info.Name, info.State)
		options = append(options, huh.NewOption(label, fmt.Sprint(info.ID)))
	}
	return m.showChoices("Subagents", "pick one to see what it does and talk to it", options, func(m model, choice string) (tea.Model, tea.Cmd) {
		return m.pickAgent(choice)
	})
}
