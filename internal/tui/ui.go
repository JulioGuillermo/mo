// Package tui is mo's interactive terminal UI. It runs inline: finished output
// goes to the normal terminal scrollback and only the prompt, the streaming
// answer and the status line are redrawn.
package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/gitops"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/notify"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/sessions"
	"github.com/jg/mo/internal/tools"
)

type Session struct {
	Agent    *agent.Agent
	Provider string
	Model    string
	Config   config.Config
	WorkDir  string
	// Reload builds a session from the user file given and the project's:
	// the one given, which the user made, or the one on disk when nil.
	Reload    func(userLayer config.Config, projectLayer *config.Config) (Session, error)
	MCP       []mcphub.Status
	Plugins   *plugins.Set
	ToolNames []string
	// Skills names every skill found, also the ones turned off.
	Skills []string
	// Instructions are the instructions files read into the system prompt.
	Instructions []string
	// WindowFrom says where the context window came from.
	WindowFrom string

	ToolDescriptions map[string]string

	// Continue resumes the latest conversation of the working directory.
	Continue bool

	Activate func()
	Discard  func()
}

// activate is called once a reloaded session replaces the one in use, and
// discard when it is dropped instead.
func (s Session) activate() {
	if s.Activate != nil {
		s.Activate()
	}
}

func (s Session) discard() {
	if s.Discard != nil {
		s.Discard()
	}
}

type model struct {
	session Session
	bridge  *bridge
	printer *printer

	input   textarea.Model
	spinner spinner.Model
	width   int
	height  int

	running   bool
	cancel    context.CancelFunc
	finished  chan struct{}
	stream    string
	streamCut int
	// forming are the tool calls the model is writing now.
	forming []formingCall
	// activity is what the agent waits on, said by the spinner.
	activity string
	// pendingSession is built from settings saved while a turn ran, to take
	// over when it ends.
	pendingSession *Session
	// lastEvent is the kind of what the model sent last, and runningTool
	// the call running now: they say what the agent is doing.
	lastEvent llm.EventKind
	// branch is the git branch of the working directory, as last read.
	branch string
	// changes are the files changed in the working tree, as last read;
	// repoReading and repoStale keep one read at a time.
	changes     []gitops.Change
	repoReading bool
	repoStale   bool
	// history is what was sent before, for the arrows to bring back.
	history inputHistory
	// titling is the title the model is working out, after /title auto.
	titling *titling
	// promptRead is how far the server is in reading the prompt, when it
	// says so.
	promptRead  llm.PromptProgress
	runningTool *llm.ToolCall
	streamDone  string
	reasoning   string
	dialog      *dialog
	usage       llm.Usage
	speed       llm.Usage
	settings    *settingsScreen
	// viewer shows a job's output over the whole screen while it is open.
	viewer *outputViewer
	// vim is where vim keys are, when they are on.
	vim vimState
	// popup is a small menu over the screen; clickedAt is where the right
	// click that opens one was, clicked unset when no click opened it.
	popup            *popup
	clickedAt        point
	clicked          bool
	settingsInFlight bool

	suggestion   int
	blocks       []block
	redraws      int
	attachments  map[string]string
	pastedImages int
	showDetails  bool
	thread       conversation
	lastEsc      time.Time
	// focused is whether the terminal has the focus, as it reports it; mo
	// starts as if it had.
	focused bool
	// todos is the model's task list, from its latest todo call.
	todos []tools.Todo
	// sidebarHidden is /sidebar turning the side panel off for the session.
	sidebarHidden bool
	// sidebarScroll is how many of the panel's lines the wheel scrolled past.
	sidebarScroll int
	// openSection is the settings section to open once settings load, as
	// a click on the model asks.
	openSection string
	// selected is text being picked with the mouse; flash says for a moment
	// what was copied.
	selected    *selection
	flash       string
	flashes     int
	jobsTicking bool
	viewport    viewport.Model
	scrolled    bool
}

const agentShutdownWait = 3 * time.Second

func Run(session Session) error {
	printer := newPrinter()
	printer.setImages(render.Detect(session.Config.Render.Images))
	if screen := screenFor(session.Config.Render.Background); screen.set {
		printer.setDark(screen.dark)
	}
	printer.workDir = session.WorkDir
	bridge := &bridge{printer: printer, exited: make(chan struct{})}
	m := model{
		session:  session,
		bridge:   bridge,
		printer:  printer,
		input:    newInput(),
		viewport: viewport.New(),
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(workingStyle)),
		focused:  true,
		history:  loadHistory(),
		vim:      vimState{enabled: session.Config.Render.Keys == "vim"},
	}
	printer.setKind(m.toolKind)
	program := tea.NewProgram(m)
	bridge.program = program
	if titles := session.Agent.Tools.Titles(); titles != nil {
		titles.OnSet(func(title string) { bridge.send(titleSetMsg(title)) })
	}
	if jobs := session.Agent.Tools.Jobs(); jobs != nil {
		jobs.OnEnd(func(status tools.JobStatus) { bridge.send(jobEndedMsg(status)) })
		jobs.OnStart(func(status tools.JobStatus) { bridge.send(jobStartedMsg(status)) })
	}
	final, err := program.Run()
	close(bridge.exited)
	if last, ok := final.(model); ok && last.running {
		last.stop()
		select {
		case <-last.finished:
		case <-time.After(agentShutdownWait):
		}
	}
	return err
}

func newInput() textarea.Model {
	input := textarea.New()
	input.Prompt = "┃ "
	input.Placeholder = "ask anything · /help"
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight = 1
	// MaxHeight is how tall it grows; MaxContentHeight, set far above it, is
	// what lets it hold more lines than that, scrolled inside, since bubbles
	// would stop new lines at MaxHeight without it.
	input.MaxHeight = 10
	input.MaxContentHeight = 1 << 20
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	input.SetStyles(inputStyles(input.Styles(), true))
	input.Focus()
	return input
}

// inputStyles shades the whole input, every line to its end.
func inputStyles(styles textarea.Styles, dark bool) textarea.Styles {
	shade := panel(dark)
	for _, state := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		state.Base = shade
		state.Text = shade
		state.CursorLine = shade
		state.Placeholder = shade.Faint(true)
		state.EndOfBuffer = shade
		state.Prompt = shade.Foreground(accent).Bold(true)
	}
	return styles
}

func (m model) Init() tea.Cmd {
	banner := m.printer.Note(fmt.Sprintf("mo · %s · %s · /help", m.session.Provider, shortModelName(m.session.Model)))
	for _, status := range m.session.Plugins.Status() {
		if status.Err != nil {
			banner += "\n" + m.printer.Error(fmt.Errorf("plugin %s: %v", status.Name, status.Err))
		}
	}
	if user, err := config.ReadUserLayer(); err == nil {
		if notice := projectNotice(m.session.WorkDir, user); notice != "" {
			banner += "\n" + m.printer.Note(notice)
		}
	}
	start := printText(banner)
	if m.session.Continue {
		start = tea.Sequence(start, listConversations(m.session.WorkDir, true))
	}
	return tea.Batch(start, tea.RequestBackgroundColor, textarea.Blink, m.readRepo())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		return m.openSettings(msg)
	case settingsSavedMsg:
		return m.finishSettings(msg)
	case deletedMsg:
		return m.conversationsDeleted(msg)
	case modelsMsg:
		return m.showModels(msg)
	}
	if m.settings != nil && !isAgentMsg(msg) {
		return m.updateSettings(msg)
	}
	if m.popup != nil && !isAgentMsg(msg) {
		if _, tick := msg.(viewerTickMsg); !tick {
			return m.updatePopup(msg)
		}
	}
	if _, tick := msg.(viewerTickMsg); (tick || m.viewer != nil) && !isAgentMsg(msg) {
		if m.viewer == nil {
			return m, nil
		}
		return m.updateViewer(msg)
	}
	switch msg := msg.(type) {
	case printMsg:
		return m.emit(string(msg))
	case clipboardMsg:
		if msg.err != nil {
			return m.emitError(msg.err)
		}
		if msg.image != "" {
			m.attach([]string{msg.image}, true)
		}
		m.attach(msg.files, false)
		return m, nil
	case tea.PasteMsg:
		if m.dialog != nil {
			return m.pasteIntoDialog(msg)
		}
		if m.pastedPaths(msg.Content) {
			return m, nil
		}
	case rulesSavedMsg:
		return m.finishRules(msg)
	case jobEndedMsg:
		return m.jobEnded(tools.JobStatus(msg))
	case jobTickMsg:
		m.jobsTicking = false
		return m, m.tickJobs()
	case jobStartedMsg:
		return m, m.tickJobs()
	case vimPasteMsg:
		return m.vimPasted(msg)
	case loginMsg:
		return m.loggedIn(msg)
	case titledMsg:
		return m.titled(msg)
	case titleSetMsg:
		return m.setTitle(sessions.Title(string(msg)))
	case titleEventMsg:
		if m.titling != nil {
			m.titling.follow(llm.Event(msg))
		}
		return m, nil
	case savedMsg:
		if msg.err != nil {
			return m.emitError(fmt.Errorf("the conversation was not saved: %w", msg.err))
		}
		return m, nil
	case resumeListMsg:
		return m.pickConversation(msg)
	case resumedMsg:
		return m.resume(msg)
	case reasoningMsg:
		m.addReasoning(string(msg))
		return m, nil
	case tea.MouseClickMsg:
		switch msg.Button {
		case tea.MouseLeft:
			return m.press(msg.X, msg.Y)
		case tea.MouseRight:
			return m.rightClick(msg.X, msg.Y)
		}
		return m, nil
	case tea.MouseMotionMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		return m.drag(msg.X, msg.Y)
	case tea.MouseReleaseMsg:
		return m.release(msg.X, msg.Y)
	case flashDoneMsg:
		if int(msg) == m.flashes {
			m.flash = ""
		}
		return m, nil
	case toolStartMsg:
		m.followTodos(msg.call)
		m.addTool(msg.call, msg.summary)
		running := msg.call
		m.runningTool = &running
		return m, nil
	case toolDoneMsg:
		m.runningTool = nil
		m.finishTool(msg.id, msg.output, msg.isError)
		read := m.readRepo()
		return m, tea.Batch(m.tickJobs(), read)
	case tea.MouseWheelMsg:
		if m.sidebarShown() && msg.X > m.mainWidth() {
			m.scrollSidebar(msg.Button == tea.MouseWheelUp)
			return m, nil
		}
		m.scroll(msg.Button == tea.MouseWheelUp, 3)
		return m, nil
	case tea.WindowSizeMsg:
		return m, m.resize(msg)
	case redrawMsg:
		if int(msg) == m.redraws {
			m.redraw()
		}
		return m, nil
	case drawMsg:
		return m.emitDrawn(msg)
	case tea.BackgroundColorMsg:
		return m, m.applyScreen(msg.IsDark())
	case tea.KeyPressMsg:
		m.focused = true
		return m.handleKey(msg)
	case streamMsg:
		m.lastEvent = msg.Kind
		switch msg.Kind {
		case llm.EventToolCall:
			m.growCall(msg.Call, msg.Name, msg.Text)
		case llm.EventPromptProgress:
			m.promptRead = msg.Progress
		case llm.EventReasoning:
			m.reasoning += msg.Text
		default:
			m.stream += msg.Text
			m.renderStream(false)
		}
		return m, nil
	case streamEndMsg:
		m.clearStream()
		return m, nil
	case activityMsg:
		m.activity = string(msg)
		return m, nil
	case usageMsg:
		m.usage = llm.Usage(msg)
		if msg.GenerationRate > 0 || msg.PromptRate > 0 {
			m.speed = llm.Usage(msg)
		}
		return m, nil
	case interactionMsg:
		return m.openDialog(msg)
	case tea.FocusMsg:
		m.focused = true
		read := m.readRepo()
		return m, read
	case repoMsg:
		again := m.repoRead(msg)
		return m, again
	case tea.BlurMsg:
		m.focused = false
		return m, nil
	case runDoneMsg:
		finished, cmd := m.finishRun(msg.err)
		done := finished.(model)
		read := done.readRepo()
		return done, tea.Batch(cmd, read)
	case spinner.TickMsg:
		if !m.running && m.titling == nil {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.suggestion = 0
	}
	return m, cmd
}

// resize fits everything to the new size: what is being written at once,
// what was printed once the resizing stops.
func (m *model) resize(size tea.WindowSizeMsg) tea.Cmd {
	widthChanged := size.Width != m.width
	m.width, m.height = size.Width, size.Height
	m.input.SetWidth(size.Width)
	m.printer.setWidth(m.mainWidth())
	m.renderStream(true)
	if !widthChanged {
		return nil
	}
	return m.scheduleRedraw()
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.dialog != nil {
		return m.handleDialogKey(msg)
	}
	if m.vimOn() {
		if handled, next, cmd := m.vimKey(msg); handled {
			return next, cmd
		}
	}
	return m.plainKey(msg)
}

// plainKey handles a key as mo does without vim keys, as vim's normal mode
// does too for the keys it passes on.
func (m model) plainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if handled, next, cmd := m.historyKey(msg.String()); handled {
		return next, cmd
	}
	if matches := m.suggestions(); len(matches) > 0 {
		if handled, next, cmd := m.handleSuggestionKey(msg.String(), matches); handled {
			return next, cmd
		}
	}
	switch msg.String() {
	case "ctrl+v":
		return m, pasteClipboard()
	case "backspace", "delete":
		if m.eraseAttachment(msg.String()) {
			return m, nil
		}
	case "shift+tab":
		if policy := m.session.Agent.Permissions; policy != nil {
			policy.SetMode(policy.Mode().Next())
		}
		return m, nil
	case "ctrl+o":
		m.toggleDetails()
		return m, nil
	case "ctrl+b":
		return m.toBackground()
	case "pgup", "shift+up":
		m.scroll(true, m.pageSize(msg.String()))
		return m, nil
	case "pgdown", "shift+down":
		m.scroll(false, m.pageSize(msg.String()))
		return m, nil
	case "ctrl+c":
		if m.running {
			m.stop()
			return m, nil
		}
		if m.input.Value() != "" {
			m.input.Reset()
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.running {
			m.stop()
			return m, nil
		}
		if m.titling != nil {
			m.titling.cancel()
			return m, nil
		}
		if m.input.Value() == "" && time.Since(m.lastEsc) < doubleEscWindow {
			m.lastEsc = time.Time{}
			return m.pickPrompt()
		}
		m.lastEsc = time.Now()
		return m, nil
	case "ctrl+d":
		if m.input.Value() == "" {
			if m.running {
				m.stop()
			}
			return m, tea.Quit
		}
	case "enter":
		if m.settingsInFlight {
			return m, nil
		}
		return m.submit()
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.suggestion = 0
	}
	return m, cmd
}

// handleSuggestionKey moves through the command suggestions and takes one
// with tab or enter. Enter runs a command that needs nothing more.
func (m model) handleSuggestionKey(key string, matches []command) (bool, tea.Model, tea.Cmd) {
	selected := matches[m.suggestion%len(matches)]
	switch key {
	case "up":
		m.suggestion = (m.suggestion + len(matches) - 1) % len(matches)
	case "down":
		m.suggestion = (m.suggestion + 1) % len(matches)
	case "tab":
		m.completeCommand(selected)
	case "enter":
		if m.input.Value() == "/"+selected.name {
			return false, m, nil
		}
		if !m.completeCommand(selected) || m.settingsInFlight {
			return true, m, nil
		}
		next, cmd := m.submit()
		return true, next, cmd
	default:
		return false, m, nil
	}
	return true, m, nil
}

func (m model) submit() (tea.Model, tea.Cmd) {
	prompt := strings.TrimSpace(m.input.Value())
	if prompt == "" {
		return m, nil
	}
	m.input.Reset()
	if keep := m.history.add(prompt); keep != nil {
		next, cmd := m.submitted(prompt)
		return next, tea.Batch(cmd, keep)
	}
	return m.submitted(prompt)
}

// submitted runs what was sent: a command, a message queued while the
// model works, or a turn.
func (m model) submitted(prompt string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(prompt, "/") {
		return m.runCommand(prompt)
	}
	if m.running {
		return m.queue(prompt)
	}

	m.scrolled = false
	echo := drawText(func() string { return m.printer.User(prompt) })
	sent := m.withAttachments(prompt)
	return m.start(echo, func(ctx context.Context, hooks agent.Hooks) error {
		return m.session.Agent.Run(ctx, sent, hooks)
	})
}

// start runs work on the agent in the background, after first, until it
// ends or is stopped.
func (m model) start(first tea.Cmd, work func(context.Context, agent.Hooks) error) (tea.Model, tea.Cmd) {
	m.ensureThread()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	m.running = true
	m.cancel = cancel
	m.finished = finished
	hooks := m.bridge.hooks()
	runAgent := func() tea.Msg {
		defer close(finished)
		return runDoneMsg{err: work(ctx, hooks)}
	}
	return m, tea.Batch(tea.Sequence(first, runAgent), m.spinner.Tick)
}

// runCommand dispatches on the command's first word, so mo's own commands
// always win over a plugin's.
func (m model) runCommand(command string) (tea.Model, tea.Cmd) {
	name, argument, _ := strings.Cut(strings.TrimPrefix(command, "/"), " ")
	argument = strings.TrimSpace(argument)
	if m.running && !whileRunning[name] {
		return m.emit(m.printer.Note("/" + name + " changes the conversation: wait for the model to finish, or esc"))
	}
	switch name {
	case "quit", "exit":
		return m, tea.Quit
	case "clear":
		m.newConversation()
		return m, printText(m.printer.Note("new conversation · the last one stays in /resume"))
	case "resume":
		if m.running {
			return m.emit(m.printer.Note("stop the current run first"))
		}
		return m, listConversations(m.session.WorkDir, false)
	case "fork":
		return m.fork()
	case "delete":
		return m.deleteCurrent()
	case "clean":
		return m.clean(argument)
	case "title":
		return m.retitle(argument)
	case "model":
		return m.switchModel()
	case "login":
		return m.login(argument)
	case "logout":
		return m.logout(argument)
	case "continue":
		if m.running {
			return m.emit(m.printer.Note("the model is working already"))
		}
		return m.start(printText(m.printer.Note("continuing…")), m.session.Agent.Continue)
	case "vim":
		m.vim = vimState{enabled: !m.vim.enabled}
		state := "off"
		if m.vim.enabled {
			state = "on: esc for normal mode, i to write"
		}
		return m.emit(m.printer.Note("vim keys " + state))
	case "sidebar":
		m.sidebarHidden = !m.sidebarHidden
		if m.session.Config.Render.Sidebar == sidebarOff {
			return m, printText(m.printer.Note("the side panel is off in /settings › Interface › Look and keys"))
		}
		return m, m.fitWidth()
	case "jobs":
		if argument == "tell" {
			return m.tellJobs()
		}
		return m.listJobs()
	case "kill":
		return m.killJob(argument)
	case "notify":
		method := notify.Method(m.session.Config.Notify.Method)
		return m, tea.Batch(sendNotification(method, "test notification"), printText(m.printer.Note("sent a test notification · "+string(method))))
	case "rewind":
		return m.pickPrompt()
	case "help":
		return m, drawText(m.helpView)
	case "settings":
		if argument == "project" {
			m.openSection = menuProject
		}
		m.settingsInFlight = true
		return m, tea.Sequence(printText(m.printer.Note("looking for providers…")), loadSettings(m.session, generalSettings))
	case "providers":
		m.settingsInFlight = true
		return m, tea.Sequence(printText(m.printer.Note("looking for providers…")), loadSettings(m.session, providerSettings))
	case "view":
		if argument == "" {
			return m, printText(m.printer.Note("usage: /view <image, svg or mermaid file>"))
		}
		return m, m.view(argument)
	case "plugins":
		return m, printText(m.printer.Note(pluginReport(m.session.Plugins)))
	case "mcp":
		m.settingsInFlight = true
		return m, tea.Sequence(printText(m.printer.Note(mcpReport(m.session.MCP, m.session.Config))), loadSettings(m.session, mcpSettings))
	case "compact":
		return m.start(printText(m.printer.Note("compacting…")), m.session.Agent.Compact)
	}
	run, found := m.session.Plugins.Command(name)
	if !found {
		return m, printText(m.printer.Note("unknown command " + command + " · /help"))
	}
	return m.runPluginCommand(command, run, argument)
}

// view draws a file off the UI goroutine.
func (m model) view(path string) tea.Cmd {
	printer := m.printer
	return func() tea.Msg {
		return printText(printer.File(path))()
	}
}

func (m *model) stop() {
	m.cancel()
	m.closeDialog(true)
}

func (m model) finishRun(err error) (tea.Model, tea.Cmd) {
	m.running = false
	m.activity = ""
	m.runningTool = nil
	m.cancel()
	m.closeDialog(true)
	partial := strings.TrimSpace(m.stream)
	m.addReasoning(m.reasoning)
	m.clearStream()

	var lines []string
	if partial != "" {
		lines = append(lines, "\n"+m.printer.Markdown(partial))
	}
	// A long error goes folded between what came before and the notes after.
	var folded error
	foldAt := 0
	switch {
	case errors.Is(err, context.Canceled):
		lines = append(lines, m.printer.Note("stopped · /continue goes on"))
	case errors.Is(err, agent.ErrNothingToContinue):
		lines = append(lines, m.printer.Note("nothing to continue yet"))
	case err != nil && longError(err):
		folded, foldAt = err, len(lines)
	case err != nil:
		lines = append(lines, m.printer.Error(err))
	}
	save := m.saveConversation()
	stopped := errors.Is(err, context.Canceled)
	if !stopped {
		save = tea.Batch(save, m.turnEnded(err))
	}
	if stopped && m.session.Agent != nil {
		if queued := m.session.Agent.TakeQueued(); len(queued) > 0 {
			if typed := strings.TrimSpace(m.input.Value()); typed != "" {
				queued = append(queued, typed)
			}
			m.input.SetValue(strings.Join(queued, "\n"))
			m.input.CursorEnd()
			lines = append(lines, m.printer.Note("what you queued is back in the input"))
		}
	}
	if pending := m.pendingSession; pending != nil {
		m.pendingSession = nil
		takeOver(*pending, m.session)
		using, use := m.useSession(*pending)
		m = using.(model)
		lines = append(lines, m.printer.Note("the settings saved during the turn apply now"))
		save = tea.Batch(save, use)
	}
	if !stopped && m.session.Agent != nil && m.session.Agent.HasQueued() {
		next, run := m.start(nil, m.session.Agent.RunQueued)
		m = next.(model)
		save = tea.Batch(save, run)
	} else if m.session.Agent != nil && m.session.Agent.HasEndedJobs() {
		if stopped {
			lines = append(lines, m.printer.Note("a background job ended: the model goes on to hear of it · esc stops it again"))
		}
		reacting, react := m.reactToJobs()
		m = reacting.(model)
		save = tea.Batch(save, react)
	}
	if folded != nil {
		cmds := []tea.Cmd{save}
		if before := lines[:foldAt]; len(before) > 0 {
			shown, cmd := m.emit(strings.Join(before, "\n"))
			m = shown.(model)
			cmds = append(cmds, cmd)
		}
		m.addError(folded)
		if after := lines[foldAt:]; len(after) > 0 {
			shown, cmd := m.emit(strings.Join(after, "\n"))
			m = shown.(model)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if len(lines) == 0 {
		return m, save
	}
	return m, tea.Batch(save, printText(strings.Join(lines, "\n")))
}

func (m model) View() tea.View {
	var view tea.View
	if m.settings != nil {
		view = tea.NewView(m.settings.form.View() + "\n" + dimStyle.Render(m.settings.helpLine()))
		view.WindowTitle = "mo · settings"
	} else if m.viewer != nil {
		view = tea.NewView(m.viewerView())
		view.WindowTitle = fmt.Sprintf("mo · job %d", m.viewer.id)
	} else {
		screen, _ := m.screenLayout()
		drawn := m.highlight(screen)
		if m.popup != nil {
			drawn = m.overPopup(drawn)
		}
		view = tea.NewView(drawn)
		view.WindowTitle = "mo"
	}
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	if screen := screenFor(m.session.Config.Render.Background); screen.set {
		view.BackgroundColor, view.ForegroundColor = screen.background, screen.foreground
	}
	return view
}

// bottom is what stays on the last rows: what the agent asks or the spinner, the
// input and the status line.
func (m model) bottom() string {
	bottom, _ := m.bottomLayout()
	return bottom
}

// bottomLayout is the rows under the conversation and what can be clicked
// in them, counted from the first of them.
func (m model) bottomLayout() (string, []region) {
	upper, upperRegions := m.upperLayout()
	lower, lowerRegions := m.lowerLayout()
	if upper == "" {
		return lower, lowerRegions
	}
	return upper + "\n" + lower, append(upperRegions, shifted(lowerRegions, lipgloss.Height(upper))...)
}

// upperLayout is what sits between the conversation and the input: the
// tasks when there is no side panel, what the agent asks, the spinner, the
// commands that match. Beside the side panel it takes the conversation's
// width.
func (m model) upperLayout() (string, []region) {
	var sections []string
	var regions []region
	add := func(section string, sectionRegions []region) {
		lines := 0
		if len(sections) > 0 {
			lines = lipgloss.Height(strings.Join(sections, "\n"))
		}
		regions = append(regions, shifted(sectionRegions, lines)...)
		sections = append(sections, section)
	}
	if panel := m.todoPanel(); panel != "" {
		add(panel, nil)
	}
	switch {
	case m.dialog != nil:
		add(m.dialogLayout())
	case m.running:
		queued := ""
		if count := m.session.Agent.QueuedCount(); count > 0 {
			queued = fmt.Sprintf(" · %d queued", count)
		}
		add(m.spinner.View()+" "+m.currentState().label(true)+dimStyle.Render(queued+" · esc to stop"), nil)
	}
	if m.titling != nil {
		stop := ""
		if !m.running && m.dialog == nil {
			stop = " · esc to stop"
		}
		add(m.spinner.View()+" "+m.titleState().label(true)+dimStyle.Render(stop), nil)
		if said := m.saidLines(); said != "" {
			add(said, nil)
		}
	}
	if matches := m.suggestions(); len(matches) > 0 {
		add(m.suggestionLayout(matches))
	}
	if bar, barRegions := m.attachmentBar(); bar != "" {
		add(bar, barRegions)
	}
	return strings.Join(sections, "\n"), regions
}

// lowerLayout is the input and the status line, the whole width.
func (m model) lowerLayout() (string, []region) {
	status, regions := m.statusLayout()
	input := m.input.View()
	return input + "\n" + status, shifted(regions, lipgloss.Height(input))
}

// screenLayout is the whole screen and what can be clicked on it: the
// conversation, and beside it the side panel down to the input, then the
// input and the status line.
func (m model) screenLayout() (string, []region) {
	upper, upperRegions := m.upperLayout()
	lower, lowerRegions := m.lowerLayout()
	height := m.conversationHeight()
	left := m.conversation(height).View() + "\n"
	regions := shifted(upperRegions, height+1)
	if upper != "" {
		left += "\n" + upper
	}
	if m.sidebarShown() {
		panel, panelRegions := m.sidebarLayout(lipgloss.Height(left))
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", panel)
		for _, r := range panelRegions {
			r.x0, r.x1 = r.x0+m.mainWidth()+1, r.x1+m.mainWidth()+1
			regions = append(regions, r)
		}
	}
	return left + "\n" + lower, append(regions, shifted(lowerRegions, lipgloss.Height(left))...)
}

// conversationHeight is the rows left for the conversation over a blank
// line and what goes under it.
func (m model) conversationHeight() int {
	return max(m.height-lipgloss.Height(m.bottom())-1, 1)
}

// conversation is the transcript and the reply being written, in a viewport
// of the given height that follows the end unless the user scrolled up.
func (m model) conversation(height int) viewport.Model {
	conversation := m.viewport
	conversation.SetWidth(m.mainWidth())
	conversation.SetHeight(max(height, 1))
	conversation.SetContent(m.markConversation(m.conversationContent()))
	if !m.scrolled {
		conversation.GotoBottom()
	}
	return conversation
}

// emit adds printed output to the transcript. Kitty image uploads go to the
// terminal once, and only their placeholders stay in the transcript.
func (m model) emit(text string) (tea.Model, tea.Cmd) {
	uploads := strings.Join(kittyUploads.FindAllString(text, -1), "")
	m.blocks = append(m.blocks, block{text: kittyUploads.ReplaceAllString(text, "")})
	if uploads == "" {
		return m, nil
	}
	return m, tea.Raw(uploads)
}

var kittyUploads = regexp.MustCompile(`\x1bPtmux;(?:[^\x1b]|\x1b\x1b)*\x1b\\|\x1b_G[^\x1b]*\x1b\\`)

func (m *model) scroll(up bool, lines int) {
	m.viewport = m.conversation(m.conversationHeight())
	if up {
		m.viewport.ScrollUp(lines)
	} else {
		m.viewport.ScrollDown(lines)
	}
	m.scrolled = !m.viewport.AtBottom()
}

func (m model) pageSize(key string) int {
	if strings.HasPrefix(key, "shift") {
		return 3
	}
	return max(m.height-lipgloss.Height(m.bottom())-3, 1)
}

func printText(text string) tea.Cmd {
	return func() tea.Msg {
		return printMsg(text)
	}
}

// renderStream renders as markdown the lines of the reply that are complete,
// again each time one more is, or all of them when again is set.
func (m *model) renderStream(again bool) {
	cut := strings.LastIndex(m.stream, "\n") + 1
	if cut == 0 || (cut == m.streamCut && !again) {
		return
	}
	m.streamCut = cut
	m.streamDone = m.printer.Markdown(m.stream[:cut])
}

func (m *model) clearStream() {
	m.stream, m.streamCut, m.streamDone = "", 0, ""
	m.forming = nil
	m.reasoning = ""
}

// liveText is what the model is writing right now, set apart from what came
// before like the answer it becomes: the complete lines as markdown, the line
// being written as it comes. Before any of it, its reasoning shows dimmed.
func (m model) liveText() string {
	reasoning := strings.TrimSpace(m.reasoning)
	forming := m.formingView()
	if m.stream == "" && forming == "" {
		if reasoning == "" {
			return ""
		}
		return m.printer.fold("reasoning", reasoning, m.showDetails, true)
	}
	parts := []string{""}
	if reasoning != "" {
		parts = []string{m.printer.fold("reasoning", reasoning, m.showDetails, false), ""}
	}
	if m.streamDone != "" {
		parts = append(parts, m.streamDone)
	}
	wrap := lipgloss.NewStyle().Width(max(m.mainWidth()-4, 20)).PaddingLeft(2)
	if tail := strings.TrimSpace(m.stream[m.streamCut:]); tail != "" {
		parts = append(parts, wrap.Render(tail))
	}
	if forming != "" {
		parts = append(parts, forming)
	}
	return strings.Join(parts, "\n")
}

const contextBarCells = 10

// contextGauge is how full the context is in one cell, in eighths.
func contextGauge(share float64) string {
	levels := []rune("▁▂▃▄▅▆▇█")
	return string(levels[max(0, min(int(share*float64(len(levels))), len(levels)-1))])
}

// contextBar draws how full the context is in cells, the used ones in fill.
func contextBar(share float64, cells int, fill, empty color.Color) string {
	used := min(int(share*float64(cells)+0.5), cells)
	if share > 0 && used == 0 {
		used = 1
	}
	return lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("▰", used)) +
		lipgloss.NewStyle().Foreground(empty).Render(strings.Repeat("▱", cells-used))
}

type statusColors struct {
	provider, model, label, low, mid, high, empty, prompt, generation color.Color
	// base is what segments are shaded from; segment, segmentDim and
	// segmentDeep are the backgrounds of the model, the context and the
	// speeds; text and branch colour the folder and the branch.
	base, segment, segmentDim, segmentDeep, text, branch color.Color
}

// mode colours a permission mode by how much it lets through.
func (c statusColors) mode(mode permissions.Mode) color.Color {
	switch mode {
	case permissions.ModeAsk:
		return c.provider
	case permissions.ModeEdits:
		return c.low
	case permissions.ModeAuto:
		return c.mid
	}
	return c.high
}

func statusPalette(dark bool) statusColors {
	if dark {
		return statusColors{
			provider: accent, model: lipgloss.Color("#bb9af7"), label: lipgloss.Color("#565f89"),
			low: lipgloss.Color("#9ece6a"), mid: warning, high: lipgloss.Color("#f7768e"), empty: lipgloss.Color("#3b4261"),
			prompt: lipgloss.Color("#7dcfff"), generation: lipgloss.Color("#73daca"),
			base: lipgloss.Color("#0b1230"), segment: lipgloss.Color("#2a2f4a"), segmentDim: lipgloss.Color("#1f2440"),
			segmentDeep: lipgloss.Color("#171b33"), text: lipgloss.Color("#a9b1d6"), branch: warning,
		}
	}
	return statusColors{
		provider: lipgloss.Color("#2e7de9"), model: lipgloss.Color("#9854f1"), label: lipgloss.Color("#8990b3"),
		low: lipgloss.Color("#587539"), mid: lipgloss.Color("#8c6c3e"), high: lipgloss.Color("#f52a65"), empty: lipgloss.Color("#c4c8da"),
		prompt: lipgloss.Color("#007197"), generation: lipgloss.Color("#118c74"),
		base: lipgloss.Color("#f4f5fb"), segment: lipgloss.Color("#d5d9ec"), segmentDim: lipgloss.Color("#e1e4f2"),
		segmentDeep: lipgloss.Color("#e9ebf6"), text: lipgloss.Color("#3760bf"), branch: lipgloss.Color("#8c6c3e"),
	}
}

// shortModelName drops the directories and the extension from model ids that
// are file paths, as llama-server reports them.
func shortModelName(model string) string {
	return strings.TrimSuffix(filepath.Base(model), ".gguf")
}

// runPluginCommand runs a plugin's command like a turn of the agent: Esc
// stops it, and no prompt can start meanwhile. What it returns is printed
// as markdown.
func (m model) runPluginCommand(command string, run func(context.Context, string) (string, error), argument string) (tea.Model, tea.Cmd) {
	bridge := m.bridge
	return m.start(drawText(func() string { return m.printer.User(command) }), func(ctx context.Context, _ agent.Hooks) error {
		output, err := run(ctx, argument)
		if err != nil {
			return err
		}
		bridge.print(bridge.printer.Answer(output))
		return nil
	})
}

func pluginReport(loaded *plugins.Set) string {
	statuses := loaded.Status()
	if len(statuses) == 0 {
		return "no plugins; put .lua files in " + config.PluginDir()
	}
	var lines []string
	for _, status := range statuses {
		if status.Err != nil {
			lines = append(lines, fmt.Sprintf("✗ %s: %v", status.Name, status.Err))
			continue
		}
		var parts []string
		parts = append(parts, status.Tools...)
		parts = append(parts, status.Commands...)
		parts = append(parts, status.Hooks...)
		lines = append(lines, fmt.Sprintf("✓ %s: %s", status.Name, strings.Join(parts, ", ")))
		for _, warning := range status.Warnings {
			lines = append(lines, "  ! "+warning)
		}
	}
	return strings.Join(lines, "\n")
}

func mcpReport(statuses []mcphub.Status, cfg config.Config) string {
	var lines []string
	for _, name := range slices.Sorted(maps.Keys(cfg.MCP)) {
		if slices.Contains(cfg.Tools.MCPDisabled, name) {
			lines = append(lines, fmt.Sprintf("○ %s: off · /settings › Tools turns it on", name))
		}
	}
	if len(statuses)+len(lines) == 0 {
		return "no MCP servers yet"
	}
	for _, status := range statuses {
		if status.Err != nil {
			lines = append(lines, fmt.Sprintf("✗ %s: %v", status.Name, status.Err))
			continue
		}
		lines = append(lines, fmt.Sprintf("✓ %s: %d tools", status.Name, status.Tools))
	}
	return strings.Join(lines, "\n")
}

func formatTokens(count int) string {
	if count < 1000 {
		return fmt.Sprint(count)
	}
	return fmt.Sprintf("%.1fk", float64(count)/1000)
}
