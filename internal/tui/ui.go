// Package tui is mo's interactive terminal UI. It runs inline: finished output
// goes to the normal terminal scrollback and only the prompt, the streaming
// answer and the status line are redrawn.
package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/render"
)

const helpText = `enter send · ctrl+j / alt+enter new line · esc / ctrl+c stop · ctrl+d quit
/clear forget the conversation · /compact summarize it · /settings provider, tools, context
/view <file> show an image, svg or mermaid file · /mcp MCP servers · /plugins plugins and their commands
/help this help · /quit exit`

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
	reasoning        string
	approval         *approvalMsg
	usage            llm.Usage
	settings         *settingsScreen
	settingsInFlight bool
}

const agentShutdownWait = 3 * time.Second

func Run(session Session) error {
	printer := newPrinter()
	printer.setImages(render.Detect(session.Config.Render.Images))
	printer.workDir = session.WorkDir
	bridge := &bridge{printer: printer, exited: make(chan struct{})}
	m := model{
		session: session,
		bridge:  bridge,
		printer: printer,
		input:   newInput(),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(toolStyle)),
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
	input.Prompt = "› "
	input.Placeholder = "ask anything · /help"
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = 10
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	input.Focus()
	return input
}

func (m model) Init() tea.Cmd {
	banner := m.printer.Note(fmt.Sprintf("mo · %s · %s · /help", m.session.Provider, shortModelName(m.session.Model)))
	for _, status := range m.session.Plugins.Status() {
		if status.Err != nil {
			banner += "\n" + m.printer.Error(fmt.Errorf("plugin %s: %v", status.Name, status.Err))
		}
	}
	return tea.Batch(tea.Println(banner), tea.RequestBackgroundColor, textarea.Blink)
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
	case tea.WindowSizeMsg:
		m.resize(msg)
		return m, nil
	case tea.BackgroundColorMsg:
		m.printer.setDark(msg.IsDark())
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case streamMsg:
		if msg.Kind == llm.EventReasoning {
			m.reasoning += msg.Text
		} else {
			m.stream += msg.Text
		}
		return m, nil
	case streamEndMsg:
		m.stream = ""
		m.reasoning = ""
		return m, nil
	case usageMsg:
		m.usage = llm.Usage(msg)
		return m, nil
	case approvalMsg:
		m.approval = &msg
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
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) resize(size tea.WindowSizeMsg) {
	m.width, m.height = size.Width, size.Height
	m.input.SetWidth(size.Width)
	m.printer.setWidth(size.Width)
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.approval != nil {
		return m.answerApproval(msg.String())
	}
	switch msg.String() {
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
		}
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
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) answerApproval(pressed string) (tea.Model, tea.Cmd) {
	switch pressed {
	case "y":
		m.approval.reply <- agent.Allow
	case "a":
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

	echo := tea.Println(m.printer.User(prompt))
	return m.start(echo, func(ctx context.Context, hooks agent.Hooks) error {
		return m.session.Agent.Run(ctx, prompt, hooks)
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
		m.session.Agent.Clear()
		m.usage = llm.Usage{}
		return m, tea.Println(m.printer.Note("conversation cleared"))
	case "help":
		return m, tea.Println(m.printer.Note(helpText))
	case "settings":
		m.settingsInFlight = true
		return m, tea.Sequence(tea.Println(m.printer.Note("looking for providers…")), loadSettings(m.session))
	case "view":
		if argument == "" {
			return m, tea.Println(m.printer.Note("usage: /view <image, svg or mermaid file>"))
		}
		return m, m.view(argument)
	case "plugins":
		return m, tea.Println(m.printer.Note(pluginReport(m.session.Plugins)))
	case "mcp":
		return m, tea.Println(m.printer.Note(mcpReport(m.session.MCP)))
	case "compact":
		return m.start(tea.Println(m.printer.Note("compacting…")), m.session.Agent.Compact)
	}
	run, found := m.session.Plugins.Command(name)
	if !found {
		return m, tea.Println(m.printer.Note("unknown command " + command + " · /help"))
	}
	return m.runPluginCommand(command, run, argument)
}

// view draws a file off the UI goroutine.
func (m model) view(path string) tea.Cmd {
	printer := m.printer
	return func() tea.Msg {
		return tea.Println(printer.File(path))()
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
	m.stream = ""
	m.reasoning = ""

	var lines []string
	if partial != "" {
		lines = append(lines, m.printer.Markdown(partial))
	}
	switch {
	case errors.Is(err, context.Canceled):
		lines = append(lines, m.printer.Note("stopped"))
	case err != nil:
		lines = append(lines, m.printer.Error(err))
	}
	if len(lines) == 0 {
		return m, nil
	}
	return m, tea.Println(strings.Join(lines, "\n"))
}

func (m model) View() tea.View {
	if m.settings != nil {
		view := tea.NewView(m.settings.form.View() + "\n" + dimStyle.Render(settingsHelp))
		view.WindowTitle = "mo · settings"
		return view
	}
	var sections []string
	if live := m.liveText(); live != "" {
		sections = append(sections, live)
	}
	switch {
	case m.approval != nil:
		sections = append(sections, m.approvalPrompt())
	case m.running:
		sections = append(sections, m.spinner.View()+dimStyle.Render(" working · esc to stop"))
	}
	sections = append(sections, m.input.View(), m.statusLine())
	view := tea.NewView(strings.Join(sections, "\n"))
	view.WindowTitle = "mo"
	return view
}

// liveText is the tail of what the model is writing right now, kept short so
// the redrawn area never grows past the screen.
func (m model) liveText() string {
	text := m.stream
	style := lipgloss.NewStyle()
	if text == "" {
		text = m.reasoning
		style = dimStyle
	}
	if text == "" {
		return ""
	}
	wrapped := lipgloss.NewStyle().Width(max(m.width-2, 20)).Render(strings.TrimSpace(text))
	lines := strings.Split(wrapped, "\n")
	maxLines := max(m.height-8, 3)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m model) approvalPrompt() string {
	request := m.approval.request
	question := toolStyle.Render("● "+request.Call.Name) + " " + firstLine(request.Summary)
	options := "[y]es · [n]o · [a]lways this session"
	if request.Warning != "" {
		question += errorStyle.Render("  (" + request.Warning + ")")
		options = "[y]es · [n]o"
	}
	return question + "\n" + lipgloss.NewStyle().Bold(true).Render("  run it? ") + dimStyle.Render(options)
}

func (m model) statusLine() string {
	status := fmt.Sprintf("%s · %s", m.session.Provider, shortModelName(m.session.Model))
	used := m.usage.InputTokens + m.usage.OutputTokens
	window := m.session.Agent.Window
	switch {
	case window > 0:
		status += fmt.Sprintf(" · ctx %s/%s", formatTokens(used), formatTokens(window))
	case used > 0:
		status += " · ctx " + formatTokens(used)
	}
	return dimStyle.Render(status)
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
	return m.start(tea.Println(m.printer.User(command)), func(ctx context.Context, _ agent.Hooks) error {
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
		return "no MCP servers; add them under [mcp.<name>] in " + config.UserFile()
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
