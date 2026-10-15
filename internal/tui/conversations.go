package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

const (
	doubleEscWindow = 600 * time.Millisecond
	shownPrompt     = 70
)

// conversation names the saved conversation the agent holds. A new one gets
// its id when it is first saved; a fork names the one it came from.
type conversation struct {
	id, title, parent, forked string
	created                   time.Time
}

type savedMsg struct{ err error }

type resumeListMsg struct {
	infos []sessions.Info
	err   error
	// latest resumes the newest one at once, as mo -c does.
	latest bool
}

type resumedMsg struct {
	record sessions.Record
	err    error
}

// saveConversation writes the conversation after a turn, off the UI
// goroutine, from a copy of the history so the next turn cannot change it
// while it is written.
func (m *model) saveConversation() tea.Cmd {
	history := slices.Clone(m.session.Agent.History)
	if len(history) == 0 {
		return nil
	}
	now := time.Now()
	m.ensureThread()
	prompts := m.session.Agent.Prompts()
	if len(prompts) > 0 && m.thread.title == "" {
		m.thread.title = sessions.Title(prompts[0].Text)
	}
	record := sessions.Record{History: history, Info: sessions.Info{
		ID: m.thread.id, WorkDir: config.ProjectKey(m.session.WorkDir), Title: m.thread.title,
		Parent: m.thread.parent, Forked: m.thread.forked, Prompts: len(prompts),
		Created: m.thread.created, Updated: now, Usage: m.session.Agent.LastUsage,
	}}
	m.trackThread()
	return func() tea.Msg { return savedMsg{err: sessions.Save(config.SessionDir(), record)} }
}

// ensureThread names a conversation before its first turn, so the commands
// it runs keep their output in its folder; a fork starts with the output of
// the one it came from.
func (m *model) ensureThread() {
	if m.thread.id != "" {
		return
	}
	m.thread.id, m.thread.created = sessions.NewID(), time.Now()
	if m.thread.parent != "" {
		_ = sessions.ShareJobs(config.SessionDir(), m.thread.parent, m.thread.id)
	}
	m.trackThread()
}

// configureJobs tells the jobs where to keep command output and for how
// many turns, as the settings say.
func (m model) configureJobs() {
	if jobs := m.jobs(); jobs != nil {
		jobs.Configure(m.session.Config.Tools.Outputs, m.session.Config.Tools.OutputTurns)
	}
}

// trackTurn tells the jobs which of the user's turns runs, counted in the
// conversation, a new prompt being the next one.
func (m model) trackTurn(newPrompt bool) {
	jobs := m.jobs()
	if jobs == nil || m.session.Agent == nil {
		return
	}
	turn := len(m.session.Agent.Prompts())
	if newPrompt {
		turn++
	}
	jobs.SetTurn(turn)
}

// trackThread tells recall which conversation is in progress, to leave it
// out of what the model searches, and the jobs which one runs commands.
func (m *model) trackThread() {
	if m.session.Agent == nil || m.session.Agent.Tools == nil {
		return
	}
	if recall := m.session.Agent.Tools.Recall(); recall != nil {
		recall.SetCurrent(m.thread.id)
	}
	if jobs := m.session.Agent.Tools.Jobs(); jobs != nil {
		jobs.SetConversation(m.thread.id)
	}
	if uis := m.session.Agent.Tools.UIs(); uis != nil {
		uis.SetConversation(m.thread.id)
	}
}

// listConversations reads the conversations of the working directory.
func listConversations(workDir string, latest bool) tea.Cmd {
	return func() tea.Msg {
		infos, err := sessions.List(config.SessionDir(), config.ProjectKey(workDir))
		return resumeListMsg{infos: infos, err: err, latest: latest}
	}
}

func loadConversation(id string) tea.Cmd {
	return func() tea.Msg {
		record, err := sessions.Load(config.SessionDir(), id)
		return resumedMsg{record: record, err: err}
	}
}

// pickConversation lists the saved conversations to resume, forks under the
// name of what they came from.
func (m model) pickConversation(msg resumeListMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(msg.err)
	}
	if len(msg.infos) == 0 {
		return m.emit(m.printer.Note("no saved conversations in this folder yet"))
	}
	if msg.latest {
		return m, loadConversation(msg.infos[0].ID)
	}
	titles := map[string]string{}
	for _, info := range msg.infos {
		titles[info.ID] = info.Title
	}
	var options []huh.Option[string]
	for _, info := range msg.infos {
		label := errorStyle.Render(removeMark) + " " + orDefault(info.Title, "(no prompt)")
		if info.Parent != "" {
			label = "↳ " + label + dimStyle.Render(" · fork of "+orDefault(titles[info.Parent], "an older one"))
		}
		label += dimStyle.Render(fmt.Sprintf(" · %d prompts · %s", info.Prompts, ago(info.Updated)))
		if info.ID == m.thread.id {
			label += keyStyle.Render(" · this one")
		}
		options = append(options, huh.NewOption(label, info.ID))
	}
	picked := msg.infos[0].ID
	list := huh.NewSelect[string]().Title("Resume a conversation").
		Description("saved in " + config.SessionDir() + " · the current one is saved already").
		Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) { return m, loadConversation(picked) }
	back := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	remove := func(m model) (tea.Model, tea.Cmd) { return m.removeFromList(list, msg.infos) }
	form := huh.NewForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: resumeHelp, remove: remove})
}

// resume puts a saved conversation in place of the current one, which was
// saved after its last turn.
func (m model) resume(msg resumedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(msg.err)
	}
	if m.running {
		return m.emit(m.printer.Note("stop the current run first"))
	}
	record := msg.record
	m.worked = 0
	m.session.Agent.Restore(record.History, record.Usage)
	m.usage = record.Usage
	m.thread = conversation{id: record.ID, title: record.Title, parent: record.Parent, forked: record.Forked, created: record.Created}
	m.trackThread()
	m.replay(record.History)
	m.scrolled = false
	return m.emit(m.printer.Note(fmt.Sprintf("resumed: %s · %d prompts · %s", orDefault(record.Title, record.ID), record.Prompts, ago(record.Updated))))
}

// fork goes on in a copy of the conversation; the original stays as it was
// saved, to resume.
func (m model) fork() (tea.Model, tea.Cmd) {
	if len(m.session.Agent.History) == 0 {
		return m.emit(m.printer.Note("nothing to fork yet"))
	}
	m.thread = conversation{parent: m.thread.id, title: m.thread.title}
	save := m.saveConversation()
	updated, note := m.emit(m.printer.Note("forked: you go on in a copy; the original stays in /resume"))
	return updated, tea.Batch(save, note)
}

// pickPrompt lists the user's prompts, the latest first, to go back to one.
func (m model) pickPrompt() (tea.Model, tea.Cmd) {
	if m.running {
		return m.emit(m.printer.Note("stop the current run first"))
	}
	prompts := m.session.Agent.Prompts()
	if len(prompts) == 0 {
		return m.emit(m.printer.Note("no prompts to go back to"))
	}
	var options []huh.Option[string]
	for i := len(prompts) - 1; i >= 0; i-- {
		options = append(options, huh.NewOption("› "+oneLine(prompts[i].Text, shownPrompt), fmt.Sprint(i)))
	}
	picked := fmt.Sprint(len(prompts) - 1)
	list := huh.NewSelect[string]().Title("Go back to a prompt").
		Description("it comes back into the input to edit and send · what follows it stays in /resume · files changed since stay changed").
		Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) {
		var index int
		fmt.Sscan(picked, &index)
		return m.rewind(prompts[index])
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	form := huh.NewForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: menuHelp})
}

// rewind goes on from before a prompt, in a fork, with the prompt in the
// input to edit. The fork is saved once its first turn ends.
func (m model) rewind(prompt agent.Prompt) (tea.Model, tea.Cmd) {
	m.thread = conversation{parent: m.thread.id, title: m.thread.title, forked: sessions.Title(prompt.Text)}
	m.session.Agent.Rewind(prompt.Index)
	m.usage = llm.Usage{}
	m.replay(m.session.Agent.History)
	m.input.SetValue(prompt.Text)
	m.input.CursorEnd()
	m.scrolled = false
	return m.emit(m.printer.Note("↩ back before this prompt, in a fork: edit it and send · the conversation as it was stays in /resume"))
}

// newConversation starts over; the old one stays saved.
func (m *model) newConversation() {
	m.worked = 0
	m.session.Agent.Clear()
	m.usage = llm.Usage{}
	m.thread = conversation{}
	m.trackThread()
	m.todos = nil
}

func oneLine(text string, length int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > length {
		return string(runes[:length-1]) + "…"
	}
	return text
}

func ago(when time.Time) string {
	elapsed := time.Since(when)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%d min ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(elapsed.Hours()))
	case elapsed < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(elapsed.Hours()/24))
	}
	return when.Format("2006-01-02")
}

// whileRunning are the commands that leave the conversation alone, so they
// work while the model does: settings saved then apply when it finishes.
var whileRunning = map[string]bool{
	"help": true, "settings": true, "providers": true, "mcp": true, "plugins": true, "notify": true,
	"sidebar": true, "vim": true, "jobs": true, "kill": true, "view": true, "quit": true, "exit": true, "title": true, "model": true, "login": true, "logout": true,
}

// queue keeps a message written while the model works for its next step,
// and shows it now.
func (m model) queue(prompt string) (tea.Model, tea.Cmd) {
	m.session.Agent.Queue(m.withAttachments(prompt))
	m.scrolled = false
	return m, tea.Batch(drawText(func() string { return m.printer.User(prompt) }),
		printText(m.printer.Note("queued · the model reads it at its next step")))
}

// saveOrWait writes the settings now; while the model works the session
// built from them waits for the turn's end, as swapping it under a running
// agent would pull it from under its feet.
func (m model) saveOrWait(screen *settingsScreen) (tea.Model, tea.Cmd) {
	m.settingsInFlight = true
	return m, saveSettings(screen, m.session, m.running)
}
