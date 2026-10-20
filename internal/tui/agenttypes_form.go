package tui

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

const (
	menuAgentTypes    = "\x00agenttypes"
	newTypeChoice     = "\x00newtype"
	typeProviderEntry = "Provider"
	typeModelEntry    = "Model"
)

// agentTypeDraft holds what the form edits for one type of subagent.
type agentTypeDraft struct {
	name        string
	isNew       bool
	description string
	model       providerModel
	effort      string
	prompt      string
	canStart    bool
	tools       []string
	skills      []string
	permissions string
	deny        string
	maxSteps    string
}

func newAgentTypeDraft(name string, kind config.AgentType) *agentTypeDraft {
	if name == newTypeChoice {
		return &agentTypeDraft{isNew: true}
	}
	steps := ""
	if kind.MaxSteps > 0 {
		steps = strconv.Itoa(kind.MaxSteps)
	}
	return &agentTypeDraft{
		name:        name,
		description: kind.Description,
		model:       splitProviderModel(kind.Model),
		effort:      kind.Effort,
		prompt:      kind.Prompt,
		canStart:    kind.CanStart,
		tools:       slices.Clone(kind.Tools),
		skills:      slices.Clone(kind.Skills),
		permissions: kind.Permissions,
		deny:        strings.Join(kind.Deny, "\n"),
		maxSteps:    steps,
	}
}

// agentType is the type as the draft has it, checked as the config is.
func (d *agentTypeDraft) agentType() (config.AgentType, error) {
	model, err := d.model.joined("model")
	if err != nil {
		return config.AgentType{}, err
	}
	kind := config.AgentType{
		Description: strings.TrimSpace(d.description),
		Model:       model,
		Effort:      d.effort,
		Prompt:      strings.TrimSpace(d.prompt),
		CanStart:    d.canStart,
		Tools:       d.tools,
		Skills:      d.skills,
		Permissions: d.permissions,
		Deny:        lines(d.deny),
	}
	if text := strings.TrimSpace(d.maxSteps); text != "" {
		steps, err := parseAtLeast(text, 1)
		if err != nil {
			return kind, fmt.Errorf("steps: %w", err)
		}
		kind.MaxSteps = steps
	}
	return kind, config.ValidateAgentType(strings.TrimSpace(d.name), kind)
}

// applyTo writes the type into the user layer.
func (d *agentTypeDraft) applyTo(layer config.Config) (config.Config, error) {
	kind, err := d.agentType()
	if err != nil {
		return layer, err
	}
	layer.Agents.Types = withEntry(layer.Agents.Types, strings.TrimSpace(d.name), kind)
	return layer, nil
}

func withoutAgentType(name string) func(config.Config) (config.Config, error) {
	return func(layer config.Config) (config.Config, error) {
		layer.Agents.Types = withoutEntry(layer.Agents.Types, name)
		if len(layer.Agents.Types) == 0 {
			layer.Agents.Types = nil
		}
		return layer, nil
	}
}

// entries are the fields of a type, a row each.
func (d *agentTypeDraft) entries(toolOptions, skillOptions []string, taken map[string]config.AgentType) []entry {
	var entries []entry
	if d.isNew {
		entries = append(entries, entry{"Name", func() string { return valueOr(d.name, "required") }, func() huh.Field {
			return huh.NewInput().Title("Name").Description("what the model asks for, e.g. reviewer; lowercase, digits, - and _").
				Validate(validTypeName(taken)).Value(&d.name)
		}})
	}
	return append(entries,
		entry{"Description", func() string { return valueOr(d.description, "none: say what it is for") }, func() huh.Field {
			return huh.NewText().Title("Description").Description("what it is for: the model reads it to pick the type").Lines(3).Value(&d.description)
		}},
		entry{typeProviderEntry, func() string { return valueOr(d.model.provider, "the subagents'") }, nil},
		entry{typeModelEntry, func() string { return valueOr(d.model.model, "the subagents'") }, nil},
		entry{"Effort", func() string { return orDefault(d.effort, "model's default") }, func() huh.Field {
			return huh.NewSelect[string]().Title("Effort").Options(effortOptions...).Value(&d.effort)
		}},
		entry{"Instructions", func() string { return valueOr(d.prompt, "none") }, func() huh.Field {
			return huh.NewText().Title("Instructions").Description("added to its system prompt, e.g. how to review and what to report").Lines(8).Value(&d.prompt)
		}},
		entry{"Starts subagents", func() string { return yesNo(d.canStart) }, func() huh.Field {
			return huh.NewConfirm().Title("Starts subagents").Description("it may start subagents of its own, while the levels allow").Value(&d.canStart)
		}},
		entry{"Tools", func() string { return chosenOrAll(d.tools) }, func() huh.Field {
			return toolChoice("Tools", toolOptions, &d.tools).Description("the only ones it has; none ticked: all the session has")
		}},
		entry{"Skills", func() string { return chosenOrAll(d.skills) }, func() huh.Field {
			return toolChoice("Skills", skillOptions, &d.skills).Description("the only ones it may load; none ticked: all")
		}},
		entry{"Permissions", func() string { return orDefault(d.permissions, "the session's") }, func() huh.Field {
			return huh.NewSelect[string]().Title("Permissions").Description("the stricter of this and the session's mode is used: never looser").Options(
				huh.NewOption("the session's", ""),
				huh.NewOption("ask: before any change or command", string(permissions.ModeAsk)),
				huh.NewOption("edits: file changes in the folder go through", string(permissions.ModeEdits)),
				huh.NewOption("auto: and what the model judges safe", string(permissions.ModeAuto)),
				huh.NewOption("all: when the session's is all too", string(permissions.ModeAll)),
			).Value(&d.permissions)
		}},
		entry{"Never without asking", func() string { return countOf(d.deny, "rule", "rules") }, func() huh.Field {
			return huh.NewText().Title("Never without asking").Description("deny rules besides the session's, one per line, e.g. git push *").
				Lines(6).Validate(validRules).Value(&d.deny)
		}},
		entry{"Steps", func() string { return valueOr(d.maxSteps, "as any subagent") }, func() huh.Field {
			return huh.NewInput().Title("Steps").Description("replies of the model before it is stopped; empty: as any subagent").Value(&d.maxSteps)
		}},
	)
}

func validTypeName(taken map[string]config.AgentType) func(string) error {
	return func(name string) error {
		name = strings.TrimSpace(name)
		if _, exists := taken[name]; exists {
			return fmt.Errorf("%s already exists; pick it in the list", name)
		}
		return config.ValidateAgentType(name, config.AgentType{})
	}
}

func chosenOrAll(names []string) string {
	if len(names) == 0 {
		return "all"
	}
	return strings.Join(names, ", ")
}

// typeToolOptions are the tools a type may be given: the session's that a
// subagent can have, all of an MCP server at once, and what the type has
// already.
func typeToolOptions(m model, menu *settingsMenu, chosen []string) []string {
	var names []string
	governedElsewhere := append([]string{tools.AgentName, tools.TellParentName}, tools.MainOnly...)
	for _, name := range toolNames(m.session) {
		if !slices.Contains(governedElsewhere, name) {
			names = append(names, name)
		}
	}
	servers := slices.Concat(slices.Collect(maps.Keys(m.session.Config.MCP)), slices.Collect(maps.Keys(menu.layer.MCP)))
	for _, server := range servers {
		names = append(names, "mcp__"+server+"__*")
	}
	return withChosen(names, chosen)
}

// withChosen adds what is chosen already to the names to pick from, so
// ticking leaves none out, each once.
func withChosen(names, chosen []string) []string {
	for _, name := range chosen {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return slices.Compact(names)
}

// pickAgentType lists the types to edit, then opens the chosen one.
func (m model) pickAgentType(menu *settingsMenu, last string) (tea.Model, tea.Cmd) {
	types := menu.layer.Agents.Types
	picked := last
	width := 0
	for name := range types {
		width = max(width, len(name))
	}
	var options []huh.Option[string]
	for _, name := range slices.Sorted(maps.Keys(types)) {
		options = append(options, huh.NewOption(fmt.Sprintf("%-*s   %s", width, name, dimStyle.Render(oneLine(typeSummary(types[name]), max(m.width-width-12, 20)))), name))
	}
	options = append(options, huh.NewOption("+ add a type", newTypeChoice), huh.NewOption("‹ back", menuBack))
	description := "kinds of subagent the model picks by name, each with its own model, tools and limits\nesc leaves them as they are"
	if menu.notice != "" {
		description, menu.notice = menu.notice, ""
	}
	form := newForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Settings › Agents › Types").Description(description).Options(options...).Value(&picked),
	)).WithShowHelp(false)
	back := func(m model) (tea.Model, tea.Cmd) { return m.showGroup(menu, agentsGroup(), menuAgentTypes) }
	next := func(m model) (tea.Model, tea.Cmd) {
		if picked == menuBack {
			return back(m)
		}
		return m.editAgentType(menu, picked)
	}
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

func agentsGroup() settingsGroup {
	for _, group := range settingsGroups {
		if group.key == groupPrefix+"agents" {
			return group
		}
	}
	return settingsGroup{}
}

// typeSummary is a type's row in the list: its model and what it has.
func typeSummary(kind config.AgentType) string {
	parts := []string{orDefault(kind.Model, "subagents' model")}
	if len(kind.Tools) > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", len(kind.Tools)))
	}
	if kind.CanStart {
		parts = append(parts, "starts subagents")
	}
	if kind.Description != "" {
		parts = append(parts, kind.Description)
	}
	return strings.Join(parts, " · ")
}

// editAgentType fills in a type a field at a time; it waits in the menu
// for Save and close, as the rest does.
func (m model) editAgentType(menu *settingsMenu, name string) (tea.Model, tea.Cmd) {
	draft := newAgentTypeDraft(name, menu.layer.Agents.Types[name])
	toolOptions := typeToolOptions(m, menu, draft.tools)
	skillOptions := withChosen(skillNames(m, menu), draft.skills)
	title := "Type " + name
	if draft.isNew {
		title = "New type of subagent"
	}
	e := &editor{
		title: title,
		open: func(m model, field string, back func(m model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd, bool) {
			switch field {
			case typeProviderEntry:
				shown, cmd := m.pickProviderThenModel(menu, typeProviderEntry, "the subagents': as Subagents' model says", &draft.model, back)
				return shown, cmd, true
			case typeModelEntry:
				shown, cmd := m.pickModelOf(menu, &draft.model, back)
				return shown, cmd, true
			}
			return m, nil, false
		},
		entries:   func() []entry { return draft.entries(toolOptions, skillOptions, menu.layer.Agents.Types) },
		saveLabel: "Keep",
		back:      func(m model) (tea.Model, tea.Cmd) { return m.pickAgentType(menu, name) },
		snapshot: func() func() {
			saved := *draft
			saved.tools, saved.skills = slices.Clone(draft.tools), slices.Clone(draft.skills)
			return func() { *draft = saved }
		},
	}
	keep := func(m model, apply func(config.Config) (config.Config, error), what string) (tea.Model, tea.Cmd) {
		layer, err := apply(menu.layer)
		if err != nil {
			e.notice = errorStyle.Render("not kept: " + err.Error())
			return m.showEditor(e, editorSave)
		}
		menu.layer = layer
		menu.notice = what + " · pick Save and close in the menu to keep it"
		return m.pickAgentType(menu, strings.TrimSpace(draft.name))
	}
	e.save = func(m model) (tea.Model, tea.Cmd) {
		return keep(m, draft.applyTo, "type "+strings.TrimSpace(draft.name)+" changed")
	}
	if !draft.isNew {
		e.removeLabel = "Delete this type"
		e.remove = func(m model) (tea.Model, tea.Cmd) {
			return keep(m, withoutAgentType(name), "type "+name+" deleted")
		}
	}
	return m.showEditor(e, "")
}
