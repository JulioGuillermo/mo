package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
)

const agentViewHelp = "message for %s · enter sends · esc stops it · ctrl+x closes"

// agentView is a subagent over the whole screen, drawn as the main
// conversation is, from the subagent's own: its conversation live, its
// spinner, its input, its status line and context, and the side panel with
// its tasks and the subagents under it; over them a title bar says where it
// is in the tree.
type agentView struct {
	id    int
	input textarea.Model
	// top and scrolled are where its conversation is scrolled to.
	top      int
	scrolled bool
	notice   string
	// blocks are its conversation drawn, from history messages at width;
	// todos its task list.
	blocks  []block
	todos   []tools.Todo
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
	info, ok := m.agentInfo(id)
	if !ok {
		return m.emit(m.printer.Note(fmt.Sprintf("no subagent %d", id)))
	}
	input := newInput()
	input.Placeholder = fmt.Sprintf(agentViewHelp, info.Name)
	// The whole width, as the main input, under the side panel too.
	input.SetWidth(max(m.width, 10))
	focus := input.Focus()
	m.agentView = &agentView{id: id, input: input, history: -1}
	m.popup = nil
	m.refreshAgentView()
	cmds := []tea.Cmd{focus}
	if !m.running && !info.State.Ended() {
		// Its spinner turns while it works, though the main agent does not.
		cmds = append(cmds, m.spinner.Tick)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) closeAgentView() {
	m.agentView = nil
}

// refreshAgentView draws the subagent's conversation again when it has
// more of it or the width changed, folds kept; closes the view when it is
// gone.
func (m *model) refreshAgentView() {
	view := m.agentView
	if view == nil {
		return
	}
	if _, ok := m.agentInfo(view.id); !ok {
		m.agentView = nil
		return
	}
	view.input.SetWidth(max(m.width, 10))
	history := m.team().History(view.id)
	width := m.mainWidth()
	if len(history) == view.history && width == view.width {
		return
	}
	drawn := *m
	drawn.blocks = nil
	drawn.replay(history)
	for i := range drawn.blocks {
		if i < len(view.blocks) && drawn.blocks[i].foldable && view.blocks[i].foldable && view.blocks[i].open != drawn.blocks[i].open {
			drawn.blocks[i].open = view.blocks[i].open
			drawn.blocks[i].text = drawn.blocks[i].draw(drawn.blocks[i].open)
		}
	}
	view.blocks, view.todos = drawn.blocks, drawn.todos
	view.history, view.width = len(history), width
}

// agentWorking tells whether the subagent in view works.
func (m model) agentWorking() bool {
	if m.agentView == nil {
		return false
	}
	info, ok := m.agentInfo(m.agentView.id)
	return ok && !info.State.Ended()
}

// lens is the model as the subagent in view would draw it: its
// conversation, input, usage, window, model, tasks and state in place of
// the main agent's, one row shorter for the title bar.
func (m model) lens() model {
	view := m.agentView
	info, _ := m.agentInfo(view.id)
	v := m
	v.agentView, v.lensOf, v.popup, v.shownSpeeds = nil, view.id, nil, nil
	v.height = max(m.height-1, 3)
	v.blocks, v.todos = view.blocks, view.todos
	v.top, v.scrolled = view.top, view.scrolled
	v.input = view.input
	v.queued = nil
	v.thread.title = info.Name
	v.usage, v.speed, v.metrics = info.Usage, info.Usage, liveMetrics{}
	v.running = !info.State.Ended()
	v.worked, v.runStarted = 0, time.Time{}
	v.forming, v.reasoning, v.promptRead = nil, "", llm.PromptProgress{}
	v.runningTool, v.activity, v.lastEvent = info.Tool, "", llm.EventText
	switch info.State {
	case team.Waiting:
		v.waitingFor = "for its subagents"
	case team.Queued:
		v.waitingFor = "for a free place to work"
	}
	// The calls of the reply in progress, which its history does not hold
	// yet, as the main conversation shows them.
	v.blocks = slices.Clone(view.blocks)
	for _, call := range m.team().Calls(view.id) {
		shown := slices.ContainsFunc(v.blocks, func(b block) bool { return b.tool != nil && b.tool.id == call.Call.ID })
		if !shown {
			// Summed up by its own tools, which the main agent may not have.
			v.addTool(call.Call, call.Summary)
		}
		if call.Done {
			v.finishTool(call.Call.ID, call.Output, call.IsError)
		}
	}
	v.stream = m.team().Live(view.id)
	v.streamCut, v.streamDone = 0, ""
	v.renderStream(true)
	session := m.session
	session.Model = orDefault(info.Model, session.Model)
	if provider, _, ok := strings.Cut(session.Config.Agents.Model, "/"); ok {
		session.Provider = provider
	}
	main := m.session.Agent
	session.Agent = &agent.Agent{Window: info.Window, Permissions: main.Permissions}
	v.session = session
	return v
}

// agentMark is the sign and colour of a subagent's state, round as the
// calls' are: dotted while it waits, purple for its own subagents and
// yellow when one under it waits for the user; an arc while it works;
// beating while it asks; filled when done, a cross when it failed and a bar
// when it was stopped.
func agentMark(info team.Info, dark, nerd bool) (string, color.Color) {
	pick := func(onDark, onLight string) color.Color {
		if dark {
			return lipgloss.Color(onDark)
		}
		return lipgloss.Color(onLight)
	}
	yellow := pick("#e0af68", "#8c6c00")
	switch info.State {
	case team.Queued:
		return markQueued, pick("#737aa2", "#8990b3")
	case team.Waiting:
		if info.AskingBelow > 0 {
			return markQueued, yellow
		}
		return markQueued, pick("#bb9af7", "#7847bd")
	case team.Running:
		return arc(), pick("#7aa2f7", "#2e7de9")
	case team.Asking:
		return askingSign(), yellow
	case team.Done:
		return markDone, pick("#9ece6a", "#587539")
	case team.Stopped:
		return markStopped, pick("#f7768e", "#f52a65")
	}
	return markFailed(nerd), pick("#f7768e", "#f52a65")
}

// agentRows are the rows of a tree of subagents, indented by level from
// below, each opening its view, with its menu on a right click.
func (m model) agentRows(infos []team.Info, below int, width int, base lipgloss.Style, colors sidebarColors, first int) ([]string, []region) {
	dark := m.printer == nil || m.printer.isDark()
	var lines []string
	var regions []region
	for _, info := range infos {
		mark, tint := agentMark(info, dark, m.icons() == iconsNerd)
		indent := strings.Repeat("  ", max(info.Depth-below-1, 0))
		note, noteStyle := " · "+string(info.State), base.Foreground(colors.dim)
		if info.AskingBelow > 0 && info.State != team.Asking {
			note, noteStyle = fmt.Sprintf(" · %d asks", info.AskingBelow), base.Foreground(lipgloss.Color("#e0af68"))
		}
		// Its context, used of its window, on the right; the name gives
		// way when there is no room.
		tokens := agentTokens(info)
		room := width - ansi.StringWidth(indent+mark+" "+note) - ansi.StringWidth(tokens) - 1
		name := oneLine(info.Name, max(room, 4))
		gap := max(width-ansi.StringWidth(indent+mark+" "+name+note+tokens), 1)
		line := base.Foreground(tint).Render(indent+mark+" ") + base.Render(name) + noteStyle.Render(note) +
			base.Render(strings.Repeat(" ", gap)) + base.Foreground(colors.dim).Render(tokens)
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

// below tells whether id is under ancestor in the tree.
func (m model) below(id, ancestor int) bool {
	for _, step := range m.team().Path(id) {
		if step.ID == ancestor && step.ID != id {
			return true
		}
	}
	return false
}

// agentViewLayout draws the view: a title bar, then the subagent's screen
// as the main one is drawn; and what can be clicked in it.
func (m model) agentViewLayout() (string, []region) {
	view := m.agentView
	info, _ := m.agentInfo(view.id)
	dark := m.printer == nil || m.printer.isDark()
	mark, tint := agentMark(info, dark, m.icons() == iconsNerd)
	bar := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1a1b26"))
	if !dark {
		bar = lipgloss.NewStyle().Background(lipgloss.Color("#2e7de9")).Foreground(lipgloss.Color("#ffffff"))
	}
	var regions []region
	title := bar.Render(" ") + bar.Foreground(tint).Background(lipgloss.Color("#1a1b26")).Bold(true).Render(" "+mark+" ") + bar.Render(" ")
	x := ansi.StringWidth(title)
	crumb := func(text string, do func(m model) (tea.Model, tea.Cmd)) {
		style := bar
		if do == nil {
			style = style.Bold(true)
		} else {
			style = style.Underline(true)
			regions = append(regions, region{y: 0, x0: x, x1: x + ansi.StringWidth(text), do: do})
		}
		title += style.Render(text)
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
	title += bar.Render(fmt.Sprintf("  ·  %s  ·  level %d  ·  %d steps", info.State, info.Depth, info.Steps))
	closer := " ctrl+x ✕ "
	if view.notice != "" {
		closer = " " + view.notice + "  ·" + closer
	}
	room := m.width - ansi.StringWidth(closer)
	if ansi.StringWidth(title) > room {
		title = ansi.Truncate(title, room, "…")
	}
	title += bar.Render(strings.Repeat(" ", max(room-ansi.StringWidth(title), 0)))
	regions = append(regions, region{y: 0, x0: room, x1: m.width, do: func(m model) (tea.Model, tea.Cmd) { m.closeAgentView(); return m, nil }})
	title += bar.Bold(true).Render(closer)

	screen, screenRegions := m.lens().screenLayout()
	return title + "\n" + screen, append(regions, shifted(screenRegions, 1)...)
}

func (m model) agentViewScreen() string {
	screen, _ := m.agentViewLayout()
	if m.popup != nil {
		screen = m.overPopup(screen)
	}
	return screen
}

// keepLens takes back what the view's own model changed: its input, where
// it is scrolled and its folds.
func (m *model) keepLens(v model) {
	view := m.agentView
	view.input, view.top, view.scrolled, view.blocks = v.input, v.top, v.scrolled, v.blocks
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
		v := m.lens()
		switch msg.String() {
		case "esc":
			if !v.running {
				view.notice = "it is not working"
				return m, nil
			}
			m.team().Stop(view.id)
			view.notice = "stopping it"
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
			view.notice, view.scrolled = said, false
			var tick tea.Cmd
			if !m.running {
				tick = m.spinner.Tick
			}
			return m, tick
		case "pgup", "shift+up", "ctrl+up", "pgdown", "shift+down", "ctrl+down":
			v.scroll(strings.HasSuffix(msg.String(), "up"), v.pageSize(msg.String()))
			m.keepLens(v)
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
		v := m.lens()
		v.scroll(msg.Button == tea.MouseWheelUp, 3)
		m.keepLens(v)
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
		// A click on its conversation opens or folds what is there.
		v := m.lens()
		if msg.Button == tea.MouseLeft && msg.Y >= 1 && msg.Y <= v.conversationHeight() && msg.X < v.mainWidth() && v.clickDetails(msg.Y-1) {
			m.keepLens(v)
		}
		return m, nil
	}
	return m, nil
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
		mark, _ := agentMark(info, true, m.icons() == iconsNerd)
		label := fmt.Sprintf("%s%s %d %s · %s", strings.Repeat("  ", info.Depth-1), mark, info.ID, info.Name, info.State)
		options = append(options, huh.NewOption(label, fmt.Sprint(info.ID)))
	}
	return m.showChoices("Subagents", "pick one to see what it does and talk to it", options, func(m model, choice string) (tea.Model, tea.Cmd) {
		return m.pickAgent(choice)
	})
}

// agentTokens is a subagent's context, used of its window, as 4.5k/1M.
func agentTokens(info team.Info) string {
	used := info.Usage.InputTokens + info.Usage.OutputTokens
	if used == 0 && info.Window == 0 {
		return ""
	}
	if info.Window == 0 {
		return shortTokens(used)
	}
	return shortTokens(used) + "/" + shortTokens(info.Window)
}

// shortTokens is a count of tokens in a few characters: 950, 4.5k, 1M.
func shortTokens(count int) string {
	trim := func(value float64, unit string) string {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0") + unit
	}
	switch {
	case count >= 1_000_000:
		return trim(float64(count)/1_000_000, "M")
	case count >= 1000:
		return trim(float64(count)/1000, "k")
	}
	return fmt.Sprint(count)
}
