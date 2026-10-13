// Package tui is mo's interactive terminal UI. It runs inline: finished output
// goes to the normal terminal scrollback and only the prompt, the streaming
// answer and the status line are redrawn.
package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/notify"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/render"
)

type Session struct {
	Agent     *agent.Agent
	Provider  string
	Model     string
	Config    config.Config
	WorkDir   string
	Reload    func(userLayer config.Config) (Session, error)
	MCP       []mcphub.Status
	Plugins   *plugins.Set
	ToolNames []string

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

	running          bool
	cancel           context.CancelFunc
	finished         chan struct{}
	stream           string
	streamCut        int
	streamDone       string
	reasoning        string
	approval         *approvalMsg
	usage            llm.Usage
	speed            llm.Usage
	settings         *settingsScreen
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
	focused  bool
	viewport viewport.Model
	scrolled bool
}

const agentShutdownWait = 3 * time.Second

func Run(session Session) error {
	printer := newPrinter()
	printer.setImages(render.Detect(session.Config.Render.Images))
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
	}
	program := tea.NewProgram(m)
	bridge.program = program
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
	input.MaxHeight = 10
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
	start := printText(banner)
	if m.session.Continue {
		start = tea.Sequence(start, listConversations(m.session.WorkDir, true))
	}
	return tea.Batch(start, tea.RequestBackgroundColor, textarea.Blink)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		return m.openSettings(msg)
	case settingsSavedMsg:
		return m.finishSettings(msg)
	}
	if m.settings != nil && !isAgentMsg(msg) {
		return m.updateSettings(msg)
	}
	switch msg := msg.(type) {
	case printMsg:
		return m.emit(string(msg))
	case clipboardMsg:
		if msg.err != nil {
			return m.emit(m.printer.Error(msg.err))
		}
		if msg.image != "" {
			m.attach([]string{msg.image}, true)
		}
		m.attach(msg.files, false)
		return m, nil
	case tea.PasteMsg:
		if m.approval == nil && m.pastedPaths(msg.Content) {
			return m, nil
		}
	case rulesSavedMsg:
		return m.finishRules(msg)
	case savedMsg:
		if msg.err != nil {
			return m.emit(m.printer.Error(fmt.Errorf("the conversation was not saved: %w", msg.err)))
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
		if msg.Button == tea.MouseLeft && m.approval == nil {
			m.clickDetails(msg.Y)
		}
		return m, nil
	case toolStartMsg:
		m.addTool(msg.call, msg.summary)
		return m, nil
	case toolDoneMsg:
		m.finishTool(msg.id, msg.output, msg.isError)
		return m, nil
	case tea.MouseWheelMsg:
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
		m.printer.setDark(msg.IsDark())
		m.input.SetStyles(inputStyles(m.input.Styles(), msg.IsDark()))
		return m, m.scheduleRedraw()
	case tea.KeyPressMsg:
		m.focused = true
		return m.handleKey(msg)
	case streamMsg:
		if msg.Kind == llm.EventReasoning {
			m.reasoning += msg.Text
		} else {
			m.stream += msg.Text
			m.renderStream(false)
		}
		return m, nil
	case streamEndMsg:
		m.clearStream()
		return m, nil
	case usageMsg:
		m.usage = llm.Usage(msg)
		if msg.GenerationRate > 0 || msg.PromptRate > 0 {
			m.speed = llm.Usage(msg)
		}
		return m, nil
	case approvalMsg:
		m.approval = &msg
		return m, m.permissionAsked(msg)
	case tea.FocusMsg:
		m.focused = true
		return m, nil
	case tea.BlurMsg:
		m.focused = false
		return m, nil
	case runDoneMsg:
		return m.finishRun(msg.err)
	case spinner.TickMsg:
		if !m.running {
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
	m.printer.setWidth(size.Width)
	m.renderStream(true)
	if !widthChanged {
		return nil
	}
	return m.scheduleRedraw()
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.approval != nil {
		return m.answerApproval(msg.String())
	}
	if matches := m.suggestions(); len(matches) > 0 && !m.running {
		if handled, next, cmd := m.handleSuggestionKey(msg.String(), matches); handled {
			return next, cmd
		}
	}
	switch msg.String() {
	case "ctrl+v":
		return m, pasteClipboard()
	case "shift+tab":
		if policy := m.session.Agent.Permissions; policy != nil {
			policy.SetMode(policy.Mode().Next())
		}
		return m, nil
	case "ctrl+o":
		m.toggleDetails()
		return m, nil
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
		if m.running || m.settingsInFlight {
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

func (m model) answerApproval(pressed string) (tea.Model, tea.Cmd) {
	request := m.approval.request
	switch pressed {
	case "y":
		m.approval.reply <- agent.Allow
	case "a":
		if request.Ruled {
			if len(request.Commands) == 0 || request.Warning != "" {
				return m, nil
			}
			return m.editRules(*m.approval)
		}
		m.approval.reply <- agent.AllowAlways
	case "n":
		m.approval.reply <- agent.Deny
	case "esc", "ctrl+c":
		m.approval.reply <- agent.Deny
		m.stop()
	default:
		return m, nil
	}
	m.approval = nil
	return m, nil
}

func (m model) submit() (tea.Model, tea.Cmd) {
	prompt := strings.TrimSpace(m.input.Value())
	if prompt == "" {
		return m, nil
	}
	m.input.Reset()
	if strings.HasPrefix(prompt, "/") {
		return m.runCommand(prompt)
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
	case "notify":
		method := notify.Method(m.session.Config.Notify.Method)
		return m, tea.Batch(sendNotification(method, "test notification"), printText(m.printer.Note("sent a test notification · "+string(method))))
	case "rewind":
		return m.pickPrompt()
	case "help":
		return m, drawText(m.helpView)
	case "settings":
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
		return m, tea.Sequence(printText(m.printer.Note(mcpReport(m.session.MCP))), loadSettings(m.session, mcpSettings))
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
	m.approval = nil
}

func (m model) finishRun(err error) (tea.Model, tea.Cmd) {
	m.running = false
	m.cancel()
	m.approval = nil
	partial := strings.TrimSpace(m.stream)
	m.addReasoning(m.reasoning)
	m.clearStream()

	var lines []string
	if partial != "" {
		lines = append(lines, "\n"+m.printer.Markdown(partial))
	}
	switch {
	case errors.Is(err, context.Canceled):
		lines = append(lines, m.printer.Note("stopped"))
	case err != nil:
		lines = append(lines, m.printer.Error(err))
	}
	save := m.saveConversation()
	if !errors.Is(err, context.Canceled) {
		save = tea.Batch(save, m.turnEnded(err))
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
	} else {
		bottom := m.bottom()
		conversation := m.conversation(m.height - lipgloss.Height(bottom) - 1)
		view = tea.NewView(conversation.View() + "\n\n" + bottom)
		view.WindowTitle = "mo"
	}
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	return view
}

// bottom is what stays on the last rows: the approval or the spinner, the
// input and the status line.
func (m model) bottom() string {
	var sections []string
	switch {
	case m.approval != nil:
		sections = append(sections, m.approvalPrompt())
	case m.running:
		sections = append(sections, m.spinner.View()+workingStyle.Render(" working · esc to stop"))
	}
	if matches := m.suggestions(); len(matches) > 0 && !m.running {
		sections = append(sections, m.suggestionList(matches))
	}
	sections = append(sections, m.input.View(), m.statusLine())
	return strings.Join(sections, "\n")
}

// conversation is the transcript and the reply being written, in a viewport
// of the given height that follows the end unless the user scrolled up.
func (m model) conversation(height int) viewport.Model {
	conversation := m.viewport
	conversation.SetWidth(m.width)
	conversation.SetHeight(max(height, 1))
	transcript, _ := m.transcript()
	conversation.SetContent(strings.TrimRight(transcript+m.liveText(), "\n"))
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
	m.viewport = m.conversation(m.height - lipgloss.Height(m.bottom()) - 1)
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
	m.reasoning = ""
}

// liveText is what the model is writing right now, set apart from what came
// before like the answer it becomes: the complete lines as markdown, the line
// being written as it comes. Before any of it, its reasoning shows dimmed.
func (m model) liveText() string {
	reasoning := strings.TrimSpace(m.reasoning)
	if m.stream == "" {
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
	wrap := lipgloss.NewStyle().Width(max(m.width-4, 20)).PaddingLeft(2)
	if tail := strings.TrimSpace(m.stream[m.streamCut:]); tail != "" {
		parts = append(parts, wrap.Render(tail))
	}
	return strings.Join(parts, "\n")
}

// approvalPrompt is a framed question: the tool, what it will run, and the
// keys that answer.
func (m model) approvalPrompt() string {
	request := m.approval.request
	question := lipgloss.NewStyle().Bold(true).Render("Run " + request.Call.Name + "?")
	if request.Warning != "" {
		question += "  " + errorStyle.Render(request.Warning)
	}
	lines := []string{question, m.printer.command(request.Summary)}
	if request.Reason != "" {
		lines = append(lines, workingStyle.Render(request.Reason))
	}
	if request.Ruled && len(request.Commands) > 0 {
		lines = append(lines, dimStyle.Render("no rule allows: ")+strings.Join(request.Commands, dimStyle.Render(" · ")))
	}
	keys := keyStyle.Render("y") + " yes   " + keyStyle.Render("n") + " no"
	switch {
	case request.Warning != "":
	case request.Ruled && len(request.Commands) > 0:
		keys += "   " + keyStyle.Render("a") + " always: add a rule…"
	case !request.Ruled:
		keys += "   " + keyStyle.Render("a") + " always this session"
	}
	body := strings.Join(append(lines, keys), "\n")
	frame := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(warning).Padding(0, 1).Width(max(m.width-2, 20))
	return frame.Render(body)
}

// statusLine shows the permission mode, provider and model, how full the
// context is, with a bar that turns yellow and then red, and the last
// speeds, each in its colour. On a narrow terminal the least needed parts go
// first, down to the mode, model and how full the context is.
func (m model) statusLine() string {
	if m.width <= 0 {
		return m.status(statusParts{provider: true, hint: true, tokens: true, prompt: true, generation: true, bar: contextBarCells})
	}
	for _, parts := range []statusParts{
		{provider: true, hint: true, tokens: true, prompt: true, generation: true, bar: contextBarCells},
		{provider: true, tokens: true, prompt: true, generation: true, bar: contextBarCells},
		{tokens: true, prompt: true, generation: true, bar: contextBarCells},
		{tokens: true, generation: true, bar: contextBarCells},
		{generation: true, bar: contextBarCells},
		{bar: contextBarCells},
		{bar: 5},
		{},
	} {
		if status := m.status(parts); ansi.StringWidth(status) <= m.width {
			return status
		}
	}
	return ansi.Truncate(m.status(statusParts{}), m.width, "…")
}

// statusParts says which parts of the status line to show.
type statusParts struct {
	provider, hint, tokens, prompt, generation bool
	bar                                        int
}

func (m model) status(show statusParts) string {
	colors := statusPalette(m.printer == nil || m.printer.isDark())
	paint := func(color color.Color, text string) string {
		return lipgloss.NewStyle().Foreground(color).Render(text)
	}
	var parts []string
	if policy := m.session.Agent.Permissions; policy != nil {
		mode := policy.Mode()
		part := lipgloss.NewStyle().Foreground(colors.mode(mode)).Bold(true).Render("● " + string(mode))
		if show.hint {
			part += paint(colors.label, " shift+tab")
		}
		parts = append(parts, part)
	}
	if show.provider {
		parts = append(parts, lipgloss.NewStyle().Foreground(colors.provider).Bold(true).Render(m.session.Provider))
	}
	parts = append(parts, paint(colors.model, shortModelName(m.session.Model)))
	used := m.usage.InputTokens + m.usage.OutputTokens
	window := m.session.Agent.Window
	switch {
	case window > 0:
		share := float64(used) / float64(window)
		fill := colors.low
		switch {
		case share >= 0.8:
			fill = colors.high
		case share >= 0.5:
			fill = colors.mid
		}
		context := ""
		if show.tokens || show.bar > 0 {
			context = paint(colors.label, "ctx ")
		}
		if show.tokens {
			context += paint(fill, fmt.Sprintf("%s/%s ", formatTokens(used), formatTokens(window)))
		}
		if show.bar > 0 {
			context += contextBar(share, show.bar, fill, colors.empty) + " "
		}
		parts = append(parts, context+paint(fill, fmt.Sprintf("%.0f%%", share*100)))
	case used > 0:
		parts = append(parts, paint(colors.label, "ctx ")+paint(colors.low, formatTokens(used)))
	}
	if show.prompt && m.speed.PromptRate > 0 {
		parts = append(parts, paint(colors.label, "pp ")+paint(colors.prompt, fmt.Sprintf("%.0f t/s", m.speed.PromptRate)))
	}
	if show.generation && m.speed.GenerationRate > 0 {
		parts = append(parts, paint(colors.label, "tg ")+paint(colors.generation, fmt.Sprintf("%.1f t/s", m.speed.GenerationRate)))
	}
	return strings.Join(parts, paint(colors.label, " · "))
}

const contextBarCells = 10

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
		}
	}
	return statusColors{
		provider: lipgloss.Color("#2e7de9"), model: lipgloss.Color("#9854f1"), label: lipgloss.Color("#8990b3"),
		low: lipgloss.Color("#587539"), mid: lipgloss.Color("#8c6c3e"), high: lipgloss.Color("#f52a65"), empty: lipgloss.Color("#c4c8da"),
		prompt: lipgloss.Color("#007197"), generation: lipgloss.Color("#118c74"),
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

func mcpReport(statuses []mcphub.Status) string {
	if len(statuses) == 0 {
		return "no MCP servers yet"
	}
	var lines []string
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
