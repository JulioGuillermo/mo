package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

const (
	sidebarWidth      = 36
	sidebarMinScreen  = 110
	sidebarOff        = "off"
	shownJobs         = 8
	jobRefresh        = time.Second
	jobShownLines     = 20
	sidebarConversion = "new conversation"
	sidebarWheelStep  = 3
)

type jobEndedMsg tools.JobStatus

type jobTickMsg struct{}

// sidebarShown tells whether the side panel has room: it follows what is
// being done, the tasks and the background jobs, beside the conversation.
func (m model) sidebarShown() bool {
	return m.session.Config.Render.Sidebar != sidebarOff && !m.sidebarHidden && m.width >= sidebarMinScreen
}

// mainWidth is the width of the conversation, beside the side panel when
// it shows.
func (m model) mainWidth() int {
	if m.sidebarShown() {
		return m.width - sidebarWidth - 1
	}
	return m.width
}

// fitWidth draws again at the conversation's width after the side panel
// came or went.
func (m *model) fitWidth() tea.Cmd {
	m.printer.setWidth(m.mainWidth())
	m.renderStream(true)
	return m.scheduleRedraw()
}

func (m model) jobs() *tools.Jobs {
	if m.session.Agent == nil || m.session.Agent.Tools == nil {
		return nil
	}
	return m.session.Agent.Tools.Jobs()
}

func (m model) runningJobs() bool {
	if jobs := m.jobs(); jobs != nil {
		for _, status := range jobs.List() {
			if status.State == tools.JobRunning {
				return true
			}
		}
	}
	return false
}

// tickJobs keeps the times of running jobs current while the panel shows
// them.
func (m *model) tickJobs() tea.Cmd {
	if m.jobsTicking || !m.runningJobs() {
		return nil
	}
	m.jobsTicking = true
	return tea.Tick(jobRefresh, func(time.Time) tea.Msg { return jobTickMsg{} })
}

type sidebarColors struct {
	background, title, text, dim, done, active, failed color.Color
}

func sidebarPalette(dark bool) sidebarColors {
	if dark {
		return sidebarColors{
			background: lipgloss.Color("#060e24"), title: accent, text: lipgloss.Color("#c0caf5"), dim: lipgloss.Color("#565f89"),
			done: lipgloss.Color("#9ece6a"), active: warning, failed: lipgloss.Color("#f7768e"),
		}
	}
	return sidebarColors{
		background: lipgloss.Color("#eef1fa"), title: lipgloss.Color("#2e7de9"), text: lipgloss.Color("#3760bf"), dim: lipgloss.Color("#8990b3"),
		done: lipgloss.Color("#587539"), active: lipgloss.Color("#8c6c3e"), failed: lipgloss.Color("#f52a65"),
	}
}

// sidebar draws the side panel at a height: the conversation's title, the
// model's tasks, whole, and the background jobs.
func (m model) sidebar(height int) string {
	panel, _ := m.sidebarLayout(height)
	return panel
}

// sidebarLayout draws the side panel, scrolled as the wheel left it; a
// click on a job shows its last lines, a right click on a job or a task
// opens what can be done with it.
func (m model) sidebarLayout(height int) (string, []region) {
	colors := sidebarPalette(m.printer == nil || m.printer.isDark())
	width := sidebarWidth - 2
	base := lipgloss.NewStyle().Background(colors.background).Foreground(colors.text)
	lines, regions := m.sidebarLines()
	context := m.contextBlock(width, base, colors)
	room := height
	if len(context) > 0 {
		room = max(height-len(context)-1, 1)
	}
	lines, regions = scrolled(lines, regions, room, m.sidebarScroll, base.Foreground(colors.dim))
	for len(lines) < room {
		lines = append(lines, "")
	}
	if len(context) > 0 && height > len(context) {
		lines = append(append(lines, ""), context...)
	}
	pad := base.Render(" ")
	for i, line := range lines {
		lines[i] = pad + line + base.Render(strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))) + pad
	}
	return strings.Join(lines, "\n"), regions
}

// sidebarLines are all the lines of the side panel, before scrolling.
func (m model) sidebarLines() ([]string, []region) {
	colors := sidebarPalette(m.printer == nil || m.printer.isDark())
	width := sidebarWidth - 2
	base := lipgloss.NewStyle().Background(colors.background).Foreground(colors.text)
	paint := func(c color.Color) lipgloss.Style { return base.Foreground(c) }
	var lines []string
	var regions []region
	add := func(line string) { lines = append(lines, line) }
	wrapped := func(style lipgloss.Style, first, rest, text string) {
		for i, piece := range strings.Split(ansi.Wrap(text, width-ansi.StringWidth(first), " "), "\n") {
			prefix := first
			if i > 0 {
				prefix = rest
			}
			add(style.Render(prefix + piece))
		}
	}

	add("")
	wrapped(paint(colors.text).Bold(true), "", "", orDefault(m.thread.title, sidebarConversion))
	add("")
	done, _ := tools.TodoProgress(m.todos)
	header := paint(colors.title).Bold(true).Render("Tasks")
	if len(m.todos) > 0 {
		header += paint(colors.dim).Render(fmt.Sprintf("  %d/%d", done, len(m.todos)))
	}
	add(header)
	if len(m.todos) == 0 {
		add(paint(colors.dim).Render("none yet"))
	}
	for i, todo := range m.todos {
		first := len(lines)
		switch todo.Status {
		case tools.TodoCompleted:
			wrapped(paint(colors.dim).Strikethrough(true), "✓ ", "  ", todo.Text)
		case tools.TodoInProgress:
			wrapped(paint(colors.active).Bold(true), "◐ ", "  ", todo.Text)
		default:
			wrapped(base, "○ ", "  ", todo.Text)
		}
		menu := func(m model) (tea.Model, tea.Cmd) { return m.taskMenu(i) }
		for y := first; y < len(lines); y++ {
			regions = append(regions, region{y: y, x0: 0, x1: 1 << 30, do: menu, menu: menu})
		}
	}
	add("")
	add(paint(colors.title).Bold(true).Render("Background"))
	statuses := []tools.JobStatus{}
	untold := map[int]bool{}
	if jobs := m.jobs(); jobs != nil {
		statuses, untold = jobs.List(), jobs.Untold()
	}
	if len(statuses) == 0 {
		add(paint(colors.dim).Render("no jobs"))
	}
	for i, status := range statuses {
		if i == shownJobs {
			add(paint(colors.dim).Render(fmt.Sprintf("… +%d older", len(statuses)-shownJobs)))
			break
		}
		mark, tint, state := "●", colors.active, "running "+time.Since(status.Started).Round(time.Second).String()
		switch status.State {
		case tools.JobDone:
			mark, tint, state = "✓", colors.done, "done · "+status.Ended.Sub(status.Started).Round(time.Second).String()
		case tools.JobFailed:
			mark, tint, state = "✗", colors.failed, fmt.Sprintf("exit %d · %s", status.ExitCode, status.Ended.Sub(status.Started).Round(time.Second))
		case tools.JobKilled:
			mark, tint, state = "⊘", colors.dim, "killed"
		}
		id := status.ID
		show := func(m model) (tea.Model, tea.Cmd) { return m.showJob(id) }
		menu := func(m model) (tea.Model, tea.Cmd) { return m.jobMenu(id) }
		regions = append(regions, withMenu([]region{row(len(lines), show), row(len(lines)+1, show)}, menu)...)
		add(paint(tint).Render(fmt.Sprintf("%s %d ", mark, status.ID)) + base.Render(oneLine(status.Command, width-4)))
		if untold[status.ID] && !m.running {
			state += " · /jobs tell"
		}
		add(paint(colors.dim).Render("    " + state))
	}

	return lines, regions
}

// showJob prints how a job stands and its last lines.
func (m model) showJob(id int) (tea.Model, tea.Cmd) {
	jobs := m.jobs()
	if jobs == nil {
		return m, nil
	}
	status, output, err := jobs.Output(id, jobShownLines)
	if err != nil {
		return m.emit(m.printer.Error(err))
	}
	return m.emit(m.printer.Note(status.Describe()) + "\n" + m.printer.ToolResult(output, status.State == tools.JobFailed))
}

// jobEnded refreshes the panel and, when the model is idle, lets it react
// to the jobs that ended; while it works it hears of them at its next step.
func (m model) jobEnded(status tools.JobStatus) (tea.Model, tea.Cmd) {
	note := m.printer.Note("background " + status.Describe())
	if m.running || m.dialog != nil || m.settings != nil {
		return m.emit(note)
	}
	updated, cmd := m.reactToJobs()
	shown, print := updated.(model).emit(note)
	return shown, tea.Batch(print, cmd)
}

// reactToJobs starts a turn from the notes of ended jobs, if any.
func (m model) reactToJobs() (tea.Model, tea.Cmd) {
	if m.session.Agent == nil || !m.session.Agent.HasEndedJobs() {
		return m, nil
	}
	return m.start(nil, m.session.Agent.RunEndedJobs)
}

// pickJob lists the background jobs, to kill a running one.
func (m model) listJobs() (tea.Model, tea.Cmd) {
	jobs := m.jobs()
	if jobs == nil || len(jobs.List()) == 0 {
		return m.emit(m.printer.Note("no background jobs"))
	}
	untold := jobs.Untold()
	var lines []string
	for _, status := range jobs.List() {
		line := status.Describe()
		if untold[status.ID] {
			line += " · the model was not told yet"
		}
		lines = append(lines, line)
	}
	hint := "/kill <n> stops one"
	if len(untold) > 0 {
		hint = "/jobs tell lets the model hear the endings now, else it does with your next message · " + hint
	}
	return m.emit(m.printer.Note(strings.Join(lines, "\n") + "\n" + hint))
}

// tellJobs lets the model hear of the jobs that ended, as after a stop it
// does not on its own: in a turn now, instead of with the next message.
func (m model) tellJobs() (tea.Model, tea.Cmd) {
	switch {
	case m.running:
		return m.emit(m.printer.Note("the model is working; it hears of ended jobs at its next step"))
	case m.session.Agent == nil || !m.session.Agent.HasEndedJobs():
		return m.emit(m.printer.Note("no job ended that the model has not heard of"))
	}
	return m.reactToJobs()
}

func (m model) killJob(argument string) (tea.Model, tea.Cmd) {
	var id int
	if _, err := fmt.Sscan(argument, &id); err != nil || m.jobs() == nil {
		return m.emit(m.printer.Note("usage: /kill <job number> · /jobs lists them"))
	}
	jobs := m.jobs()
	return m, func() tea.Msg {
		if err := jobs.Kill(id); err != nil {
			return printMsg(m.printer.Error(err))
		}
		return printMsg(m.printer.Note(fmt.Sprintf("job %d killed", id)))
	}
}

// scrolled shows the panel's lines from scroll on, as many as fit in
// height, saying how many are above and below; regions move with them.
func scrolled(lines []string, regions []region, height, scroll int, dim lipgloss.Style) ([]string, []region) {
	if len(lines) <= height || height < 3 {
		return lines, regions
	}
	scroll = max(0, min(scroll, len(lines)-height+1))
	end := min(scroll+height-1, len(lines))
	if scroll > 0 {
		end = min(scroll+height-2, len(lines))
	}
	var shown []string
	if scroll > 0 {
		shown = append(shown, dim.Render(fmt.Sprintf("↑ %d more · wheel scrolls", scroll)))
	}
	shown = append(shown, lines[scroll:end]...)
	if below := len(lines) - end; below > 0 {
		shown = append(shown, dim.Render(fmt.Sprintf("↓ %d more", below)))
	}
	offset := len(shown) - (end - scroll) - boolInt(len(lines)-end > 0)
	var kept []region
	for _, r := range regions {
		if r.y >= scroll && r.y < end {
			r.y = r.y - scroll + offset
			kept = append(kept, r)
		}
	}
	return shown, kept
}

func boolInt(yes bool) int {
	if yes {
		return 1
	}
	return 0
}

// contextBlock is how full the context is and how fast the model went, at
// the bottom of the panel whatever is scrolled, so the status line can
// leave them out.
func (m model) contextBlock(width int, base lipgloss.Style, colors sidebarColors) []string {
	status := statusPalette(m.printer == nil || m.printer.isDark())
	used, window, share, fill := m.contextShare(status)
	if used == 0 && window == 0 {
		return nil
	}
	paint := func(c color.Color) lipgloss.Style { return base.Foreground(c) }
	var lines []string
	if window > 0 {
		bar := contextBar(share, width-6, fill, status.empty)
		lines = append(lines,
			sidebarBackground(bar, colors.background)+paint(fill).Render(fmt.Sprintf(" %3.0f%%", share*100)),
			paint(colors.dim).Render(fmt.Sprintf("%s of %s tokens", formatTokens(used), formatTokens(window))))
	} else {
		lines = append(lines, paint(colors.dim).Render(formatTokens(used)+" tokens"))
	}
	if speeds := m.speedPart(statusParts{prompt: true, generation: true}, status); speeds != "" {
		lines = append(lines, sidebarBackground(speeds, colors.background))
	}
	return lines
}

// sidebarBackground keeps the panel's background behind text styled with
// a foreground only.
func sidebarBackground(text string, background color.Color) string {
	code := backgroundCode(background)
	text = strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+code)
	return code + strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+code)
}

// scrollSidebar moves the panel by the wheel, as far as it has lines.
func (m *model) scrollSidebar(up bool) {
	if up {
		m.sidebarScroll = max(0, m.sidebarScroll-sidebarWheelStep)
		return
	}
	lines, _ := m.sidebarLines()
	lower, _ := m.lowerLayout()
	height := m.height - lipgloss.Height(lower)
	colors := sidebarPalette(m.printer == nil || m.printer.isDark())
	if context := m.contextBlock(sidebarWidth-2, lipgloss.NewStyle(), colors); len(context) > 0 {
		height -= len(context) + 1
	}
	m.sidebarScroll = max(0, min(m.sidebarScroll+sidebarWheelStep, len(lines)-height+1))
}
