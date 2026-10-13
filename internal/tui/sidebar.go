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
	sidebarConversion = "new conversation"
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
	colors := sidebarPalette(m.printer == nil || m.printer.isDark())
	width := sidebarWidth - 2
	base := lipgloss.NewStyle().Background(colors.background).Foreground(colors.text)
	paint := func(c color.Color) lipgloss.Style { return base.Foreground(c) }
	var lines []string
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
	for _, todo := range m.todos {
		switch todo.Status {
		case tools.TodoCompleted:
			wrapped(paint(colors.dim).Strikethrough(true), "✓ ", "  ", todo.Text)
		case tools.TodoInProgress:
			wrapped(paint(colors.active).Bold(true), "◐ ", "  ", todo.Text)
		default:
			wrapped(base, "○ ", "  ", todo.Text)
		}
	}
	add("")
	add(paint(colors.title).Bold(true).Render("Background"))
	statuses := []tools.JobStatus{}
	if jobs := m.jobs(); jobs != nil {
		statuses = jobs.List()
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
		add(paint(tint).Render(fmt.Sprintf("%s %d ", mark, status.ID)) + base.Render(oneLine(status.Command, width-4)))
		add(paint(colors.dim).Render("    " + state))
	}

	if len(lines) > height {
		lines = append(lines[:max(height-1, 0)], paint(colors.dim).Render("…"))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	pad := base.Render(" ")
	for i, line := range lines {
		lines[i] = pad + line + base.Render(strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))) + pad
	}
	return strings.Join(lines, "\n")
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
	var lines []string
	for _, status := range jobs.List() {
		lines = append(lines, status.Describe())
	}
	return m.emit(m.printer.Note(strings.Join(lines, "\n") + "\n/kill <n> stops one"))
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
