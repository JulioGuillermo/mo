package tui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const (
	settingsDetectTimeout = 3 * time.Second
	modelListTimeout      = 2 * time.Second
	settingsMinHeight     = 3
	maxToolChoiceHeight   = 14
	autoProvider          = ""
	settingsHelp          = "enter next · shift+tab previous field · ←→ yes/no · ctrl+j new line in a box · esc back"
)

type settingsKind int

const (
	generalSettings settingsKind = iota
	providerSettings
	mcpSettings
)

type settingsLoadedMsg struct {
	kind       settingsKind
	findings   []detect.Finding
	layer      config.Config
	project    config.Project
	projectErr error
	models     []string
	err        error
}

type settingsSavedMsg struct {
	session Session
	saved   []string
	err     error
}

// settingsScreen is a form over the user file. A form that saves turns the
// file into the new one with apply; a form that picks what to edit opens the
// next one with next. Esc opens back, the screen it came from, if any.
type settingsScreen struct {
	form  *huh.Form
	layer config.Config
	apply func(layer config.Config) (config.Config, error)
	next  func(m model) (tea.Model, tea.Cmd)
	back  func(m model) (tea.Model, tea.Cmd)
	// tab, when set, switches between the global and the project screen.
	tab  func(m model) (tea.Model, tea.Cmd)
	help string
	// project is the project file to save with the user file, and keepUser
	// leaves the user file alone when only the project's changed.
	project  *config.Config
	keepUser bool
}

func (screen *settingsScreen) helpLine() string {
	if screen.help != "" {
		return screen.help
	}
	return settingsHelp
}

// loadSettings gathers, off the UI goroutine, what the form shows: the user
// file, and for the provider forms the providers found and the models to
// suggest.
func loadSettings(session Session, kind settingsKind) tea.Cmd {
	return func() tea.Msg {
		layer, err := config.ReadUserLayer()
		if err != nil {
			return settingsLoadedMsg{err: fmt.Errorf("reading %s: %w", config.UserFile(), err)}
		}
		project, projectErr := config.ReadProject(session.WorkDir)
		if kind == mcpSettings {
			return settingsLoadedMsg{kind: kind, layer: layer, project: project, projectErr: projectErr}
		}
		ctx, cancel := context.WithTimeout(context.Background(), settingsDetectTimeout)
		defer cancel()
		findings := detect.Run(ctx, session.Config)
		models := append(currentModels(ctx, session), foundModels(findings)...)
		return settingsLoadedMsg{kind: kind, findings: findings, layer: layer, project: project, projectErr: projectErr, models: models}
	}
}

func (m model) openSettings(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	m.settingsInFlight = false
	if msg.err != nil {
		return m, printText(m.printer.Error(msg.err))
	}
	switch msg.kind {
	case providerSettings:
		return m.pickProvider(msg, nil)
	case mcpSettings:
		return m.pickMCPServer(msg, nil)
	}
	return m.openSettingsMenu(msg)
}

func (m model) showSettings(screen *settingsScreen) (tea.Model, tea.Cmd) {
	screen.form = fitForm(screen.form.WithTheme(huh.ThemeFunc(settingsTheme)), m.width, m.height)
	m.settings = screen
	return m, screen.form.Init()
}

// fitForm keeps the form and its help line inside the terminal.
func fitForm(form *huh.Form, width, height int) *huh.Form {
	return form.WithWidth(width).WithHeight(max(height-2, settingsMinHeight))
}

// updateSettings feeds the form and acts once it is completed or left.
func (m model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		redraw := m.resize(size)
		m.settings.form = fitForm(m.settings.form, size.Width, size.Height)
		return m, redraw
	}
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
		return m.clickSettings(click.X, click.Y)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "tab" && m.settings.tab != nil {
		screen := m.settings
		m.settings = nil
		return screen.tab(m)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && (key.String() == "esc" || key.String() == "ctrl+c") {
		screen := m.settings
		m.settings = nil
		if screen.back != nil && key.String() == "esc" {
			return screen.back(m)
		}
		return m, printText(m.printer.Note("settings unchanged"))
	}
	updated, cmd := m.settings.form.Update(msg)
	m.settings.form = updated.(*huh.Form)
	switch m.settings.form.State {
	case huh.StateCompleted:
		screen := m.settings
		m.settings = nil
		if screen.next != nil {
			return screen.next(m)
		}
		return m.saveOrWait(screen)
	case huh.StateAborted:
		m.settings = nil
		return m, printText(m.printer.Note("settings unchanged"))
	}
	return m, cmd
}

// saveSettings builds a session from the edited settings first and writes
// the file only when that works, so a bad choice never reaches the disk.
func saveSettings(screen *settingsScreen, current Session) tea.Cmd {
	return func() tea.Msg {
		layer, err := screen.apply(screen.layer)
		if err != nil {
			return settingsSavedMsg{err: err}
		}
		fresh, err := current.Reload(layer, screen.project)
		if err != nil {
			return settingsSavedMsg{err: fmt.Errorf("not saved, these settings do not work: %w", err)}
		}
		var saved []string
		if !screen.keepUser {
			if err := config.SaveUserLayer(layer); err != nil {
				fresh.discard()
				return settingsSavedMsg{err: err}
			}
			saved = append(saved, config.UserFile())
		}
		if screen.project != nil {
			if err := config.SaveProject(current.WorkDir, *screen.project); err != nil {
				fresh.discard()
				return settingsSavedMsg{err: err}
			}
			saved = append(saved, projectFileName)
		}
		fresh.activate()
		fresh.Agent.TakeConversation(current.Agent)
		keepMode(fresh, current)
		return settingsSavedMsg{session: fresh, saved: saved}
	}
}

func (m model) finishSettings(msg settingsSavedMsg) (tea.Model, tea.Cmd) {
	m.settingsInFlight = false
	if msg.err != nil {
		return m, printText(m.printer.Error(msg.err))
	}
	m.session = msg.session
	m.usage = m.session.Agent.LastUsage
	redraw := m.applyScreen(m.printer.isDark())
	note := fmt.Sprintf("settings saved to %s · %s · %s", strings.Join(msg.saved, " and "), m.session.Provider, shortModelName(m.session.Model))
	return m, tea.Batch(redraw, printText(m.printer.Note(note)))
}

func isAgentMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case streamMsg, streamEndMsg, usageMsg, interactionMsg, runDoneMsg, spinner.TickMsg, printMsg, reasoningMsg, drawMsg, redrawMsg, rulesSavedMsg, toolStartMsg, toolDoneMsg, savedMsg, jobEndedMsg, jobTickMsg, flashDoneMsg, activityMsg, titledMsg, titleEventMsg, titleSetMsg, repoMsg, loginMsg, tea.FocusMsg, tea.BlurMsg:
		return true
	}
	return false
}

// toolNames is every tool the session can have, built-in first, each once.
func toolNames(session Session) []string {
	if len(session.ToolNames) == 0 {
		return tools.BuiltinNames()
	}
	var names []string
	for _, name := range session.ToolNames {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func toolChoice(title string, names []string, value *[]string) *huh.MultiSelect[string] {
	height := min(len(names)+1, maxToolChoiceHeight)
	return huh.NewMultiSelect[string]().Title(title).Options(huh.NewOptions(names...)...).Height(height).Value(value)
}

func atLeast(least int) func(string) error {
	return func(text string) error {
		_, err := parseAtLeast(text, least)
		return err
	}
}

func validCount(text string) error {
	_, err := parseCount(text)
	return err
}

func validFraction(text string) error {
	_, err := parseFraction(text)
	return err
}

func budgetText(items []agent.BudgetItem) string {
	total := 0
	var parts []string
	for _, item := range items {
		total += item.Tokens
		parts = append(parts, fmt.Sprintf("%s %d", strings.TrimPrefix(item.Name, "tool "), item.Tokens))
	}
	return fmt.Sprintf("≈ %d tokens: %s", total, strings.Join(parts, " · "))
}

// providerOptions lists every provider mo knows or found, and the saved one
// even when it is neither, so leaving the field alone keeps it.
func providerOptions(cfg config.Config, findings []detect.Finding, saved string) []huh.Option[string] {
	labels := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(cfg.Providers)) {
		labels[name] = providerLabel(name, cfg.Providers[name])
	}
	for _, found := range findings {
		if !found.Usable {
			continue
		}
		label := found.Provider + "  · " + found.Origin
		if found.Note != "" {
			label += " · " + found.Note
		}
		labels[found.Provider] = label
	}
	if _, listed := labels[saved]; !listed && saved != autoProvider {
		labels[saved] = saved + "  · not found now"
	}
	options := []huh.Option[string]{huh.NewOption("auto  · the first local server that answers", autoProvider)}
	for _, name := range slices.Sorted(maps.Keys(labels)) {
		options = append(options, huh.NewOption(labels[name], name))
	}
	return options
}

func providerLabel(name string, settings config.ProviderConfig) string {
	switch {
	case !settings.NeedsKey():
		return name + "  · " + settings.BaseURL
	case settings.Key() != "":
		return name + "  · key found"
	}
	return name + "  · no key"
}

func unusableNote(findings []detect.Finding) string {
	var lines []string
	for _, found := range findings {
		if !found.Usable {
			lines = append(lines, fmt.Sprintf("%s: %s", found.Provider, found.Note))
		}
	}
	return strings.Join(lines, "\n")
}

// currentModels lists the models of the provider in use, the only one asked
// with its key, to suggest them.
func currentModels(ctx context.Context, session Session) []string {
	settings, ok := session.Config.Providers[session.Provider]
	if !ok || settings.Protocol != "openai" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelListTimeout)
	defer cancel()
	models, _ := (&llm.OpenAI{BaseURL: settings.BaseURL, APIKey: settings.Key()}).ListModels(ctx)
	return models
}

func foundModels(findings []detect.Finding) []string {
	var models []string
	for _, found := range findings {
		models = append(models, found.Models...)
	}
	return models
}

// keepMode keeps the mode shift+tab picked in the session, unless the saved
// settings changed the mode to start in.
func keepMode(fresh, current Session) {
	if fresh.Agent.Permissions == nil || current.Agent.Permissions == nil {
		return
	}
	if fresh.Config.Permissions.Mode == current.Config.Permissions.Mode {
		fresh.Agent.Permissions.SetMode(current.Agent.Permissions.Mode())
	}
}

// settingsTheme is huh's own, with descriptions and notes in grey so they
// stand apart from the settings and their values.
func settingsTheme(isDark bool) *huh.Styles {
	styles := huh.ThemeCharm(isDark)
	grey := lipgloss.Color("#737aa2")
	if !isDark {
		grey = lipgloss.Color("#8990b3")
	}
	styles.Focused.Description = styles.Focused.Description.Foreground(grey)
	styles.Blurred.Description = styles.Blurred.Description.Foreground(grey)
	styles.Group.Description = styles.Group.Description.Foreground(grey)
	return styles
}
