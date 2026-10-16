package tui

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/notify"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/web"
)

const shownValueLength = 48

// settingItem is one setting of a section: a row with its current value in
// the section's list, and the field that edits it alone.
type settingItem struct {
	title string
	value func(d *settingsDraft) string
	field func(d *settingsDraft) huh.Field
}

// settingsSection is a row of the settings menu that opens a list of items.
type settingsSection struct {
	key     string
	title   string
	summary func(d *settingsDraft) string
	note    func(m model, menu *settingsMenu) string
	items   func(m model, menu *settingsMenu) []settingItem
	// parent is the section a nested one goes back to, with the row it
	// was opened from.
	parent, from string
}

var settingsSections = []settingsSection{
	{key: "model", title: "Model", summary: func(d *settingsDraft) string {
		summary := orDefault(d.provider, "auto") + " · " + modelValue(d)
		if d.effort != "" {
			summary += " · effort " + d.effort
		}
		return summary
	}, note: modelNote, items: modelItems},
	{key: "tools", title: "Loading and output", summary: func(d *settingsDraft) string {
		return d.mode + " · ask before: " + listValue(d.confirm)
	}, items: toolItems},
	{key: "context", title: "Context window", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("window %s · make room at %s · %s", windowValue(d), d.compactAt, d.strategy)
	}, note: func(m model, _ *settingsMenu) string {
		note := "Sent with every request now " + budgetText(m.session.Agent.Budget())
		if from := m.session.WindowFrom; from != "" {
			note += fmt.Sprintf("\nWindow now %s tokens, from %s", formatTokens(m.session.Agent.Window), from)
		}
		return note
	}, items: contextItems},
	{key: "descriptions", title: "Descriptions", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("%d tools", len(d.descriptions))
	}, note: func(model, *settingsMenu) string { return "an empty one sends the tool without it" }, items: descriptionItems},
	{key: "loop", title: "Loop guard", summary: func(d *settingsDraft) string {
		if !d.loopEnabled {
			return "off"
		}
		return fmt.Sprintf("on · same call %s× · same text %s×", d.loopRepeats, d.loopStreamRepeats)
	}, items: loopItems},
	{key: "skills", title: "Skills", summary: skillsSummary, note: skillsNote, items: skillItems},
	{key: "instructions", title: "Instructions", summary: func(d *settingsDraft) string {
		if !d.instructionsOn {
			return "off"
		}
		return strings.Join(ruleLines(d.instructionsFiles), ", ")
	}, note: instructionsNote, items: instructionItems},
	{key: "toolmodes", title: "Mode of each tool", summary: func(d *settingsDraft) string {
		return modesSummary(d, toolModes, mcpModes)
	}, note: func(model, *settingsMenu) string {
		return "full: declared with its description · compact: declared without · search: found with tool_search\n" +
			"group, for an MCP server: one entry in tool_search under the server's name, which gives all its tools"
	}, items: toolModeItems},
	{key: "skillmodes", title: "Mode of each skill", summary: func(d *settingsDraft) string {
		return modesSummary(d, skillModes)
	}, note: func(m model, _ *settingsMenu) string {
		if len(m.session.Skills) == 0 {
			return "no skills found"
		}
		return "full: in the skill tool with what it is for · compact: by name · search: found with skill_search"
	}, items: skillModeItems},
	{key: "web", title: "Web search", summary: func(d *settingsDraft) string { return d.search }, items: webItems},
	{key: "permissions", title: "Permissions", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("%s · %d allowed · %d never", d.permMode, len(ruleLines(d.allow)), len(ruleLines(d.deny)))
	}, note: func(model, *settingsMenu) string {
		return "shift+tab changes the mode for the session · " + ruleSyntax
	}, items: permissionItems},
	{key: "notify", title: "Notifications", summary: func(d *settingsDraft) string {
		var when []string
		if d.notifyFinish {
			when = append(when, "turn ends")
		}
		if d.notifyApproval {
			when = append(when, "permission")
		}
		if len(when) == 0 || d.notifyMethod == string(notify.MethodOff) {
			return "off"
		}
		return d.notifyMethod + " · " + strings.Join(when, ", ")
	}, note: func(model, *settingsMenu) string { return "/notify sends a test" }, items: notifyItems},
	{key: "appearance", title: "Look and keys", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("keys %s · panel %s · status %s", d.keys, d.sidebar, d.statusStyle)
	}, items: appearanceItems},
	{key: "memory", title: "Memory", summary: func(d *settingsDraft) string { return d.memoryDir }, items: memoryItems},
}

func modelItems(m model, menu *settingsMenu) []settingItem {
	cfg := menu.config(m.session.Config)
	return []settingItem{
		{"Provider", func(d *settingsDraft) string { return orDefault(d.provider, "auto") }, func(d *settingsDraft) huh.Field {
			options := providerOptions(cfg, menu.loaded.findings, d.initialProvider)
			if menu.scope == projectScope && menu.draft.provider != autoProvider {
				options = options[1:]
			}
			return huh.NewSelect[string]().Title("Provider").Options(options...).Value(&d.provider)
		}},
		{"Model", modelValue, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Model").Description("empty: the first model the server lists").
				Suggestions(menu.loaded.models).Value(&d.model)
		}},
		{"Effort", func(d *settingsDraft) string { return orDefault(d.effort, "model's default") }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Effort").
				Description("how hard a model that reasons thinks; one that does not take it answers with an error").
				Options(effortOptions...).Value(&d.effort)
		}},
		{"Max output tokens", countValue(func(d *settingsDraft) string { return d.maxTokens }, "provider's default"), func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Max output tokens").Description("0: the provider's default").Validate(validCount).Value(&d.maxTokens)
		}},
	}
}

var effortOptions = []huh.Option[string]{
	huh.NewOption("model's default: nothing is sent", ""),
	huh.NewOption("none: OpenAI only, no reasoning", "none"),
	huh.NewOption("minimal: OpenAI only", "minimal"),
	huh.NewOption("low", "low"),
	huh.NewOption("medium", "medium"),
	huh.NewOption("high", "high"),
	huh.NewOption("xhigh: Anthropic from Opus 4.7, some OpenAI models", "xhigh"),
	huh.NewOption("max: Anthropic only", "max"),
}

func modelNote(_ model, menu *settingsMenu) string {
	var notes []string
	if note := unusableNote(menu.loaded.findings); note != "" {
		notes = append(notes, "Found but not usable: "+strings.ReplaceAll(note, "\n", " · "))
	}
	switch {
	case menu.scope == projectScope:
		notes = append(notes, "Providers are set up in Global; here you pick one for this project.")
	case menu.project != nil && (menu.project.set["provider"] || menu.project.set["model"]):
		notes = append(notes, "This project picks its own model: tab to see it.")
	}
	return strings.Join(notes, "\n")
}

func toolItems(m model, menu *settingsMenu) []settingItem {
	names := toolNames(m.session)
	choice := func(title string, value func(d *settingsDraft) *[]string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return listValue(*value(d)) }, func(d *settingsDraft) huh.Field {
			return toolChoice(title, names, value(d))
		}}
	}
	items := []settingItem{
		{"Tool mode", func(d *settingsDraft) string { return d.mode }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Tool mode").Options(
				huh.NewOption("full: every tool with its description", string(tools.ModeFull)),
				huh.NewOption("compact: no descriptions", string(tools.ModeCompact)),
				huh.NewOption("search: load tools on demand", string(tools.ModeSearch)),
			).Value(&d.mode)
		}},
		{"Found tools in search mode", func(d *settingsDraft) string { return d.load }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Found tools in search mode").Options(
				huh.NewOption("context: stay as the search result, called through tool_call; the request never changes", string(tools.LoadContext)),
				huh.NewOption("declare: declared from the next request on; restarts the prompt cache once", string(tools.LoadDeclare)),
			).Value(&d.load)
		}},
		choice("Loaded from the start in search mode", func(d *settingsDraft) *[]string { return &d.alwaysLoaded }),
		choice("Ask before running", func(d *settingsDraft) *[]string { return &d.confirm }),
		choice("Disabled", func(d *settingsDraft) *[]string { return &d.disabled }),
	}
	if servers := mcpNames(m, menu); len(servers) > 0 {
		items = append(items, settingItem{"MCP servers off", func(d *settingsDraft) string { return listValue(d.mcpDisabled) }, func(d *settingsDraft) huh.Field {
			return toolChoice("MCP servers off", servers, &d.mcpDisabled).Description("not started: their tools are gone")
		}})
	}
	return append(items, []settingItem{
		{"Max tool output (bytes)", func(d *settingsDraft) string { return d.maxOutput }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Max tool output (bytes)").Validate(validCount).Value(&d.maxOutput)
		}},
		{"Tool calls run", func(d *settingsDraft) string { return d.calls }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Tool calls run").
				Description("the calls of a reply run one after another, in order, either way").Options(
				huh.NewOption("streamed: each as soon as it is written, asking what it needs while the reply goes on", "streamed"),
				huh.NewOption("reply: all once the reply ends, asked together", "reply"),
			).Value(&d.calls)
		}},
		{"Command output kept", func(d *settingsDraft) string { return d.outputs }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Command output kept").
				Description("the whole output of each bash command, for the model to read any part of later").Options(
				huh.NewOption("disk: in the conversation's folder, also after mo restarts", tools.OutputsDisk),
				huh.NewOption("memory: until mo exits, never on disk", tools.OutputsMemory),
				huh.NewOption("none: only its start and end, as the command returns them", tools.OutputsNone),
			).Value(&d.outputs)
		}},
		{"Command output for (turns)", func(d *settingsDraft) string {
			if strings.TrimSpace(d.outputTurns) == "0" || d.outputTurns == "" {
				return "the whole conversation"
			}
			return d.outputTurns + " turns"
		}, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Command output for (turns)").
				Description("how many of your turns the output of commands is kept for; 0 for the whole conversation").
				Validate(func(text string) error {
					if turns, err := strconv.Atoi(strings.TrimSpace(text)); err != nil || turns < 0 {
						return fmt.Errorf("use 0 or a number of turns")
					}
					return nil
				}).Value(&d.outputTurns)
		}},
	}...)
}

// mcpNames are the MCP servers set up, as the menu edits them, and those
// turned off already, so ticking one off never loses it.
func mcpNames(m model, menu *settingsMenu) []string {
	names := slices.Collect(maps.Keys(m.session.Config.MCP))
	if menu != nil {
		names = append(names, slices.Collect(maps.Keys(menu.layer.MCP))...)
		names = append(names, menu.draft.mcpDisabled...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// mcpSuffix marks an MCP server among the tools in Tool modes.
const mcpSuffix = " (MCP server)"

func modesSummary(d *settingsDraft, kinds ...modeKind) string {
	set := 0
	for _, kind := range kinds {
		set += len(d.modesOf(kind))
	}
	if set == 0 {
		return "all as the default"
	}
	return fmt.Sprintf("%d set apart", set)
}

// modeItem is one tool, server or skill with its mode, the default when it
// has none of its own.
func modeItem(title string, kind modeKind, name string, fallback func(d *settingsDraft) string, group bool) settingItem {
	options := []huh.Option[string]{
		huh.NewOption("as the default", ""),
		huh.NewOption("full: declared with its description", string(tools.ModeFull)),
		huh.NewOption("compact: declared without descriptions", string(tools.ModeCompact)),
		huh.NewOption("search: found when looked for", string(tools.ModeSearch)),
	}
	if group {
		options = append(options, huh.NewOption("group: one entry for the server, all its tools when looked for", string(tools.ModeGroup)))
	}
	return settingItem{title: title, value: func(d *settingsDraft) string {
		if mode := *d.modeOf(kind, name); mode != "" {
			return mode
		}
		return fallback(d) + " (default)"
	}, field: func(d *settingsDraft) huh.Field {
		return huh.NewSelect[string]().Title(title).Options(options...).Value(d.modeOf(kind, name))
	}}
}

// mcpToolPrefix starts the names of an MCP server's tools.
func mcpToolPrefix(server string) string {
	return "mcp__" + server + "__"
}

// toolModeItems are mo's tools and the plugins' one by one, and each MCP
// server as one row opening its mode and, unless it is a group, its tools.
func toolModeItems(m model, menu *settingsMenu) []settingItem {
	var items []settingItem
	toolDefault := func(d *settingsDraft) string { return d.mode }
	servers := mcpNames(m, menu)
	for _, name := range toolNames(m.session) {
		if slices.ContainsFunc(servers, func(server string) bool { return strings.HasPrefix(name, mcpToolPrefix(server)) }) {
			continue
		}
		items = append(items, modeItem(name, toolModes, name, toolDefault, false))
	}
	for _, server := range servers {
		item := modeItem(server+mcpSuffix, mcpModes, server, toolDefault, true)
		menu.opens(itemKey(settingsSection{key: "toolmodes"}, item.title), serverModeSection(server))
		items = append(items, item)
	}
	return items
}

// serverModeSection is an MCP server's own list: its mode, then its tools
// one by one, which only a server out of group mode has to set.
func serverModeSection(server string) settingsSection {
	toolDefault := func(d *settingsDraft) string { return d.mode }
	return settingsSection{
		key: "toolmodes:" + server, title: server + mcpSuffix, parent: "toolmodes", from: server + mcpSuffix,
		summary: func(d *settingsDraft) string { return orDefault(*d.modeOf(mcpModes, server), "as the default") },
		note: func(model, *settingsMenu) string {
			return "group: the model finds all its tools at once by the server's name, so they have no mode of their own"
		},
		items: func(m model, menu *settingsMenu) []settingItem {
			items := []settingItem{modeItem(server+mcpSuffix, mcpModes, server, toolDefault, true)}
			section := serverModeSection(server)
			if *menu.sectionDraft(section).modeOf(mcpModes, server) == string(tools.ModeGroup) {
				return items
			}
			for _, name := range toolNames(m.session) {
				if strings.HasPrefix(name, mcpToolPrefix(server)) {
					items = append(items, modeItem(strings.TrimPrefix(name, mcpToolPrefix(server)), toolModes, name, toolDefault, false))
				}
			}
			return items
		},
	}
}

func skillModeItems(m model, _ *settingsMenu) []settingItem {
	var items []settingItem
	for _, name := range m.session.Skills {
		items = append(items, modeItem(name, skillModes, name, func(d *settingsDraft) string { return d.skillMode }, false))
	}
	return items
}

func instructionsNote(m model, _ *settingsMenu) string {
	note := "files that tell the agent how to work, added to the system prompt:\n~/.config/mo, then each folder from the repository's root down to here"
	if len(m.session.Instructions) == 0 {
		return note + "\nnone found now"
	}
	return note + "\nread now: " + strings.Join(m.session.Instructions, ", ")
}

func instructionItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Read instructions files", func(d *settingsDraft) string { return yesNo(d.instructionsOn) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Read instructions files").Description("no: the system prompt is mo's alone").Value(&d.instructionsOn)
		}},
		{"File names", func(d *settingsDraft) string { return listValue(ruleLines(d.instructionsFiles)) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title("File names").Description("one per line, read in every folder where they are, e.g. CLAUDE.md").
				Lines(6).Value(&d.instructionsFiles)
		}},
	}
}

func skillsSummary(d *settingsDraft) string {
	return d.skillMode + " · off: " + listValue(d.skillsDisabled)
}

func skillsNote(m model, _ *settingsMenu) string {
	if len(m.session.Skills) == 0 {
		return "No skills found: add folders with a SKILL.md to ~/.config/mo/skills or the project's .mo/skills.\nWithout skills the model gets no skill tools."
	}
	return fmt.Sprintf("%d found: %s", len(m.session.Skills), strings.Join(m.session.Skills, ", "))
}

func skillItems(m model, menu *settingsMenu) []settingItem {
	items := []settingItem{
		{"Skill mode", func(d *settingsDraft) string { return d.skillMode }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Skill mode").Description("how the model learns which skills there are").Options(
				huh.NewOption("full: every skill with what it is for, in the skill tool", string(tools.ModeFull)),
				huh.NewOption("compact: only their names", string(tools.ModeCompact)),
				huh.NewOption("search: found with skill_list and skill_search", string(tools.ModeSearch)),
			).Value(&d.skillMode)
		}},
	}
	names := slices.Clone(m.session.Skills)
	if menu != nil {
		names = append(names, menu.draft.skillsDisabled...)
	}
	slices.Sort(names)
	if names = slices.Compact(names); len(names) > 0 {
		items = append(items, settingItem{"Skills off", func(d *settingsDraft) string { return listValue(d.skillsDisabled) }, func(d *settingsDraft) huh.Field {
			return toolChoice("Skills off", names, &d.skillsDisabled)
		}})
	}
	return append(items, settingItem{"More skill folders", func(d *settingsDraft) string { return orDefault(textValue(d.skillDirs), "none") }, func(d *settingsDraft) huh.Field {
		return huh.NewText().Title("More skill folders").
			Description("one per line, each holding skill folders, e.g. ~/.claude/skills\nbesides ~/.config/mo/skills and the project's .mo/skills").
			Lines(6).Value(&d.skillDirs)
	}})
}

func contextItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Context window (tokens)", windowValue, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Context window (tokens)").Description("0: the model's own, as its server says; set to use another").
				Validate(validCount).Value(&d.window)
		}},
		{"Make room at", func(d *settingsDraft) string { return d.compactAt }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Make room at").Description("share of the window, 0 never").
				Validate(validFraction).Value(&d.compactAt)
		}},
		{"To make room", func(d *settingsDraft) string { return d.strategy }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("To make room").Options(
				huh.NewOption("summarize the older messages", "compact"),
				huh.NewOption("drop the oldest messages", "drop"),
			).Value(&d.strategy)
		}},
		{"Send reasoning back", func(d *settingsDraft) string { return yesNo(d.keepReasoning) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Send the model its reasoning back").
				Description("no: it is only shown, which saves tokens · yes: OpenAI-compatible servers get it as reasoning_content; Anthropic never does").
				Value(&d.keepReasoning)
		}},
		{"System prompt", func(d *settingsDraft) string { return textValue(d.systemPrompt) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title("System prompt").Lines(12).Value(&d.systemPrompt)
		}},
		{"Summary prompt", func(d *settingsDraft) string { return textValue(d.compactPrompt) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title("Summary prompt").Description("used when compacting").Lines(8).Value(&d.compactPrompt)
		}},
	}
}

func descriptionItems(m model, menu *settingsMenu) []settingItem {
	var items []settingItem
	for _, name := range toolNames(m.session) {
		if _, shown := menu.draft.descriptions[name]; !shown {
			continue
		}
		items = append(items, settingItem{name, func(d *settingsDraft) string { return textValue(*d.descriptions[name]) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title(name).Description("empty sends the tool without one").Lines(6).Value(d.descriptions[name])
		}})
	}
	return items
}

func loopItems(model, *settingsMenu) []settingItem {
	count := func(title, description string, least int, value func(d *settingsDraft) *string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return *value(d) }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title(title).Description(description).Validate(atLeast(least)).Value(value(d))
		}}
	}
	return []settingItem{
		{"Watch for loops", func(d *settingsDraft) string { return onOff(d.loopEnabled) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Watch for loops").Value(&d.loopEnabled)
		}},
		count("Loop: same tool call, same result", "times before it is not run", 2, func(d *settingsDraft) *string { return &d.loopRepeats }),
		count("Loop: within the last replies", "replies of the model in this turn", 1, func(d *settingsDraft) *string { return &d.loopReplies }),
		count("Loop: same text in a row", "times before the reply is cut", 2, func(d *settingsDraft) *string { return &d.loopStreamRepeats }),
		count("Loop: warnings to the model", "per turn; the next loop stops the turn", 0, func(d *settingsDraft) *string { return &d.loopWarnings }),
	}
}

func webItems(_ model, menu *settingsMenu) []settingItem {
	items := []settingItem{
		{"Web search", func(d *settingsDraft) string { return d.search }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Web search").Options(
				huh.NewOption("DuckDuckGo: no key", web.SearchDuckDuckGo),
				huh.NewOption("SearXNG: your own instance", web.SearchSearXNG),
				huh.NewOption("Brave: needs a key", web.SearchBrave),
				huh.NewOption("Tavily: needs a key", web.SearchTavily),
			).Value(&d.search)
		}},
		{"SearXNG URL", func(d *settingsDraft) string { return orDefault(d.searchURL, "none") }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("SearXNG URL").Description("e.g. http://localhost:8888").Value(&d.searchURL)
		}},
		{"Search key variable", func(d *settingsDraft) string { return orDefault(d.searchKeyEnv, "none") }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Search key variable").Description("environment variable holding the Brave or Tavily key").Value(&d.searchKeyEnv)
		}},
		{"Search key", searchKeyValue, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Search key").Description("saved in the settings file; empty keeps the one it has").
				EchoMode(huh.EchoModePassword).Value(&d.searchKey)
		}},
	}
	if menu.draft.savedKey {
		items = append(items, settingItem{"Forget the saved search key", func(d *settingsDraft) string { return yesNo(d.forgetKey) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Forget the saved search key").Value(&d.forgetKey)
		}})
	}
	return items
}

var modeOptions = []huh.Option[string]{
	huh.NewOption("ask: before every change and command; reading never asks", string(permissions.ModeAsk)),
	huh.NewOption("edits: file changes go through, commands and fetches ask", string(permissions.ModeEdits)),
	huh.NewOption("auto: like edits, and the model lets through what it judges safe", string(permissions.ModeAuto)),
	huh.NewOption("all: nothing asks, except deny rules", string(permissions.ModeAll)),
}

func permissionItems(model, *settingsMenu) []settingItem {
	rules := func(title, description string, value func(d *settingsDraft) *string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return ruleCount(*value(d)) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title(title).Description(description + "\n" + ruleSyntax + "\n" + ruleSyntaxTip).
				Lines(14).Validate(validRules).Value(value(d))
		}}
	}
	items := []settingItem{
		{"Permission mode", func(d *settingsDraft) string { return d.permMode }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Permission mode").Description("shift+tab changes it too; it is kept").Options(modeOptions...).Value(&d.permMode)
		}},
		rules("Allowed commands", "run without asking in every project", func(d *settingsDraft) *string { return &d.allow }),
		rules("Never without asking", "ask in every mode, also in all; wins over allowed", func(d *settingsDraft) *string { return &d.deny }),
	}
	return append(items, tryItem)
}

func ruleCount(text string) string {
	switch count := len(ruleLines(text)); count {
	case 0:
		return "none"
	case 1:
		return ruleLines(text)[0]
	default:
		return fmt.Sprintf("%d rules", count)
	}
}

func notifyItems(model, *settingsMenu) []settingItem {
	flag := func(title, description string, value func(d *settingsDraft) *bool) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return yesNo(*value(d)) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title(title).Description(description).Value(value(d))
		}}
	}
	return []settingItem{
		flag("Notify when a turn ends", "also when the model ends asking you something", func(d *settingsDraft) *bool { return &d.notifyFinish }),
		flag("Notify when it asks for permission", "", func(d *settingsDraft) *bool { return &d.notifyApproval }),
		{"Notify by", func(d *settingsDraft) string { return d.notifyMethod }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("How").Options(
				huh.NewOption("auto: the terminal's own when it has them, else the desktop's, else the bell", string(notify.MethodAuto)),
				huh.NewOption("terminal: Ghostty, kitty, WezTerm, iTerm2, foot; works over ssh", string(notify.MethodTerminal)),
				huh.NewOption("desktop: notify-send or macOS notifications", string(notify.MethodDesktop)),
				huh.NewOption("bell", string(notify.MethodBell)),
				huh.NewOption("off", string(notify.MethodOff)),
			).Value(&d.notifyMethod)
		}},
		flag("Notify while you look at mo", "no: only when the terminal is not focused", func(d *settingsDraft) *bool { return &d.notifyFocused }),
	}
}

func memoryItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Memory folder", func(d *settingsDraft) string { return d.memoryDir }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Memory folder").Description("where the notes the model keeps across conversations go").Value(&d.memoryDir)
		}},
	}
}

func appearanceItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Keys", func(d *settingsDraft) string { return d.keys }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Keys").Options(
				huh.NewOption("default: type and send, as in a chat", "default"),
				huh.NewOption("vim: insert, normal and visual modes; ctrl+h/j/k/l move between the panes; h and l go back and in through menus", "vim"),
			).Value(&d.keys)
		}},
		{"Background", func(d *settingsDraft) string { return d.background }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Background").
				Description("painted behind everything so mo looks the same in any terminal: #rrggbb, or terminal to keep its own").
				Validate(func(text string) error {
					if !config.ValidBackground(text) {
						return fmt.Errorf("use #rrggbb or terminal")
					}
					return nil
				}).Value(&d.background)
		}},
		{"Side panel", func(d *settingsDraft) string { return d.sidebar }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Side panel").Options(
				huh.NewOption("auto: tasks and background jobs beside the conversation when the terminal is wide enough", "auto"),
				huh.NewOption("off: tasks above the input instead", "off"),
			).Value(&d.sidebar)
		}},
		{"Status line", func(d *settingsDraft) string { return d.statusStyle }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Status line").Options(
				huh.NewOption("powerline: segments on backgrounds joined by arrows; needs a Nerd Font or a powerline font", statusPowerline),
				huh.NewOption("plain: text joined by │, for any font", statusPlain),
			).Value(&d.statusStyle)
		}},
		{"Icons", func(d *settingsDraft) string { return d.icons }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Icons").Description("in the status line: the branch and its changes").Options(
				huh.NewOption("nerd: Nerd Font icons \ue0a0 \U000f0752 \U000f11e7 \U000f1aa1 \U000f086f", iconsNerd),
				huh.NewOption("text: + ~ - » ? ! ✓, for any font", iconsText),
			).Value(&d.icons)
		}},
		{"Images, SVG and Mermaid", func(d *settingsDraft) string { return d.images }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Images, SVG and Mermaid").Options(
				huh.NewOption("auto: kitty graphics where the terminal has them, else colour blocks", string(render.ModeAuto)),
				huh.NewOption("kitty graphics", string(render.ModeKitty)),
				huh.NewOption("colour blocks", string(render.ModeBlocks)),
				huh.NewOption("off: only the file path", string(render.ModeOff)),
			).Value(&d.images)
		}},
	}
}

func modelValue(d *settingsDraft) string {
	if strings.TrimSpace(d.model) == "" {
		return "first listed"
	}
	return shortModelName(d.model)
}

func windowValue(d *settingsDraft) string {
	return countValue(func(d *settingsDraft) string { return d.window }, "auto")(d)
}

func countValue(value func(d *settingsDraft) string, zero string) func(d *settingsDraft) string {
	return func(d *settingsDraft) string {
		if text := strings.TrimSpace(value(d)); text != "0" && text != "" {
			return text
		}
		return zero
	}
}

func searchKeyValue(d *settingsDraft) string {
	switch {
	case strings.TrimSpace(d.searchKey) != "":
		return "new one typed"
	case d.savedKey && !d.forgetKey:
		return "saved"
	}
	return "none"
}

// textValue is the start of a text, on one line.
func textValue(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "(empty)"
	}
	if runes := []rune(text); len(runes) > shownValueLength {
		return string(runes[:shownValueLength-1]) + "…"
	}
	return text
}

func listValue(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return textValue(strings.Join(values, ", "))
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func yesNo(yes bool) string {
	if yes {
		return "yes"
	}
	return "no"
}
