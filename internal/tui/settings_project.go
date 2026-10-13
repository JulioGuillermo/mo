package tui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
)

type settingsScope int

const (
	globalScope settingsScope = iota
	projectScope
)

const (
	menuProject        = "\x00project"
	menuGlobal         = "\x00global"
	projectPermissions = "project-permissions"
	overrideChange     = "change"
	overrideGlobal     = "global"
	projectFileName    = ".mo/settings.toml"
)

// projectField is a setting a project can override: its key in the file and
// the value of the draft that edits it.
type projectField struct {
	key  string
	bind func(d *settingsDraft) any
}

// projectFields names, by the title of their item, the settings the project
// screen shows. The rest are only global.
var projectFields = map[string]projectField{
	"Provider":                             {"provider", func(d *settingsDraft) any { return &d.provider }},
	"Model":                                {"model", func(d *settingsDraft) any { return &d.model }},
	"Max output tokens":                    {"max_tokens", func(d *settingsDraft) any { return &d.maxTokens }},
	"Tool mode":                            {"tools.mode", func(d *settingsDraft) any { return &d.mode }},
	"Found tools in search mode":           {"tools.load", func(d *settingsDraft) any { return &d.load }},
	"Loaded from the start in search mode": {"tools.always_loaded", func(d *settingsDraft) any { return &d.alwaysLoaded }},
	"Ask before running":                   {"tools.confirm", func(d *settingsDraft) any { return &d.confirm }},
	"Disabled":                             {"tools.disabled", func(d *settingsDraft) any { return &d.disabled }},
	"Max tool output (bytes)":              {"tools.max_output", func(d *settingsDraft) any { return &d.maxOutput }},
	"Context window (tokens)":              {"context.window", func(d *settingsDraft) any { return &d.window }},
	"Make room at":                         {"context.compact_at", func(d *settingsDraft) any { return &d.compactAt }},
	"To make room":                         {"context.strategy", func(d *settingsDraft) any { return &d.strategy }},
	"Send reasoning back":                  {"context.keep_reasoning", func(d *settingsDraft) any { return &d.keepReasoning }},
	"System prompt":                        {"system_prompt", func(d *settingsDraft) any { return &d.systemPrompt }},
	"Summary prompt":                       {"context.compact_prompt", func(d *settingsDraft) any { return &d.compactPrompt }},
	"Watch for loops":                      {"loop.enabled", func(d *settingsDraft) any { return &d.loopEnabled }},
	"Same tool call, same result":          {"loop.repeats", func(d *settingsDraft) any { return &d.loopRepeats }},
	"Within the last":                      {"loop.replies", func(d *settingsDraft) any { return &d.loopReplies }},
	"Same text in a row":                   {"loop.stream_repeats", func(d *settingsDraft) any { return &d.loopStreamRepeats }},
	"Warnings to the model":                {"loop.warnings", func(d *settingsDraft) any { return &d.loopWarnings }},
}

// projectSectionKeys are the sections of the project screen, besides its
// permissions.
var projectSectionKeys = []string{"model", "tools", "context", "loop"}

// projectSettings is the project screen: a draft that is the global one with
// what the project sets on top, and which settings those are.
type projectSettings struct {
	workDir string
	file    config.Project
	err     error
	// heldBack is set when the file is not trusted and asks for more than
	// that lets apply.
	heldBack bool
	draft    *settingsDraft
	set      map[string]bool
	last     string
	// visited is set once the screen is opened: only then is the file
	// saved, so saving the global settings never trusts it unseen.
	visited bool
}

func newProjectSettings(workDir string, user config.Config, file config.Project, err error) *projectSettings {
	cfg := config.WithProject(config.UserSettings(user), file.Layer)
	draft := draftFrom(cfg, cfg.Provider, config.EffectiveModel(cfg))
	return &projectSettings{workDir: workDir, file: file, err: err, heldBack: file.HeldBack(workDir, user),
		draft: draft, set: config.ProjectSets(file.Layer), last: projectSectionKeys[0]}
}

// sync gives every setting the project does not set its global value.
func (p *projectSettings) sync(global *settingsDraft) {
	for _, field := range projectFields {
		if !p.set[field.key] {
			copyValue(field.bind(p.draft), field.bind(global))
		}
	}
}

func copyValue(to, from any) {
	switch to := to.(type) {
	case *string:
		*to = *from.(*string)
	case *bool:
		*to = *from.(*bool)
	case *[]string:
		*to = slices.Clone(*from.(*[]string))
	}
}

// overriding tells whether a section edits the project's draft: every one
// of the project screen but its permissions, which are the user's.
func (menu *settingsMenu) overriding(section settingsSection) bool {
	return menu.scope == projectScope && section.key != projectPermissions
}

// sectionDraft is the draft a section edits.
func (menu *settingsMenu) sectionDraft(section settingsSection) *settingsDraft {
	if menu.overriding(section) {
		menu.project.sync(menu.draft)
		return menu.project.draft
	}
	return menu.draft
}

// projectLayer is the project file as the screen leaves it, or nil when it
// was not opened, so it is left as it is.
func (menu *settingsMenu) projectLayer() (*config.Config, error) {
	project := menu.project
	if project == nil || !project.visited || project.err != nil {
		return nil, nil
	}
	global, err := menu.apply(menu.layer)
	if err != nil {
		return nil, err
	}
	project.sync(menu.draft)
	draft := project.draft
	draft.initialProvider, draft.initialModel = menu.draft.provider, strings.TrimSpace(menu.draft.model)
	applied, err := draft.applyTo(global, menu.loaded.findings)
	if err != nil {
		return nil, fmt.Errorf("this project: %w", err)
	}
	layer := config.ProjectOverrides(config.UserSettings(applied), project.set, project.file.Layer)
	return &layer, nil
}

// projectChanged tells whether saving would write the project file: it
// differs, saving trusts what it holds back, or it is still the old .mo.toml.
func (menu *settingsMenu) projectChanged() bool {
	layer, err := menu.projectLayer()
	if err != nil {
		return true
	}
	if layer == nil {
		return false
	}
	file := menu.project.file
	moved := file.Path != "" && file.Path != config.ProjectFile(menu.project.workDir)
	return !reflect.DeepEqual(*layer, file.Layer) || menu.project.heldBack || moved
}

// switchScope goes from one screen to the other.
func (m model) switchScope(menu *settingsMenu) (tea.Model, tea.Cmd) {
	if menu.scope == globalScope && menu.project != nil {
		menu.scope = projectScope
		menu.project.visited = true
	} else {
		menu.scope = globalScope
	}
	return m.showMenu(menu)
}

// scopeTabs shows both screens, the open one lit, as the menu's first line.
func scopeTabs(scope settingsScope) string {
	global, project := keyStyle.Render("Global"), dimStyle.Render("This project")
	if scope == projectScope {
		global, project = dimStyle.Render("Global"), keyStyle.Render("This project")
	}
	return global + dimStyle.Render("  │  ") + project + dimStyle.Render("   tab switches")
}

func (menu *settingsMenu) projectDescription() string {
	lines := []string{projectFileName + " · what is set here wins; the rest follows Global"}
	project := menu.project
	switch {
	case project.err != nil:
		lines = append(lines, errorStyle.Render("cannot read it: "+project.err.Error()))
	case project.file.Path != "" && project.file.Path != config.ProjectFile(project.workDir):
		lines = append(lines, "read from the old .mo.toml; saving moves it to "+projectFileName)
	case project.heldBack:
		lines = append(lines, errorStyle.Render("not saved by you here (it came with the repository or changed since):")+
			"\nonly its harmless settings apply until you save it")
	}
	return strings.Join(lines, "\n")
}

func (m model) projectOptions(menu *settingsMenu, row func(title, summary string) string) []huh.Option[string] {
	var options []huh.Option[string]
	for _, section := range projectSections() {
		set := 0
		for _, item := range overridable(section.items(m, menu)) {
			if menu.project.set[projectFields[item.title].key] {
				set++
			}
		}
		summary := "as Global"
		if set > 0 {
			summary = fmt.Sprintf("%d set here · %s", set, section.summary(menu.sectionDraft(section)))
		}
		options = append(options, huh.NewOption(row(section.title, summary), section.key))
	}
	return append(options,
		huh.NewOption(row("Permissions", projectPermissionSection.summary(menu.draft)), projectPermissions),
		huh.NewOption(row("Global ›", "the settings for every project · tab"), menuGlobal),
		huh.NewOption(keyStyle.Render("Save and close"), menuSave),
		huh.NewOption("Close without saving", menuDiscard),
	)
}

func projectSections() []settingsSection {
	var sections []settingsSection
	for _, section := range settingsSections {
		if slices.Contains(projectSectionKeys, section.key) {
			sections = append(sections, section)
		}
	}
	return sections
}

// overridable keeps the items a project can set.
func overridable(items []settingItem) []settingItem {
	var kept []settingItem
	for _, item := range items {
		if _, ok := projectFields[item.title]; ok {
			kept = append(kept, item)
		}
	}
	return kept
}

// overrideMark says, after a value in the project screen, where it comes from.
func (menu *settingsMenu) overrideMark(item settingItem) string {
	if menu.project.set[projectFields[item.title].key] {
		return keyStyle.Render("  ● this project")
	}
	return dimStyle.Render("  · global")
}

// chooseOverride asks what to do with a setting the project sets: change it,
// or go back to the global value.
func (m model) chooseOverride(menu *settingsMenu, section settingsSection, item settingItem) (tea.Model, tea.Cmd) {
	field := projectFields[item.title]
	choice := overrideChange
	global := item.value(menu.draft)
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("This project › "+item.title).Description("set here: "+item.value(menu.project.draft)).Options(
			huh.NewOption("Change it for this project", overrideChange),
			huh.NewOption("Use the global value  "+dimStyle.Render(global), overrideGlobal),
		).Value(&choice),
	)).WithShowHelp(false)
	back := func(m model) (tea.Model, tea.Cmd) { return m.showSection(menu, section, item.title) }
	next := func(m model) (tea.Model, tea.Cmd) {
		if choice == overrideGlobal {
			delete(menu.project.set, field.key)
			menu.project.sync(menu.draft)
			return back(m)
		}
		return m.editItem(menu, section, item)
	}
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

// projectPermissionSection holds the rules of this folder. They stay in the
// user's own file, so a cloned repository cannot allow commands.
var projectPermissionSection = settingsSection{key: projectPermissions, title: "Permissions",
	summary: func(d *settingsDraft) string {
		return fmt.Sprintf("%s · %d allowed · %d never", orDefault(d.projectMode, "mode as Global"), len(ruleLines(d.projectAllow)), len(ruleLines(d.projectDeny)))
	},
	note: func(model, *settingsMenu) string {
		return "kept in your own settings for this folder, not in " + projectFileName + ": a cloned repository cannot allow commands\n" + ruleSyntax
	},
	items: projectPermissionItems,
}

func projectPermissionItems(model, *settingsMenu) []settingItem {
	rules := func(title, description string, value func(d *settingsDraft) *string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return ruleCount(*value(d)) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title(title).Description(description + "\n" + ruleSyntax + "\n" + ruleSyntaxTip).
				Lines(14).Validate(validRules).Value(value(d))
		}}
	}
	return []settingItem{
		{"Mode in this project", func(d *settingsDraft) string { return orDefault(d.projectMode, "same as Global") }, func(d *settingsDraft) huh.Field {
			options := append([]huh.Option[string]{huh.NewOption("same as the global mode to start in", "")}, modeOptions...)
			return huh.NewSelect[string]().Title("Mode in this project").Options(options...).Value(&d.projectMode)
		}},
		rules("Allowed in this project", "added to the global allowed commands here only", func(d *settingsDraft) *string { return &d.projectAllow }),
		rules("Never without asking here", "added to the global never list here only", func(d *settingsDraft) *string { return &d.projectDeny }),
		tryItem,
	}
}

var tryItem = settingItem{"Try a command", func(d *settingsDraft) string { return orDefault(d.tryCommand, "see which rule decides") }, func(d *settingsDraft) huh.Field {
	return huh.NewInput().Title("Try a command").DescriptionFunc(d.tryRules, &d.tryCommand).Value(&d.tryCommand)
}}

// projectNotice warns, when mo starts, that the project file holds more
// than what applies because the user has not saved it here.
func projectNotice(workDir string, user config.Config) string {
	project, err := config.ReadProject(workDir)
	if err != nil || !project.HeldBack(workDir, user) {
		return ""
	}
	return "this project's settings were not saved by you here, so only their harmless part applies · /settings project to review and save them"
}
