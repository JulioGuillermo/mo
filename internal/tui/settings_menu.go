package tui

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/config"
)

const (
	menuSave      = "\x00save"
	menuDiscard   = "\x00discard"
	menuProviders = "\x00providers"
	menuMCP       = "\x00mcp"
	menuBack      = "\x00back"
	menuHelp      = "↑↓ choose · enter open · esc back"
)

// settingsMenu is the /settings screen: a menu of sections, each opening its
// own form that comes back to the menu. Nothing reaches the file until the
// user picks save; providers and MCP servers edited from here wait too.
type settingsMenu struct {
	loaded settingsLoadedMsg
	layer  config.Config
	draft  *settingsDraft
	last   string
	notice string
}

func (m model) openSettingsMenu(loaded settingsLoadedMsg) (tea.Model, tea.Cmd) {
	draft := newSettingsDraft(loaded.layer)
	draft.forProject(m.session.WorkDir, loaded.layer)
	if len(m.session.ToolDescriptions) > 0 {
		draft.describeTools(m.session.ToolDescriptions)
	}
	return m.showMenu(&settingsMenu{loaded: loaded, layer: loaded.layer, draft: draft, last: settingsSections[0].key})
}

func (m model) showMenu(menu *settingsMenu) (tea.Model, tea.Cmd) {
	choice := menu.last
	title := "Settings"
	if menu.changed() {
		title += "  · unsaved changes"
	}
	description := "enter opens a section · esc leaves"
	if menu.notice != "" {
		description = menu.notice
		menu.notice = ""
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Description(description).
			Options(menu.options()...).Value(&choice),
	)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		menu.last = choice
		return m.openMenuChoice(menu, choice)
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.leaveMenu(menu) }
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

func (menu *settingsMenu) options() []huh.Option[string] {
	width := len("MCP servers")
	for _, section := range settingsSections {
		width = max(width, len(section.title))
	}
	row := func(title, summary string) string {
		return fmt.Sprintf("%-*s   %s", width, title, dimStyle.Render(summary))
	}
	var options []huh.Option[string]
	for _, section := range settingsSections {
		options = append(options, huh.NewOption(row(section.title, section.summary(menu.draft)), section.key))
	}
	return append(options,
		huh.NewOption(row("Providers", "add, edit, delete"), menuProviders),
		huh.NewOption(row("MCP servers", fmt.Sprintf("%d set up · add, edit, delete", len(menu.layer.MCP))), menuMCP),
		huh.NewOption(keyStyle.Render("Save and close"), menuSave),
		huh.NewOption("Close without saving", menuDiscard),
	)
}

// changed tells whether saving would change the user file.
func (menu *settingsMenu) changed() bool {
	applied, err := menu.apply(menu.layer)
	return err != nil || !reflect.DeepEqual(applied, menu.loaded.layer)
}

// apply turns the file into the edited one: the providers and servers edited
// from the menu, then the sections.
func (menu *settingsMenu) apply(config.Config) (config.Config, error) {
	return menu.draft.applyTo(menu.layer, menu.loaded.findings)
}

// loadedNow is what the provider and MCP lists show: the file as edited so far.
func (menu *settingsMenu) loadedNow() settingsLoadedMsg {
	loaded := menu.loaded
	loaded.layer = menu.layer
	return loaded
}

// config adds the providers set up from the menu, not saved yet, to cfg.
func (menu *settingsMenu) config(cfg config.Config) config.Config {
	cfg.Providers = maps.Clone(cfg.Providers)
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.ProviderConfig{}
	}
	for name, settings := range config.UserSettings(menu.layer).Providers {
		if _, known := cfg.Providers[name]; !known {
			cfg.Providers[name] = settings
		}
	}
	return cfg
}

func (m model) openMenuChoice(menu *settingsMenu, choice string) (tea.Model, tea.Cmd) {
	switch choice {
	case menuSave:
		if _, err := menu.apply(menu.layer); err != nil {
			menu.notice = errorStyle.Render("not saved: " + err.Error())
			return m.showMenu(menu)
		}
		if !menu.changed() {
			return m, printText(m.printer.Note("settings unchanged"))
		}
		m.settingsInFlight = true
		return m, saveSettings(&settingsScreen{layer: menu.layer, apply: menu.apply}, m.session)
	case menuDiscard:
		return m.leaveMenu(menu)
	case menuProviders:
		return m.pickProvider(menu.loadedNow(), menu)
	case menuMCP:
		return m.pickMCPServer(menu.loadedNow(), menu)
	}
	for _, section := range settingsSections {
		if section.key == choice {
			return m.showSection(menu, section, "")
		}
	}
	return m.showMenu(menu)
}

// showSection lists the settings of a section with their values; each one
// opens on its own.
func (m model) showSection(menu *settingsMenu, section settingsSection, last string) (tea.Model, tea.Cmd) {
	items := section.items(m, menu)
	width := 0
	for _, item := range items {
		width = max(width, len(item.title))
	}
	var options []huh.Option[string]
	for _, item := range items {
		label := fmt.Sprintf("%-*s   %s", width, item.title, dimStyle.Render(item.value(menu.draft)))
		options = append(options, huh.NewOption(label, item.title))
	}
	options = append(options, huh.NewOption("‹ back", menuBack))
	choice := last
	if choice == "" && len(items) > 0 {
		choice = items[0].title
	}
	description := ""
	if section.note != nil {
		description = section.note(m, menu)
	}
	list := huh.NewSelect[string]().Title("Settings › " + section.title).Description(description).
		Options(options...).Value(&choice)
	if m.height > 0 {
		list.Height(min(len(options)+2+lipgloss.Height(description), max(m.height-4, settingsMinHeight)))
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	next := func(m model) (tea.Model, tea.Cmd) {
		for _, item := range items {
			if item.title == choice {
				return m.editItem(menu, section, item)
			}
		}
		return m.showMenu(menu)
	}
	form := huh.NewForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

// editItem opens one setting. Enter keeps the new value, esc puts the old
// one back; both go back to the section.
func (m model) editItem(menu *settingsMenu, section settingsSection, item settingItem) (tea.Model, tea.Cmd) {
	restore := menu.draft.snapshot()
	field := item.field(menu.draft)
	toSection := func(m model) (tea.Model, tea.Cmd) { return m.showSection(menu, section, item.title) }
	cancel := func(m model) (tea.Model, tea.Cmd) {
		restore()
		return toSection(m)
	}
	form := huh.NewForm(huh.NewGroup(field).Title("Settings › " + section.title)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: toSection, back: cancel, help: fieldHelp(field)})
}

func fieldHelp(field huh.Field) string {
	switch field.(type) {
	case *huh.Text:
		return "enter keep · ctrl+j new line · esc cancel"
	case *huh.MultiSelect[string]:
		return "↑↓ move · space or x toggle · enter keep · esc cancel"
	case *huh.Confirm:
		return "←→ yes/no · enter keep · esc cancel"
	case *huh.Select[string]:
		return "↑↓ choose · enter keep · esc cancel"
	}
	return "enter keep · esc cancel"
}

// leaveMenu closes the settings, asking first when that loses changes.
func (m model) leaveMenu(menu *settingsMenu) (tea.Model, tea.Cmd) {
	if !menu.changed() {
		return m, printText(m.printer.Note("settings unchanged"))
	}
	discard := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Close without saving?").Description("the changes made in this menu are lost").
			Affirmative("Discard").Negative("Keep editing").Value(&discard),
	)).WithShowHelp(false)
	back := func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	next := func(m model) (tea.Model, tea.Cmd) {
		if discard {
			return m, printText(m.printer.Note("settings unchanged"))
		}
		return m.showMenu(menu)
	}
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

// keepEdit applies a provider or MCP server form to the menu's copy of the
// file, saved later with the rest, and goes back to the menu.
func (m model) keepEdit(menu *settingsMenu, what string, apply func(config.Config) (config.Config, error)) (tea.Model, tea.Cmd) {
	layer, err := apply(menu.layer)
	if err != nil {
		menu.notice = errorStyle.Render("not kept: " + err.Error())
		return m.showMenu(menu)
	}
	if layer.Provider != menu.layer.Provider || layer.Model != menu.layer.Model {
		menu.draft.followProvider(layer)
	}
	menu.layer = layer
	menu.notice = what + " changed · pick save to keep it"
	return m.showMenu(menu)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
