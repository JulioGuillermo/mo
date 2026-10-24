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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/gitops"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/peers"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/sessions"
	"github.com/jg/mo/internal/skills"
	"github.com/jg/mo/internal/team"
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
	Reload func(userLayer config.Config, projectLayer *config.Config) (Session, error)
	// Rebuild builds a session from settings already put together.
	Rebuild   func(cfg config.Config) (Session, error)
	MCP       []mcphub.Status
	Plugins   *plugins.Set
	ToolNames []string
	// Skills names every skill found, also the ones turned off.
	Skills []string
	// Found are those skills, which the user may load by hand: $name or
	// /skill name.
	Found []skills.Skill
	// Instructions are the instructions files read into the system prompt.
	Instructions []string
	// WindowFrom says where the context window came from.
	WindowFrom string

	ToolDescriptions map[string]string

	// Continue resumes the latest conversation of the working directory;
	// Resume opens on the list of them, to pick one.
	Continue bool
	Resume   bool

	Activate func()
	Discard  func()

	// Team runs the subagents; nil when mo cannot start any.
	Team *team.Team
	// Peers are the other mo instances, this one among them once started.
	Peers *peers.Node
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
	// forming are the tool calls the model is writing now; callsOpen while
	// the reply still streams a call, the last of them being written.
	forming   []formingCall
	callsOpen bool
	// activity is what the agent waits on, said by the spinner.
	activity string
	// pendingSession is built from settings saved while a turn ran, to take
	// over when it ends.
	pendingSession *Session
	// config follows the settings files on disk.
	config *configSync
	// peers is what /peers changed for the session, and the peers'
	// messages that wait for the user.
	peers *peerSession
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
	// userRuns numbers the commands run with ! and the /git actions;
	// shells stops those of ! still going, and shellNotes keeps what they
	// printed for the model's next message.
	userRuns   int
	shells     map[string]context.CancelFunc
	shellNotes []string
	// ranByUser are the commands run with ! that ended, by their call's
	// id, to give one to the model later with a right click.
	ranByUser map[string]*userRun
	// history is what was sent before, for the arrows to bring back.
	history inputHistory
	// titling is the title the model is working out, after /title auto.
	titling *titling
	// promptRead is how far the server is in reading the prompt, when it
	// says so.
	promptRead llm.PromptProgress
	// metrics counts the reply being written's context and speeds until the
	// exact ones come.
	metrics     liveMetrics
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
	// efforts are what the model in use takes, read from its provider.
	efforts effortInfo
	// fileComments are the user's comments on files shown with /view, by
	// path, and on diffs; commentNotes what each attached comments label
	// sends, commentSources whose comments it holds; files the index @
	// searches.
	fileComments   map[string][]fileComment
	commentNotes   map[string]string
	commentSources map[string]string
	files          *fileIndex
	// runStarted is when the turn running began; worked how long the last
	// one took, shown once it ends.
	runStarted time.Time
	worked     time.Duration
	// newPrompt is set while a prompt is sent, so the turn it starts counts
	// as a new one for the jobs.
	newPrompt bool
	// popup is a small menu over the screen; clickedAt is where the right
	// click that opens one was, clicked unset when no click opened it.
	popup *popup
	// uiOpen is the interface shown as a dialog, 0 for none; uiLives what
	// the user did in each.
	uiOpen  int
	uiLives map[int]*uiLive
	// uiStore holds the open dialog's interface: the model's or a plugin's.
	uiStore *tools.UIs
	// pluginSends is how the messages plugins send the model stand.
	pluginSends *pluginSends
	// svgs draws the svg nodes of interfaces, away from the screen.
	svgs             *svgFrames
	sixel            *sixelScreen
	sixelTerminal    bool
	clickedAt        point
	clicked          bool
	settingsInFlight bool
	// agentView, when set, shows a subagent over the whole screen;
	// lensOf is set on the model drawn for it, to that subagent.
	agentView *agentView
	lensOf    int
	// waitingFor says, on that model, what the subagent waits for.
	waitingFor string
	// stepHistory is the main agent's history as its last step left it,
	// to save while it works; saves numbers the saves, saveTicking is set
	// while one is due.
	stepHistory []llm.Message
	saves       int
	saveTicking bool

	suggestion int
	// queued are the messages written while the model works, as typed,
	// until it reads them.
	queued []string
	// sendingNow is set when ctrl+s stops the model to hand it what is queued:
	// the stop then runs those messages instead of giving them back.
	sendingNow bool
	// suggestionsClosedOn is what the input held when esc closed its
	// suggestions: they stay closed until it changes.
	suggestionsClosedOn string
	suggestionsClosed   bool
	blocks              []block
	redraws             int
	attachments         map[string]string
	// thumbnails are the kitty lines that show the images in the input, by
	// path; nil while drawn.
	thumbnails map[string][]string
	// queuedAttachments are the labels of the queued messages, by the path
	// they stand for, to have them back with the messages in the input.
	queuedAttachments map[string]string
	pastedImages      int
	showDetails       bool
	thread            conversation
	lastEsc           time.Time
	// stopping is the stop asked of the model and the subagents while
	// they work, until they stop.
	stopping agent.StopMode
	// terminalDark is whether the terminal's own background is dark, as it
	// reports it.
	terminalDark bool
	// focused is whether the terminal has the focus, as it reports it; mo
	// starts as if it had.
	focused bool
	// todos is the model's task list, from its latest todo call.
	todos []tools.Todo
	// terminalCopies is whether the terminal copies any type of data
	// itself, with kitty's clipboard protocol: files and images too, over
	// SSH as well.
	terminalCopies bool
	// sidebarSession is the side panel's mode /sidebar set for the session;
	// empty follows the setting.
	sidebarSession string
	// sidebarSessionWidth is the side panel's width /sidebar width set for
	// the session; 0 keeps the setting's.
	sidebarSessionWidth int
	// sidebarScroll is how many of the panel's lines the wheel scrolled past.
	sidebarScroll int
	// openSection is the settings section to open once settings load, as
	// a click on the model asks.
	openSection string
	// selected is text being picked with the mouse; flash says for a moment
	// what was copied.
	selected *selection
	flash    string
	flashes  int
	// modeSaves counts the permission mode's switches, so only the last is
	// written.
	modeSaves   int
	jobsTicking bool
	// top is the first line of the conversation in view, once scrolled.
	top      int
	scrolled bool
	// shownSpeeds are the live speeds as last shown, kept to the pace the
	// reply is drawn at; nil shows them as they are.
	shownSpeeds *shownSpeeds
}

const agentShutdownWait = 3 * time.Second

func Run(session Session) error {
	printer := newPrinter()
	printer.setImages(imagesFor(session.Config.Render.Images, false))
	useCellSize(session.Config.Render.CellSize)
	useSyntaxTheme(session.Config.Render.Syntax)
	screen := screenFor(session.Config.Render, true)
	useUITheme(session.Config.Render.Theme, screen.dark)
	printer.setDark(screen.dark)
	printer.workDir = session.WorkDir
	bridge := &bridge{printer: printer, exited: make(chan struct{})}
	m := model{
		session:      session,
		bridge:       bridge,
		printer:      printer,
		input:        newInput(),
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(workingStyle())),
		focused:      true,
		terminalDark: true,
		history:      loadHistory(),
		vim:          vimState{enabled: session.Config.Render.Keys == "vim"},
		files:        &fileIndex{},
		shownSpeeds:  &shownSpeeds{},
		sixel:        newSixelScreen(),
		svgs:         &svgFrames{kitty: printer.images == render.ModeKitty, ready: func() { bridge.send(svgReadyMsg{}) }},
		config:       watchConfig(session.WorkDir),
		peers:        newPeerSession(),
	}
	defer m.config.close()
	m.input.SetStyles(inputStyles(m.input.Styles(), screen.dark))
	printer.setKind(m.toolKind)
	m.configureJobs()
	m.paceAnimations()
	program := tea.NewProgram(m, tea.WithOutput(sixelOutput{File: os.Stdout, screen: m.sixel}))
	bridge.program = program
	bridge.paceReply(paceOf(session.Config.Render))
	m.config.tell(bridge)
	followPeers(session.Peers, bridge)
	if titles := session.Agent.Tools.Titles(); titles != nil {
		titles.OnSet(func(title string) { bridge.send(titleSetMsg(title)) })
	}
	if jobs := session.Agent.Tools.Jobs(); jobs != nil {
		jobs.OnEnd(func(status tools.JobStatus) { bridge.send(jobEndedMsg(status)) })
		jobs.OnStart(func(status tools.JobStatus) { bridge.send(jobStartedMsg(status)) })
	}
	if uis := session.Agent.Tools.UIs(); uis != nil {
		uis.OnChange(func() { bridge.send(uiChangedMsg{}) })
	}
	session.Plugins.Follow(func() { bridge.send(uiChangedMsg{}) }, func(id int) { bridge.send(pluginDialogMsg(id)) })
	session.Plugins.OnSend(func(message plugins.SentMessage) { bridge.send(pluginSendMsg(message)) })
	session.Plugins.OnView(func(path string) { bridge.send(pluginViewMsg(path)) })
	session.Plugins.SetUIFPS(session.Config.Render.UIFPS)
	if session.Team != nil {
		session.Team.Follow(func(team.Info) agent.Hooks { return agent.Hooks{Interact: bridge.interact} },
			func() { bridge.send(agentsChangedMsg{}) }, func() { bridge.send(agentEndedMsg{}) })
	}
	final, err := program.Run()
	close(bridge.exited)
	if last, ok := final.(model); ok {
		finished := !last.running
		if last.running {
			last.stop()
			select {
			case <-last.finished:
				finished = true
			case <-time.After(agentShutdownWait):
			}
		}
		// Whatever way mo exits, the conversation is kept as it stands.
		last.saveOnExit(finished)
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
		state.Prompt = shade.Foreground(accent()).Bold(true)
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
	if opening := m.openingConversation(); opening != nil {
		start = tea.Sequence(start, opening)
	}
	return tea.Batch(start, tea.RequestBackgroundColor, tea.Raw(ansi.RequestMode(kittyClipboardMode)), textarea.Blink,
		m.askTerminal(), m.readRepo(), loadEfforts(m.session), m.applyPeers())
}

// openingConversation is what mo opens on besides a new conversation: the
// latest one with -c, the list of them with -r.
func (m model) openingConversation() tea.Cmd {
	switch {
	case m.session.Continue:
		return listConversations(m.session.WorkDir, true)
	case m.session.Resume:
		return listConversations(m.session.WorkDir, false)
	}
	return nil
}

// takeEvent adds a piece of the reply, which came at a time.
func (m *model) takeEvent(event llm.Event, at time.Time) {
	m.metrics.add(event, at)
	m.lastEvent = event.Kind
	m.callsOpen = event.Kind == llm.EventToolCall
	switch event.Kind {
	case llm.EventToolCall:
		m.growCall(event.Call, event.ID, event.Name, event.Text)
	case llm.EventPromptProgress:
		m.promptRead = event.Progress
	case llm.EventReasoning:
		m.reasoning += event.Text
	default:
		m.stream += event.Text
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	updated, ok := next.(model)
	if !ok {
		return next, cmd
	}
	return updated, tea.Batch(cmd, updated.drawThumbnails())
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case thumbnailMsg:
		return m.thumbnailDrawn(msg)
	case blameReadMsg:
		return m.blameRead(msg)
	case sidebarWidthSaved:
		return m.sidebarWidthSaved(msg)
	case historyReadMsg:
		return m.historyRead(msg)
	case mcpTestedMsg:
		return m.mcpTested(msg)
	case svgReadyMsg:
		// A picture was drawn: the screen shows it as it is drawn again,
		// and a kitty one goes to the terminal, whatever is open.
		return m, sendRaw(m.svgs.uploads())
	case settingsLoadedMsg:
		return m.openSettings(msg)
	case settingsSavedMsg:
		return m.finishSettings(msg)
	case deletedMsg:
		return m.conversationsDeleted(msg)
	case effortsMsg:
		m.efforts = effortInfo(msg)
		return m, nil
	case modelsMsg:
		return m.showModels(msg)
	case configChangedMsg:
		return m.configChanged()
	case configBuiltMsg:
		return m.configBuilt(msg)
	case peerMsg:
		return m.peerArrived(peers.Message(msg))
	case peerJudgedMsg:
		return m.peerJudged(msg)
	case peerSentMsg:
		return m.peerSent(msg)
	case peerRetryMsg:
		return m.peerRetried()
	case peersListedMsg:
		if msg.err != nil {
			return m.emitError(msg.err)
		}
		return m.emit(m.printer.Note(msg.text))
	}
	if m.settings != nil && !isAgentMsg(msg) {
		return m.updateSettings(msg)
	}
	if _, changed := msg.(uiChangedMsg); changed {
		if _, ok := m.dialogUIs().Get(m.uiOpen); !ok {
			m.uiOpen = 0
		}
		return m, nil
	}
	if page, ok := msg.(signInPageMsg); ok {
		return m.showSignInPage(page)
	}
	if signed, ok := msg.(mcpSignedInMsg); ok {
		return m.mcpSignedIn(signed)
	}
	if clicked, ok := msg.(pluginClickMsg); ok {
		return m.pluginClicked(clicked)
	}
	switch msg := msg.(type) {
	case pluginSendMsg:
		return m.pluginSent(plugins.SentMessage(msg))
	case pluginJudgedMsg:
		return m.pluginJudged(msg)
	case pluginRetryMsg:
		return m.pluginRetried()
	case pluginViewMsg:
		return m.viewArgument(string(msg))
	case fileCopiedMsg:
		return m.fileCopied(msg)
	}
	if opened, ok := msg.(pluginDialogMsg); ok {
		if m.settings != nil || m.popup != nil {
			return m, nil
		}
		return m.openPluginDialog(int(opened))
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && m.pluginsTookKey(key) {
		return m, nil
	}
	if m.popup != nil && !isAgentMsg(msg) {
		if _, tick := msg.(viewerTickMsg); !tick {
			return m.updatePopup(msg)
		}
	}
	if m.uiOpen != 0 && m.popup == nil && m.viewer == nil && !isAgentMsg(msg) {
		if _, tick := msg.(viewerTickMsg); !tick {
			return m.updateUI(msg)
		}
	}
	if m.agentView != nil && m.popup == nil && !isAgentMsg(msg) {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg, tea.MouseWheelMsg, tea.MouseClickMsg:
			return m.updateAgentView(msg)
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
		if msg.Content == "" {
			return m, pasteClipboard()
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
	case modeSaveMsg:
		return m.saveMode(msg)
	case modeSavedMsg:
		if msg.err != nil {
			return m.emitError(msg.err)
		}
		return m, nil
	case edgeScrollMsg:
		return m.edgeScroll()
	case flashDoneMsg:
		if int(msg) == m.flashes {
			m.flash = ""
		}
		return m, nil
	case toolStartMsg:
		m.followTodos(msg.call)
		m.formingStarted(msg.call.ID, msg.call.Name)
		m.addTool(msg.call, msg.summary)
		running := msg.call
		m.runningTool = &running
		return m, nil
	case shellDoneMsg:
		return m.finishShell(msg)
	case gitDoneMsg:
		return m.finishGitAction(msg)
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
		resized := m.resize(msg)
		m.refreshAgentView()
		return m, resized
	case redrawMsg:
		if int(msg) == m.redraws {
			m.redraw()
		}
		return m, nil
	case drawMsg:
		return m.emitDrawn(msg)
	case tea.ModeReportMsg:
		if msg.Mode == kittyClipboardMode && !msg.Value.IsNotRecognized() {
			m.terminalCopies = true
		}
		return m, nil
	case tea.BackgroundColorMsg:
		return m, m.applyScreen(msg.IsDark())
	case uv.PrimaryDeviceAttributesEvent:
		return m.terminalAnswered(msg)
	case uv.CellSizeEvent:
		return m.cellSizeAnswered(msg)
	case tea.KeyPressMsg:
		m.focused = true
		return m.handleKey(msg)
	case streamMsg:
		m.takeEvent(llm.Event(msg), time.Now())
		m.renderStream(false)
		return m, nil
	case streamBatchMsg:
		for _, each := range msg {
			m.takeEvent(each.event, each.at)
		}
		m.renderStream(false)
		return m, nil
	case streamEndMsg:
		// The text is drawn as the answer; the calls stay in view until
		// they run, the later ones waiting for those before them.
		forming := m.forming
		m.clearStream()
		m.forming = forming
		return m, nil
	case activityMsg:
		m.activity = string(msg)
		return m, nil
	case modelMsg:
		return m.modelChanged(msg)
	case contextCutMsg:
		return m.contextCut(msg)
	case agentsChangedMsg:
		m.refreshAgentView()
		return m, m.scheduleSave()
	case historyMsg:
		m.stepHistory = msg
		return m, m.scheduleSave()
	case saveTickMsg:
		m.saveTicking = false
		return m, m.saveConversation()
	case agentEndedMsg:
		return m.agentEnded()
	case queuedReadMsg:
		return m.queuedRead(int(msg))
	case cacheClearedMsg:
		return m.cacheCleared(msg)
	case usageMsg:
		m.usage = llm.Usage(msg)
		m.metrics = liveMetrics{}
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
		return done, tea.Batch(cmd, read, done.peersAfterTurn())
	case spinner.TickMsg:
		if !m.running && m.titling == nil && !m.agentWorking() {
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
		m.suggestionsClosed = false
	}
	return m, cmd
}

// resize fits everything to the new size: what is being written at once,
// what was printed once the resizing stops.
func (m *model) resize(size tea.WindowSizeMsg) tea.Cmd {
	widthChanged := size.Width != m.width
	m.width, m.height = size.Width, size.Height
	m.session.Plugins.SetScreen(size.Width, size.Height)
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
	pressed := msg.String()
	// With nothing typed and without vim keys, ctrl+h/j/k/l are the
	// arrows ctrl+ takes, as they edit nothing in an empty input.
	if m.input.Value() == "" && !m.vimOn() {
		if arrow, ok := map[string]string{"ctrl+h": "ctrl+left", "ctrl+j": "ctrl+down", "ctrl+k": "ctrl+up", "ctrl+l": "ctrl+right"}[pressed]; ok {
			pressed = arrow
		}
	}
	switch pressed {
	case "ctrl+up", "ctrl+down":
		m.scroll(pressed == "ctrl+up", 1)
		return m, nil
	case "ctrl+left", "ctrl+right":
		// With nothing typed they move the wide diagrams across; while
		// typing, by words, as alt+←→ always does.
		if m.input.Value() == "" && m.printer.hasDrawings() {
			by := drawingStep
			if pressed == "ctrl+left" {
				by = -drawingStep
			}
			if m.printer.panDrawings(by) {
				m.redrawDrawings()
			}
			return m, nil
		}
	case "ctrl+v", "alt+v":
		return m, pasteClipboard()
	case "backspace", "delete":
		if m.eraseAttachment(msg.String()) {
			return m, nil
		}
	case "shift+tab":
		if policy := m.session.Agent.Permissions; policy != nil {
			return m.setMode(policy.Mode().Next())
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
	case "ctrl+s":
		// Sends now what is queued, and what is typed with it: the model
		// stops where it is and answers them, instead of reading them at its
		// next step.
		if m.running && m.session.Agent != nil {
			return m.sendNow()
		}
	case "ctrl+x":
		// Clears what is typed, the model's run going on.
		if m.input.Value() != "" {
			m.input.Reset()
			m.history.shown = 0
			m.suggestion = 0
			return m, nil
		}
	case "ctrl+c":
		// What is typed goes first: the model and the shells go on.
		if m.input.Value() != "" {
			m.input.Reset()
			return m, nil
		}
		if m.running {
			m.stop()
			return m, nil
		}
		if m.stopShells() {
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.running {
			return m.escStop()
		}
		if m.stopShells() {
			return m, nil
		}
		if m.titling != nil {
			m.titling.cancel()
			return m, nil
		}
		if m.working() {
			return m.escStop()
		}
		m.stopping = agent.StopNone
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
	case "esc":
		// Closes the list only: a model at work goes on.
		m.suggestionsClosedOn, m.suggestionsClosed = m.input.Value(), true
	case "tab":
		m.completeCommand(selected)
	case "enter":
		if m.input.Value() == "/"+selected.name || selected.shell {
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
	if !keptInHistory(prompt) {
		return m.submitted(prompt)
	}
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
	if command, told := strings.CutPrefix(prompt, "!!"); told {
		return m.runShell(strings.TrimSpace(command), true)
	}
	if command, isShell := strings.CutPrefix(prompt, "!"); isShell {
		return m.runShell(strings.TrimSpace(command), false)
	}
	typed := prompt
	prompt, err := m.withSkill(prompt)
	if err != nil {
		return m.emit(m.printer.Note(err.Error()))
	}
	if m.peers != nil {
		m.peers.streak = 0
	}
	if m.pluginSends != nil {
		m.pluginSends.streak = 0
	}
	if m.running {
		return m.queue(typed, prompt)
	}
	return m.beginTurn(typed, m.withShellNotes(m.withAttachments(prompt)))
}

// beginTurn starts a turn that sends the model sent, shown as typed.
func (m model) beginTurn(typed, sent string) (tea.Model, tea.Cmd) {
	m.checkConfig()
	m.scrolled = false
	m.newPrompt = true
	if m.session.Team != nil {
		m.session.Team.BeginTurn()
	}
	images := promptImages(agent.SentPrompt(llm.Message{Text: sent}))
	echo := drawText(func() string { return m.printer.User(typed) + m.printer.Thumbnails(images) })
	return m.start(echo, func(ctx context.Context, hooks agent.Hooks) error {
		return m.session.Agent.RunTyped(ctx, sent, typed, hooks)
	})
}

// start runs work on the agent in the background, after first, until it
// ends or is stopped.
func (m model) start(first tea.Cmd, work func(context.Context, agent.Hooks) error) (tea.Model, tea.Cmd) {
	m.ensureThread()
	// The agent is idle yet: its history is safe to copy.
	m.stepHistory = slices.Clone(m.session.Agent.History)
	m.runStarted = time.Now()
	m.trackTurn(m.newPrompt)
	m.newPrompt = false
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	m.stopping = agent.StopNone
	m.session.Agent.ClearStop()
	m.running = true
	m.tellPeers()
	m.cancel = cancel
	m.finished = finished
	hooks := m.bridge.hooks()
	bridge := m.bridge
	runAgent := func() tea.Msg {
		defer close(finished)
		err := work(ctx, hooks)
		// The end of the reply is drawn before the run is over.
		bridge.flush()
		return runDoneMsg{err: err}
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
	case "new", "clear":
		m.newConversation()
		return m, tea.Batch(printText(m.printer.Note("new conversation · the last one stays in /resume")), m.checkModel())
	case "resume":
		if m.running {
			return m.emit(m.printer.Note("stop the current run first"))
		}
		return m, listConversations(m.session.WorkDir, false)
	case "fork":
		return m.fork()
	case "effort":
		return m.effortCommand(argument)
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
		goOn := m.session.Agent.Continue
		switch argument {
		case "":
		case "keep":
			goOn = m.session.Agent.ContinueTelling
		default:
			return m.emit(m.printer.Note("/continue, or /continue keep to tell the model it was stopped"))
		}
		if m.session.Team != nil {
			// The subagents it left working in the background go on too.
			m.session.Team.GoOn(0)
		}
		return m.start(printText(m.printer.Note("continuing…")), goOn)
	case "stop":
		return m.stopCommand(argument)
	case "vim":
		m.vim = vimState{enabled: !m.vim.enabled}
		state := "off"
		if m.vim.enabled {
			state = "on: esc for normal mode, i to write"
		}
		return m.emit(m.printer.Note("vim keys " + state))
	case "sidebar":
		return m.sidebarCommand(argument)
	case "jobs":
		if argument == "tell" {
			return m.tellJobs()
		}
		return m.listJobs()
	case "kill":
		return m.killJob(argument)
	case "notify":
		settings := m.session.Config.Notify
		note := fmt.Sprintf("sent a test notification · %s · sound %s", settings.Method, settings.Sound)
		return m, tea.Batch(sendNotification(settings, "test notification"), printText(m.printer.Note(note)))
	case "rewind":
		return m.pickPrompt()
	case "reload":
		return m.followConfig(true)
	case "git":
		return m.gitCommand(argument)
	case "peers":
		return m.peersCommand(argument)
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
	case "diagrams":
		code := m.printer.toggleAllCode()
		m.redraw()
		if code {
			return m, printText(m.printer.Note("diagrams shown as their code · /diagrams draws them again"))
		}
		return m, printText(m.printer.Note("diagrams drawn"))
	case "view":
		return m.viewArgument(argument)
	case "agents":
		return m.pickAgent(argument)
	case "cache":
		if argument != "clear" {
			return m.emit(m.printer.Note("usage: /cache clear · erases what llama-server holds in its slots"))
		}
		return m, clearCache(m.session.Agent.Provider)
	case "plugins":
		asked, ask := m.askPostponedPlugin()
		return asked, tea.Batch(printText(m.printer.Note(pluginReport(m.session.Plugins))), ask)
	case "mcp":
		if action, server, _ := strings.Cut(argument, " "); action == "login" || action == "logout" {
			return m.mcpSignIn(action, strings.TrimSpace(server))
		}
		m.settingsInFlight = true
		return m, tea.Sequence(printText(m.printer.Note(mcpReport(m.session.MCP, m.session.Config))), loadSettings(m.session, mcpSettings))
	case "skill":
		if argument == "" {
			return m.emit(m.printer.Note(m.skillReport()))
		}
		return m.submitted("$" + argument)
	case "compact":
		note := "compacting…"
		if argument != "" {
			note = "compacting, with your instruction…"
		}
		return m.start(printText(m.printer.Note(note)), func(ctx context.Context, hooks agent.Hooks) error {
			return m.session.Agent.CompactWith(ctx, hooks, argument)
		})
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
	if m.session.Team != nil {
		m.session.Team.StopAll()
	}
}

func (m model) finishRun(err error) (tea.Model, tea.Cmd) {
	sendingNow := m.sendingNow
	m.sendingNow = false
	m.running = false
	m.tellPeers()
	if !m.runStarted.IsZero() {
		m.worked = time.Since(m.runStarted)
	}
	m.activity = ""
	m.runningTool = nil
	stopAsked := m.stopping != agent.StopNone
	m.stopping = agent.StopNone
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
	case errors.Is(err, context.Canceled) && sendingNow:
		lines = append(lines, m.printer.Note("stopped to send what you queued"))
	case errors.Is(err, context.Canceled):
		lines = append(lines, m.printer.Note("stopped · /continue goes on"))
	case errors.Is(err, agent.ErrNothingToContinue):
		lines = append(lines, m.printer.Note("nothing to continue yet"))
	case knownLimit(err):
		lines = append(lines, "\n"+m.printer.Stopped(m.limitText(err)))
		folded, foldAt = err, len(lines)
	case err != nil && longError(err):
		folded, foldAt = err, len(lines)
	case err != nil:
		lines = append(lines, m.printer.Error(err))
	}
	save := m.saveConversation()
	stopped := errors.Is(err, context.Canceled) || stopAsked
	if !stopped {
		save = tea.Batch(save, m.turnEnded(err))
	}
	if stopped && !sendingNow && m.session.Agent != nil {
		if queued := m.takeQueuedBack(); len(queued) > 0 {
			if typed := strings.TrimSpace(m.input.Value()); typed != "" {
				queued = append(queued, typed)
			}
			m.input.SetValue(joinQueued(queued))
			m.input.CursorEnd()
			m.queued = nil
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
	if (!stopped || sendingNow) && m.session.Agent != nil && m.session.Agent.HasQueued() {
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
	m.forgetHiddenDialog()
	if m.settings != nil && m.settings.floating {
		view = tea.NewView(m.floatingView())
		view.WindowTitle = "mo · settings"
	} else if m.settings != nil {
		view = tea.NewView(m.settings.form.View() + "\n" + dimStyle.Render(m.settings.helpLine()))
		view.WindowTitle = "mo · settings"
	} else if m.agentView != nil {
		view = tea.NewView(m.agentViewScreen())
		if info, ok := m.agentInfo(m.agentView.id); ok {
			view.WindowTitle = "mo · " + info.Name
		}
	} else if m.viewer != nil {
		view = tea.NewView(m.viewerView())
		view.WindowTitle = fmt.Sprintf("mo · job %d", m.viewer.id)
		if m.viewer.file != "" {
			view.WindowTitle = "mo · " + filepath.Base(m.viewer.file)
		}
	} else {
		screen, _ := m.screenLayout()
		drawn := m.highlight(screen)
		if m.uiOpen != 0 {
			drawn = m.overUI(drawn)
		}
		if m.popup != nil {
			drawn = m.overPopup(drawn)
		}
		view = tea.NewView(drawn)
		view.WindowTitle = "mo"
	}
	if m.printer.showsSixel() {
		view.Content = m.sixel.mark(view.Content)
	}
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	if screen := screenFor(m.session.Config.Render, m.terminalDark); screen.set {
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
	if waiting := m.queuedView(); waiting != "" {
		add(waiting, nil)
	}
	switch {
	case m.dialog != nil:
		add(m.dialogLayout())
	case m.running:
		queued := ""
		if count := m.session.Agent.QueuedCount(); count > 0 {
			queued = fmt.Sprintf(" · %d queued", count)
		}
		elapsed := ""
		if !m.runStarted.IsZero() {
			elapsed = " · " + formatElapsed(time.Since(m.runStarted))
		}
		escHint := " · esc to stop"
		if m.stopping != agent.StopNone {
			escHint = ""
		}
		add(m.spinnerView()+" "+m.currentState().label(true)+dimStyle.Render(elapsed+queued+escHint), nil)
	case m.worked > 0 && m.titling == nil:
		add(dimStyle.Render("  idle · worked "+formatElapsed(m.worked)), nil)
	}
	if m.titling != nil {
		stop := ""
		if !m.running && m.dialog == nil {
			stop = " · esc to stop"
		}
		add(m.spinnerView()+" "+m.titleState().label(true)+dimStyle.Render(stop), nil)
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
	input := m.shellInputView(m.input.View())
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

// conversation is the transcript and the reply being written, in a pane
// of the given height that follows the end unless the user scrolled up.
func (m model) conversation(height int) conversationPane {
	lines := m.conversationLines()
	conversation := conversationPane{width: m.mainWidth(), height: max(height, 1), total: lines.total}
	conversation.window = func(from, to int) []string {
		return m.markLines(lines.window(from, to), from)
	}
	conversation.SetYOffset(m.top)
	if !m.scrolled {
		conversation.GotoBottom()
	}
	return conversation
}

// emit adds printed output to the transcript. Kitty image uploads go to the
// terminal once, and only their placeholders stay in the transcript.
func (m model) emit(text string) (tea.Model, tea.Cmd) {
	// mo's own notes stand apart from what came before, as answers do.
	if strings.HasPrefix(ansi.Strip(text), noteMark) {
		text = "\n" + text
	}
	uploads := strings.Join(kittyUploads.FindAllString(text, -1), "")
	m.blocks = append(m.blocks, block{text: kittyUploads.ReplaceAllString(text, "")})
	if uploads == "" {
		return m, nil
	}
	return m, tea.Raw(uploads)
}

var kittyUploads = regexp.MustCompile(`\x1bPtmux;(?:[^\x1b]|\x1b\x1b)*\x1b\\|\x1b_G[^\x1b]*\x1b\\`)

func (m *model) scroll(up bool, lines int) {
	conversation := m.conversation(m.conversationHeight())
	if up {
		conversation.ScrollUp(lines)
	} else {
		conversation.ScrollDown(lines)
	}
	m.top = conversation.YOffset()
	m.scrolled = !conversation.AtBottom()
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
	m.forming, m.callsOpen = nil, false
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

// contextBar draws how full the context is in cells, in the style the
// settings pick.
func contextBar(share float64, cells int, fill, empty color.Color, style string) string {
	return barStyleNamed(style).draw(share, cells, fill, empty)
}

type statusColors struct {
	provider, model, label, separator, low, mid, high, empty, prompt, generation color.Color
	// promptSpeed and generationSpeed colour how fast the prompt was read
	// and the reply written.
	promptSpeed, generationSpeed color.Color
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
	c := ui(dark)
	return statusColors{
		provider: col(c.accent), model: col(c.purple), label: col(c.faint), separator: col(orDefault(c.separator, c.faint)),
		low: col(orDefault(c.gauge, c.green)), mid: col(c.warning), high: col(c.red), empty: col(c.empty),
		prompt: col(c.cyan), generation: col(c.teal),
		promptSpeed: col(orDefault(c.promptSpeed, c.cyan)), generationSpeed: col(orDefault(c.generationSpeed, c.teal)),
		base: col(c.statusBase), segment: col(c.segment), segmentDim: col(c.segmentDim),
		segmentDeep: col(c.segmentDeep), text: col(c.subtext), branch: col(c.warning),
	}
}

// shortModelName drops the directories and the extension from model ids that
// are file paths, as llama-server reports them.
// formatElapsed says how long something took: seconds, minutes and
// seconds, or hours and minutes.
func formatElapsed(d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm %02ds", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%dh %02dm", seconds/3600, seconds%3600/60)
}

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
		bridge.draw(func() string { return bridge.printer.Answer(output) })
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
		if status.Disabled {
			lines = append(lines, fmt.Sprintf("○ %s: off · /settings › Plugins turns it on", status.Name))
			continue
		}
		if status.Err != nil {
			lines = append(lines, fmt.Sprintf("✗ %s: %v", status.Name, status.Err))
			continue
		}
		var parts []string
		parts = append(parts, status.Tools...)
		parts = append(parts, status.Commands...)
		parts = append(parts, status.Hooks...)
		lines = append(lines, fmt.Sprintf("✓ %s: %s", status.Name, strings.Join(parts, ", ")))
		if line := globalKeysLine(status); line != "" {
			lines = append(lines, "  ! "+line)
		}
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
			lines = append(lines, fmt.Sprintf("○ %s: off · /mcp › %s › On turns it on", name, name))
		}
	}
	if len(statuses)+len(lines) == 0 {
		return "no MCP servers yet"
	}
	for _, status := range statuses {
		if mcphub.NeedsLogin(status.Err) {
			lines = append(lines, fmt.Sprintf("! %s: needs a sign-in · /mcp login %s", status.Name, status.Name))
			continue
		}
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

// slotClearer is a provider that can erase its server's slots: llama-server.
type slotClearer interface {
	ClearSlots(ctx context.Context) (int, error)
}

// cacheClearedMsg is how erasing the server's cache went.
type cacheClearedMsg struct {
	erased int
	err    error
}

// clearCache erases what llama-server holds in its slots, off the UI's
// goroutine.
func clearCache(provider llm.Provider) tea.Cmd {
	return func() tea.Msg {
		clearer, ok := provider.(slotClearer)
		if !ok {
			return cacheClearedMsg{err: llm.ErrNotLlamaServer}
		}
		ctx, cancel := context.WithTimeout(context.Background(), cacheClearTimeout)
		defer cancel()
		erased, err := clearer.ClearSlots(ctx)
		return cacheClearedMsg{erased: erased, err: err}
	}
}

const cacheClearTimeout = 30 * time.Second

func (m model) cacheCleared(msg cacheClearedMsg) (tea.Model, tea.Cmd) {
	switch {
	case errors.Is(msg.err, llm.ErrNotLlamaServer):
		return m.emit(m.printer.Note("/cache clear works with llama-server; with Ollama, ollama stop <model> unloads it, with LM Studio, lms unload"))
	case msg.err != nil:
		return m.emitError(fmt.Errorf("the cache was not cleared: %w", msg.err))
	}
	return m.emit(m.printer.Note(fmt.Sprintf("cache cleared: %d slots of llama-server erased · the next prompt is read whole", msg.erased)))
}

// checkModel asks the server what it serves before the conversation that was
// just started goes on, so the status line already says it.
func (m model) checkModel() tea.Cmd {
	current := m.session.Agent
	if current == nil || current.Refresh == nil {
		return nil
	}
	printer := m.printer
	return func() tea.Msg {
		var changed *modelMsg
		var notice string
		current.CheckModel(context.Background(), agent.Hooks{
			OnModel:   func(model string, window int) { changed = &modelMsg{model: model, window: window} },
			OnWarning: func(text string) { notice = text },
		}, agent.CheckNew)
		switch {
		case changed != nil:
			return *changed
		case notice != "":
			return printMsg(printer.Warning(notice))
		}
		return nil
	}
}

// modelChanged follows the model the server now has, and says it.
func (m model) modelChanged(msg modelMsg) (tea.Model, tea.Cmd) {
	was := m.session.Model
	m.session.Model = msg.model
	text := fmt.Sprintf("the server now has %s · context %s tokens", shortModelName(msg.model), formatTokens(msg.window))
	if msg.model == was {
		text = fmt.Sprintf("the server's context is now %s tokens", formatTokens(msg.window))
	}
	return m.emit(m.printer.Note(text))
}
