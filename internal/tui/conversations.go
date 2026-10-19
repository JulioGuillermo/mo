package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
	record, ok := m.conversationRecord(m.currentHistory())
	if !ok {
		return nil
	}
	m.saves++
	number := m.saves
	return func() tea.Msg { return savedMsg{err: saveInOrder(number, record)} }
}

// currentHistory is the main agent's history: as it is when it is idle,
// else as its last step left it, since its goroutine changes it meanwhile.
func (m model) currentHistory() []llm.Message {
	if m.running {
		return slices.Clone(m.stepHistory)
	}
	return slices.Clone(m.session.Agent.History)
}

var (
	savingMu sync.Mutex
	// lastSaves are the numbers of the last saves written, by conversation.
	lastSaves = map[string]int{}
)

// saveInOrder writes saves one at a time, a save of a conversation older
// than one written already left out, so a late one never undoes a newer.
func saveInOrder(number int, record sessions.Record) error {
	savingMu.Lock()
	defer savingMu.Unlock()
	if number < lastSaves[record.ID] {
		return nil
	}
	lastSaves[record.ID] = number
	return sessions.Save(config.SessionDir(), record)
}

// saveEvery is how often a turn's steps are saved at most.
const saveEvery = time.Second

type saveTickMsg struct{}

// historyMsg is the main agent's history after a step of its turn.
type historyMsg []llm.Message

// scheduleSave saves soon, once for the steps that come meanwhile.
func (m *model) scheduleSave() tea.Cmd {
	if m.saveTicking {
		return nil
	}
	m.saveTicking = true
	return tea.Tick(saveEvery, func(time.Time) tea.Msg { return saveTickMsg{} })
}

// conversationRecord is the conversation to save, with history as its
// main agent's; nothing for an empty one.
func (m *model) conversationRecord(history []llm.Message) (sessions.Record, bool) {
	if len(history) == 0 {
		return sessions.Record{}, false
	}
	now := time.Now()
	m.ensureThread()
	prompts := agent.PromptsIn(history)
	if len(prompts) > 0 && m.thread.title == "" {
		m.thread.title = sessions.Title(prompts[0].Text)
	}
	var agents []sessions.AgentRecord
	if m.session.Team != nil {
		agents = m.session.Team.Records()
	}
	record := sessions.Record{History: history, Agents: agents, Info: sessions.Info{
		ID: m.thread.id, WorkDir: config.ProjectKey(m.session.WorkDir), Title: m.thread.title,
		Parent: m.thread.parent, Forked: m.thread.forked, Prompts: len(prompts),
		Created: m.thread.created, Updated: now, Usage: m.session.Agent.LastUsage,
	}}
	m.trackThread()
	return record, true
}

const (
	forkPrefix    = "fork: "
	compactPrefix = "compact: "
)

// baseTitle is a title without the fork: or compact: a conversation that came
// from another carries, as many as there are.
func baseTitle(title string) string {
	for {
		switch {
		case strings.HasPrefix(title, forkPrefix):
			title = strings.TrimPrefix(title, forkPrefix)
		case strings.HasPrefix(title, compactPrefix):
			title = strings.TrimPrefix(title, compactPrefix)
		default:
			return title
		}
	}
}

// derivedTitle is the title of a conversation that came from another: fork:
// or compact: and the title the old one had, without the prefix it had. Until
// the old one has a title it has none either, and is named by its first prompt
// as any.
func derivedTitle(prefix, title string) string {
	if base := baseTitle(title); base != "" {
		return prefix + base
	}
	return ""
}

// derivedFrom tells whether a title still says it came from another conversation.
func derivedFrom(title string) bool {
	return strings.HasPrefix(title, forkPrefix) || strings.HasPrefix(title, compactPrefix)
}

// contextCut keeps the conversation as it was when the agent cut its history
// down to make room, summed up or dropped, and goes on in a conversation of its
// own that names it as its parent, titled compact: and the old title: what was
// said before is not lost, and /resume goes back to it.
func (m model) contextCut(before contextCutMsg) (tea.Model, tea.Cmd) {
	m.ensureThread()
	record, kept := m.conversationRecord(before)
	var save tea.Cmd
	if kept {
		m.saves++
		number := m.saves
		save = func() tea.Msg { return savedMsg{err: saveInOrder(number, record)} }
	}
	m.thread = conversation{parent: m.thread.id, title: derivedTitle(compactPrefix, m.thread.title)}
	m.ensureThread()
	// What the last step left is the history as it was: the next step, which
	// follows at once, brings the cut one.
	m.stepHistory = nil
	updated, note := m.emit(m.printer.Note("the conversation as it was stays in /resume · you go on in a new one that names it"))
	return updated, tea.Batch(save, note)
}

// saveOnExit saves the conversation as mo exits: its subagents stopped and
// given a moment to end, the main agent's history as it ended, or as its
// last step left it when it did not end in time.
func (m model) saveOnExit(finished bool) {
	if m.session.Team != nil {
		m.session.Team.StopAll()
		m.session.Team.WaitIdle(exitWait)
	}
	history := slices.Clone(m.stepHistory)
	if finished {
		history = slices.Clone(m.session.Agent.History)
	}
	if record, ok := m.conversationRecord(history); ok {
		m.saves++
		_ = saveInOrder(m.saves, record)
	}
}

// exitWait is how long mo waits on exit for its subagents to end.
const exitWait = 2 * time.Second

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
			label = "↳ " + label
			// A title that says what it is, fork: or compact:, says it all; one
			// the user changed says where it came from.
			if !derivedFrom(info.Title) {
				label += dimStyle.Render(" · fork of " + orDefault(titles[info.Parent], "an older one"))
			}
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
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
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
	if m.session.Team != nil {
		m.session.Team.Restore(record.Agents)
	}
	m.usage = record.Usage
	m.thread = conversation{id: record.ID, title: record.Title, parent: record.Parent, forked: record.Forked, created: record.Created}
	m.trackThread()
	uploads := m.replay(record.History)
	m.scrolled = false
	updated, cmd := m.emit(m.printer.Note(fmt.Sprintf("resumed: %s · %d prompts · %s", orDefault(record.Title, record.ID), record.Prompts, ago(record.Updated))))
	return updated, tea.Batch(cmd, sendRaw(uploads))
}

// fork goes on in a copy of the conversation; the original stays as it was
// saved, to resume.
func (m model) fork() (tea.Model, tea.Cmd) {
	if len(m.session.Agent.History) == 0 {
		return m.emit(m.printer.Note("nothing to fork yet"))
	}
	m.thread = conversation{parent: m.thread.id, title: derivedTitle(forkPrefix, m.thread.title)}
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
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: menuHelp})
}

// rewind goes on from before a prompt, in a fork, with the prompt in the
// input to edit. The fork is saved once its first turn ends.
func (m model) rewind(prompt agent.Prompt) (tea.Model, tea.Cmd) {
	m.thread = conversation{parent: m.thread.id, title: derivedTitle(forkPrefix, m.thread.title), forked: sessions.Title(prompt.Text)}
	m.session.Agent.Rewind(prompt.Index)
	m.usage = llm.Usage{}
	uploads := m.replay(m.session.Agent.History)
	m.input.SetValue(prompt.Text)
	m.input.CursorEnd()
	m.scrolled = false
	updated, cmd := m.emit(m.printer.Note("↩ back before this prompt, in a fork: edit it and send · the conversation as it was stays in /resume"))
	return updated, tea.Batch(cmd, sendRaw(uploads))
}

// newConversation starts over; the old one stays saved.
func (m *model) newConversation() {
	m.worked = 0
	m.session.Agent.Clear()
	if m.session.Team != nil {
		m.session.Team.Restore(nil)
	}
	m.usage = llm.Usage{}
	m.thread = conversation{}
	m.trackThread()
	m.todos = nil
	// The screen starts clear too: what was said stays in /resume.
	m.blocks = nil
	m.scrolled = false
	m.vim.line = 0
	m.uiOpen = 0
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
	"sidebar": true, "vim": true, "jobs": true, "agents": true, "kill": true, "view": true, "git": true, "peers": true, "quit": true, "exit": true, "title": true, "model": true, "effort": true, "login": true, "logout": true,
}

// queue keeps a message written while the model works for its next step.
// It waits over the status line, where it is in view whatever the model
// is writing, and goes into the conversation where the model reads it.
func (m model) queue(prompt string) (tea.Model, tea.Cmd) {
	m.session.Agent.Queue(m.withAttachments(prompt))
	m.queued = append(m.queued, prompt)
	m.scrolled = false
	return m, nil
}

// sendNow stops the model where it is so that it answers what is queued, and
// what is typed, now. With nothing queued or typed it does nothing.
func (m model) sendNow() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if strings.TrimSpace(m.input.Value()) != "" {
		var next tea.Model
		next, cmd = m.submit()
		m = next.(model)
	}
	if len(m.queued) == 0 || !m.running {
		return m, cmd
	}
	m.sendingNow = true
	m.stop()
	return m, cmd
}

// queuedRead puts the queued messages the model now reads into the
// conversation, after what it did before reading them.
func (m model) queuedRead(count int) (tea.Model, tea.Cmd) {
	count = min(count, len(m.queued))
	read := m.queued[:count]
	m.queued = slices.Clone(m.queued[count:])
	if len(read) == 0 {
		return m, nil
	}
	// The model reads them as one message, so they show as one.
	prompt := joinQueued(read)
	return m, drawText(func() string { return m.printer.User(prompt) })
}

// joinQueued is the messages waiting as the model reads them: one, a blank
// line between them.
func joinQueued(prompts []string) string {
	return strings.Join(prompts, "\n\n")
}

// queuedRows is the most lines the messages of the queue take over the status line.
const queuedRows = 8

// queuedBackground is the shade behind the queue, warmer than the panel of
// what the user wrote so the two are told apart.
func queuedBackground(dark bool) color.Color {
	if dark {
		return lipgloss.Color("#2f2819")
	}
	return lipgloss.Color("#f3e5c4")
}

// queuedDim is the colour of the last line's counts and hint, which are
// said softly without Faint: some terminals take it from the background too.
func queuedDim(dark bool) color.Color {
	if dark {
		return lipgloss.Color("#9a8f78")
	}
	return lipgloss.Color("#7a6a4a")
}

// queuedIcon is the mark of a message waiting, of the icon set: a speech
// bubble in Nerd Font or as an emoji, » in plain text.
func queuedIcon(set string) string {
	switch set {
	case iconsEmoji:
		return "💬"
	case iconsText:
		return "»"
	}
	return "\uf075"
}

// queuedView is the messages waiting for the model, as one shaded entry: they
// are read as one message, and up with nothing typed brings them all back.
// Each starts with a sign and takes the lines it needs inside the main panel,
// up to queuedRows; a last line says what waits and what is not shown.
func (m model) queuedView() string {
	if len(m.queued) == 0 {
		return ""
	}
	dark := m.printer == nil || m.printer.isDark()
	paint := lipgloss.NewStyle().Background(queuedBackground(dark))
	edge := paint.Foreground(warning).Render("▌")
	sign := " " + queuedIcon(m.treeIconSet()) + " "
	indent := strings.Repeat(" ", ansi.StringWidth(sign))
	width := max(m.mainWidth(), 20)
	body := max(width-1-ansi.StringWidth(sign)-1, 10)

	// Each piece carries the shade itself: the edge ends with a reset, which
	// would take it from what follows.
	row := func(content string) string {
		fill := max(width-1-ansi.StringWidth(content), 0)
		return edge + paint.Render(content+strings.Repeat(" ", fill))
	}

	var rows []string
	for _, prompt := range m.queued {
		lead := sign
		for _, line := range strings.Split(strings.TrimSpace(prompt), "\n") {
			for _, part := range strings.Split(ansi.Wrap(strings.TrimRight(line, " \t\r"), body, ""), "\n") {
				rows = append(rows, row(lead+part))
				lead = indent
			}
		}
	}
	hidden := 0
	if len(rows) > queuedRows {
		hidden = len(rows) - queuedRows
		rows = rows[:queuedRows]
	}
	rest := fmt.Sprintf(" · %d", len(m.queued))
	if hidden > 0 {
		rest += fmt.Sprintf(" · … +%d lines", hidden)
	}
	rest += " · ↑ edit"
	if ansi.StringWidth(" ⧗ queued"+rest+" · ctrl+s send now")+1 <= width {
		rest += " · ctrl+s send now"
	} else if ansi.StringWidth(" ⧗ queued"+rest+" · ^s now")+1 <= width {
		rest += " · ^s now"
	}

	label := paint.Foreground(warning).Bold(true).Render(" ⧗ queued")
	tail := paint.Foreground(queuedDim(dark)).Render(rest)
	fill := paint.Render(strings.Repeat(" ", max(width-1-ansi.StringWidth(" ⧗ queued"+rest), 0)))
	rows = append(rows, edge+label+tail+fill)
	return strings.Join(rows, "\n")
}

// saveOrWait writes the settings now; while the model works the session
// built from them waits for the turn's end, as swapping it under a running
// agent would pull it from under its feet.
func (m model) saveOrWait(screen *settingsScreen) (tea.Model, tea.Cmd) {
	m.settingsInFlight = true
	return m, saveSettings(screen, m.session, m.running)
}
