package tui

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

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
	settingsHelp          = "enter next · shift+tab back · space toggle · esc cancel"
)

type settingsLoadedMsg struct {
	findings []detect.Finding
	layer    config.Config
	models   []string
	err      error
}

type settingsSavedMsg struct {
	session Session
	err     error
}

type settingsScreen struct {
	form     *huh.Form
	draft    *settingsDraft
	findings []detect.Finding
	layer    config.Config
}

// loadSettings gathers, off the UI goroutine, what the form shows: the user
// file, the providers found, and the models to suggest.
func loadSettings(session Session) tea.Cmd {
	return func() tea.Msg {
		layer, err := config.ReadUserLayer()
		if err != nil {
			return settingsLoadedMsg{err: fmt.Errorf("reading %s: %w", config.UserFile(), err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), settingsDetectTimeout)
		defer cancel()
		findings := detect.Run(ctx, session.Config)
		models := append(currentModels(ctx, session), foundModels(findings)...)
		return settingsLoadedMsg{findings: findings, layer: layer, models: models}
	}
}

func (m model) openSettings(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	m.settingsInFlight = false
	if msg.err != nil {
		return m, tea.Println(m.printer.Error(msg.err))
	}
	draft := newSettingsDraft(msg.layer)
	form := buildSettingsForm(m.session, draft, msg.findings, msg.models)
	form = fitForm(form, m.width, m.height)
	m.settings = &settingsScreen{form: form, draft: draft, findings: msg.findings, layer: msg.layer}
	return m, form.Init()
}

// fitForm keeps the form and its help line inside the terminal.
func fitForm(form *huh.Form, width, height int) *huh.Form {
	return form.WithWidth(width).WithHeight(max(height-2, settingsMinHeight))
}

// updateSettings feeds the form and acts once it is completed or left.
func (m model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.resize(size)
		m.settings.form = fitForm(m.settings.form, size.Width, size.Height)
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && (key.String() == "esc" || key.String() == "ctrl+c") {
		m.settings = nil
		return m, tea.Println(m.printer.Note("settings unchanged"))
	}
	updated, cmd := m.settings.form.Update(msg)
	m.settings.form = updated.(*huh.Form)
	switch m.settings.form.State {
	case huh.StateCompleted:
		screen := m.settings
		m.settings = nil
		m.settingsInFlight = true
		return m, saveSettings(screen, m.session)
	case huh.StateAborted:
		m.settings = nil
		return m, tea.Println(m.printer.Note("settings unchanged"))
	}
	return m, cmd
}

// saveSettings builds a session from the edited settings first and writes
// the file only when that works, so a bad choice never reaches the disk.
func saveSettings(screen *settingsScreen, current Session) tea.Cmd {
	return func() tea.Msg {
		layer, err := screen.draft.applyTo(screen.layer, screen.findings)
		if err != nil {
			return settingsSavedMsg{err: err}
		}
		fresh, err := current.Reload(layer)
		if err != nil {
			return settingsSavedMsg{err: fmt.Errorf("not saved, these settings do not work: %w", err)}
		}
		if err := config.SaveUserLayer(layer); err != nil {
			return settingsSavedMsg{err: err}
		}
		fresh.Agent.TakeConversation(current.Agent)
		return settingsSavedMsg{session: fresh}
	}
}

func (m model) finishSettings(msg settingsSavedMsg) (tea.Model, tea.Cmd) {
	m.settingsInFlight = false
	if msg.err != nil {
		return m, tea.Println(m.printer.Error(msg.err))
	}
	m.session = msg.session
	m.usage = m.session.Agent.LastUsage
	note := fmt.Sprintf("settings saved to %s · %s · %s", config.UserFile(), m.session.Provider, shortModelName(m.session.Model))
	return m, tea.Println(m.printer.Note(note))
}

func isAgentMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case streamMsg, streamEndMsg, usageMsg, approvalMsg, runDoneMsg, spinner.TickMsg:
		return true
	}
	return false
}

func buildSettingsForm(session Session, draft *settingsDraft, findings []detect.Finding, models []string) *huh.Form {
	return huh.NewForm(
		modelGroup(session.Config, draft, findings, models),
		toolsGroup(draft, toolNames(session)),
		contextGroup(session.Agent, draft),
		descriptionsGroup(draft),
		huh.NewGroup(
			huh.NewInput().Title("Memory folder").Value(&draft.memoryDir),
		).Title("Memory"),
	).WithShowHelp(false)
}

func modelGroup(cfg config.Config, draft *settingsDraft, findings []detect.Finding, models []string) *huh.Group {
	fields := []huh.Field{
		huh.NewSelect[string]().Title("Provider").Options(providerOptions(cfg, findings, draft.initialProvider)...).Value(&draft.provider),
		huh.NewInput().Title("Model").Description("empty: the first model the server lists").
			Suggestions(models).Value(&draft.model),
	}
	if note := unusableNote(findings); note != "" {
		fields = append(fields, huh.NewNote().Title("Found but not usable").Description(note))
	}
	if _, err := os.Stat(config.ProjectFile(".")); err == nil {
		fields = append(fields, huh.NewNote().Description("This project's .mo.toml also applies on top of these settings."))
	}
	return huh.NewGroup(fields...).Title("Model")
}

func toolsGroup(draft *settingsDraft, names []string) *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().Title("Tool mode").Options(
			huh.NewOption("full: every tool with its description", string(tools.ModeFull)),
			huh.NewOption("compact: no descriptions", string(tools.ModeCompact)),
			huh.NewOption("search: load tools on demand", string(tools.ModeSearch)),
		).Value(&draft.mode),
		toolChoice("Loaded from the start in search mode", names, &draft.alwaysLoaded),
		toolChoice("Ask before running", names, &draft.confirm),
		toolChoice("Disabled", names, &draft.disabled),
		huh.NewInput().Title("Max tool output (bytes)").Validate(validCount).Value(&draft.maxOutput),
	).Title("Tools")
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

func contextGroup(current *agent.Agent, draft *settingsDraft) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().Title("Sent with every request now").Description(budgetText(current.Budget())),
		huh.NewInput().Title("Context window (tokens)").Description("0: ask the server, else 32768 / 200000").
			Validate(validCount).Value(&draft.window),
		huh.NewInput().Title("Make room at").Description("share of the window, 0 never").
			Validate(validFraction).Value(&draft.compactAt),
		huh.NewSelect[string]().Title("To make room").Options(
			huh.NewOption("summarize the older messages", "compact"),
			huh.NewOption("drop the oldest messages", "drop"),
		).Value(&draft.strategy),
		huh.NewText().Title("System prompt").Lines(6).Value(&draft.systemPrompt),
	).Title("Context")
}

func descriptionsGroup(draft *settingsDraft) *huh.Group {
	var fields []huh.Field
	for _, name := range tools.BuiltinNames() {
		fields = append(fields, huh.NewInput().Title(name).Value(draft.descriptions[name]))
	}
	return huh.NewGroup(fields...).Title("Tool descriptions").Description("empty sends the tool without one")
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
