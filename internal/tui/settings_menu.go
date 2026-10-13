package tui

import (
	"errors"
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

// settingsMenu is the /settings screen: two menus, Global and This project,
// of sections, each opening its own form that comes back to the menu.
// Nothing reaches the files until the user picks save; providers and MCP
// servers edited from here wait too.
type settingsMenu struct {
	loaded  settingsLoadedMsg
	layer   config.Config
	draft   *settingsDraft
	last    string
	notice  string
	scope   settingsScope
	project *projectSettings
}

func (m model) newSettingsMenu(loaded settingsLoadedMsg) *settingsMenu {
	draft := newSettingsDraft(loaded.layer)
	draft.forProject(m.session.WorkDir, loaded.layer)
	if len(m.session.ToolDescriptions) > 0 {
		draft.describeTools(m.session.ToolDescriptions)
	}
	return &settingsMenu{loaded: loaded, layer: loaded.layer, draft: draft, last: settingsSections[0].key,
		project: newProjectSettings(m.session.WorkDir, loaded.layer, loaded.project, loaded.projectErr)}
}

func (m model) openSettingsMenu(loaded settingsLoadedMsg) (tea.Model, tea.Cmd) {
	menu := m.newSettingsMenu(loaded)
	section := m.openSection
	m.openSection = ""
	switch {
	case section == menuProject:
		return m.switchScope(menu)
	case section == "model" && (menu.project.set["provider"] || menu.project.set["model"]):
		menu.scope, menu.project.visited = projectScope, true
		return m.openMenuChoice(menu, section)
	case section != "":
		menu.last = section
		return m.openMenuChoice(menu, section)
	}
	return m.showMenu(menu)
}

func (m model) showMenu(menu *settingsMenu) (tea.Model, tea.Cmd) {
	last := &menu.last
	description := "apply in every project · " + config.UserFile()
	if menu.scope == projectScope {
		last = &menu.project.last
		description = menu.projectDescription()
	}
	choice := *last
	title := "Settings"
	if menu.changed() {
		title += "  · unsaved changes"
	}
	if menu.notice != "" {
		description = menu.notice
		menu.notice = ""
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Description(scopeTabs(menu.scope) + "\n" + description).
			Options(m.menuOptions(menu)...).Value(&choice),
	)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		*last = choice
		return m.openMenuChoice(menu, choice)
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.leaveMenu(menu) }
	tab := func(m model) (tea.Model, tea.Cmd) { return m.switchScope(menu) }
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, tab: tab, help: menuHelp + " · tab global/project"})
}

func (m model) menuOptions(menu *settingsMenu) []huh.Option[string] {
	width := len("MCP servers")
	for _, section := range settingsSections {
		width = max(width, len(section.title))
	}
	row := func(title, summary string) string {
		return fmt.Sprintf("%-*s   %s", width, title, dimStyle.Render(summary))
	}
	if menu.scope == projectScope {
		return m.projectOptions(menu, row)
	}
	var options []huh.Option[string]
	for _, section := range settingsSections {
		options = append(options, huh.NewOption(row(section.title, section.summary(menu.draft)), section.key))
	}
	options = append(options,
		huh.NewOption(row("Providers", "add, edit, delete"), menuProviders),
		huh.NewOption(row("MCP servers", fmt.Sprintf("%d set up · add, edit, delete", len(menu.layer.MCP))), menuMCP),
	)
	if menu.project != nil {
		options = append(options, huh.NewOption(row("This project ›", fmt.Sprintf("%d set in %s · tab", len(menu.project.set), projectFileName)), menuProject))
	}
	return append(options,
		huh.NewOption(keyStyle.Render("Save and close"), menuSave),
		huh.NewOption("Close without saving", menuDiscard),
	)
}

// changed tells whether saving would change a file.
func (menu *settingsMenu) changed() bool {
	return menu.userChanged() || menu.projectChanged()
}

// userChanged tells whether saving would change the user file.
func (menu *settingsMenu) userChanged() bool {
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
		_, err := menu.apply(menu.layer)
		project, projectErr := menu.projectLayer()
		if err = errors.Join(err, projectErr); err != nil {
			menu.notice = errorStyle.Render("not saved: " + err.Error())
			return m.showMenu(menu)
		}
		if !menu.changed() {
			return m, printText(m.printer.Note("settings unchanged"))
		}
		if !menu.projectChanged() {
			project = nil
		}
		return m.saveOrWait(&settingsScreen{layer: menu.layer, apply: menu.apply, project: project, keepUser: !menu.userChanged()})
	case menuDiscard:
		return m.leaveMenu(menu)
	case menuProject, menuGlobal:
		return m.switchScope(menu)
	case projectPermissions:
		return m.showSection(menu, projectPermissionSection, "")
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
	draft := menu.sectionDraft(section)
	overriding := menu.overriding(section)
	items := section.items(m, menu)
	if overriding {
		items = overridable(items)
	}
	width := 0
	for _, item := range items {
		width = max(width, len(item.title))
	}
	var options []huh.Option[string]
	for _, item := range items {
		label := fmt.Sprintf("%-*s   %s", width, item.title, dimStyle.Render(item.value(draft)))
		if overriding {
			label += menu.overrideMark(item)
		}
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
	if overriding {
		description = strings.TrimSpace("what you change is set for this project only\n" + description)
	}
	list := huh.NewSelect[string]().Title(menu.crumb(section)).Description(description).
		Options(options...).Value(&choice)
	if m.height > 0 {
		list.Height(min(len(options)+2+lipgloss.Height(description), max(m.height-4, settingsMinHeight)))
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	next := func(m model) (tea.Model, tea.Cmd) {
		for _, item := range items {
			switch {
			case item.title != choice:
			case overriding && menu.project.set[projectFields[item.title].key]:
				return m.chooseOverride(menu, section, item)
			default:
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
	draft := menu.sectionDraft(section)
	restore := draft.snapshot()
	field := item.field(draft)
	toSection := func(m model) (tea.Model, tea.Cmd) { return m.showSection(menu, section, item.title) }
	keep := toSection
	if menu.overriding(section) {
		keep = func(m model) (tea.Model, tea.Cmd) {
			menu.project.set[projectFields[item.title].key] = true
			return toSection(m)
		}
	}
	cancel := func(m model) (tea.Model, tea.Cmd) {
		restore()
		return toSection(m)
	}
	form := huh.NewForm(huh.NewGroup(field).Title(menu.crumb(section))).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: keep, back: cancel, help: fieldHelp(field)})
}

// crumb is the title of a section's screens, which says which screen it is in.
func (menu *settingsMenu) crumb(section settingsSection) string {
	if menu.scope == projectScope {
		return "This project › " + section.title
	}
	return "Settings › " + section.title
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
