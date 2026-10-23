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
			note += fmt.Sprintf("\nWindow now %s tokens, from %s", formatTokens(m.session.Agent.ContextWindow()), from)
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
	{key: "agents", title: "Subagents", summary: func(d *settingsDraft) string {
		if !d.agentsEnabled {
			return "off"
		}
		return fmt.Sprintf("on · %s levels · %s at once", d.agentsDepth, d.agentsParallel)
	}, note: func(model, *settingsMenu) string {
		return "the model hands tasks to subagents with the agent tool; each has its own conversation and reports back"
	}, items: agentItems},
	{key: "peers", title: "Peers", summary: func(d *settingsDraft) string {
		if !d.peersEnabled {
			return "off"
		}
		where := "this computer"
		if d.peersNetwork != "off" {
			where = "network " + d.peersNetwork
		}
		return fmt.Sprintf("on · %s · %s · incoming %s · outgoing %s", orDefault(d.peersName, "folder@branch"), where, d.peersIncoming, d.peersOutgoing)
	}, note: func(model, *settingsMenu) string {
		return "the main agent sees the other mo windows of yours with peers on and sends them messages with the peers tool\n" +
			"on this computer through sockets only you can open; on the network over TLS, every connection proving the team's secret\n" +
			"/peers changes the name, approvals and network for a session"
	}, items: peerItems},
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
	{key: "skillmodes", title: "Each skill", summary: func(d *settingsDraft) string {
		return eachSkillSummary(d)
	}, note: func(m model, _ *settingsMenu) string {
		if len(m.session.Skills) == 0 {
			return "no skills found"
		}
		return "off: the model does not get it · full: in the skill tool with what it is for\n" +
			"compact: by name, skill_search tells what it is for · search: found with skill_list and skill_search"
	}, items: skillModeItems},
	{key: "web", title: "Web search", summary: func(d *settingsDraft) string { return d.search }, items: webItems},
	{key: "permissions", title: "Permissions", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("%s · %d allowed · %d never", d.permMode, len(ruleLines(d.allow)), len(ruleLines(d.deny)))
	}, note: func(model, *settingsMenu) string {
		return "shift+tab changes the mode for the session · in a rule, ! is an exception, !! always asks, !!! never runs"
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
	{key: "style", title: "Style", summary: func(d *settingsDraft) string {
		return d.uiTheme + " " + d.uiMode + " · code " + d.syntax
	}, items: styleItems},
	{key: "animations", title: "Animations", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("spinner %s · running %s · writing %s ms", d.spinnerMS, d.runningMS, d.writingMS)
	}, note: func(model, *settingsMenu) string {
		return "milliseconds between the frames of each animation; the screen is drawn as often as the quickest needs"
	}, items: animationItems},
	{key: "pluginuis", title: "Plugin UIs", summary: func(d *settingsDraft) string {
		return d.uiFPS + " frames a second at most"
	}, note: func(model, *settingsMenu) string {
		return "how often a plugin's canvas may draw itself again by itself, apart from mo's own animations"
	}, items: func(model, *settingsMenu) []settingItem {
		return []settingItem{{"Frames per second", func(d *settingsDraft) string { return d.uiFPS }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Frames per second").
				Description(fmt.Sprintf("the most a canvas is drawn again a second when it asks to, 1 to %d; a canvas may ask for fewer", config.MaxUIFPS)).
				Validate(validUIFPS).Value(&d.uiFPS)
		}}}
	}},
	{key: "memory", title: "Memory", summary: func(d *settingsDraft) string { return d.memoryDir }, items: memoryItems},
	pluginsSection,
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
		{"Max file size read whole", func(d *settingsDraft) string { return d.readMaxSize }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Max file size read whole").Description("e.g. 10MB; a larger file is read only in parts, by offset, limit or match; 0 for no limit").
				Validate(validSize).Value(&d.readMaxSize)
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
	for _, status := range m.session.MCP {
		names = append(names, status.Name)
	}
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

func skillModeItems(m model, menu *settingsMenu) []settingItem {
	var items []settingItem
	for _, name := range skillNames(m, menu) {
		items = append(items, skillItem(name))
	}
	return items
}

// skillNames are the skills found and those turned off already, so ticking
// one on never loses it.
func skillNames(m model, menu *settingsMenu) []string {
	names := slices.Clone(m.session.Skills)
	if menu != nil {
		names = append(names, menu.draft.skillsDisabled...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// skillOff is the choice that turns a skill off, in its row.
const skillOff = "off"

// skillItem is one skill: off, or on with its mode.
func skillItem(name string) settingItem {
	return settingItem{title: name, value: func(d *settingsDraft) string {
		if slices.Contains(d.skillsDisabled, name) {
			return skillOff
		}
		if mode := *d.modeOf(skillModes, name); mode != "" {
			return "on · " + mode
		}
		return "on · " + d.skillMode + " (default)"
	}, field: func(d *settingsDraft) huh.Field {
		return huh.NewSelect[string]().Title(name).Options(
			huh.NewOption("on, as the default mode", ""),
			huh.NewOption("on · full: in the skill tool with what it is for", string(tools.ModeFull)),
			huh.NewOption("on · compact: by name; skill_list and skill_search find it", string(tools.ModeCompact)),
			huh.NewOption("on · search: found with skill_list and skill_search", string(tools.ModeSearch)),
			huh.NewOption("off: the model does not get it", skillOff),
		).Accessor(&skillChoice{draft: d, name: name})
	}}
}

// skillChoice edits a skill's row: its mode, or the list of skills off.
type skillChoice struct {
	draft *settingsDraft
	name  string
}

func (c *skillChoice) Get() string {
	if slices.Contains(c.draft.skillsDisabled, c.name) {
		return skillOff
	}
	return *c.draft.modeOf(skillModes, c.name)
}

func (c *skillChoice) Set(value string) {
	off := slices.DeleteFunc(slices.Clone(c.draft.skillsDisabled), func(name string) bool { return name == c.name })
	if value == skillOff {
		off = append(off, c.name)
		slices.Sort(off)
	} else {
		*c.draft.modeOf(skillModes, c.name) = value
	}
	c.draft.skillsDisabled = off
}

func eachSkillSummary(d *settingsDraft) string {
	summary := modesSummary(d, skillModes)
	if len(d.skillsDisabled) > 0 {
		summary += fmt.Sprintf(" · %d off", len(d.skillsDisabled))
	}
	return summary
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
	return d.skillMode + " · " + eachSkillSummary(d)
}

func skillsNote(m model, _ *settingsMenu) string {
	if len(m.session.Skills) == 0 {
		return "No skills found: add folders with a SKILL.md to ~/.config/mo/skills or the project's .mo/skills.\nWithout skills the model gets no skill tools."
	}
	off := 0
	for _, name := range m.session.Skills {
		if slices.Contains(m.session.Config.Skills.Disabled, name) {
			off++
		}
	}
	return fmt.Sprintf("%d skills loaded, %d on · Each skill › turns one off or sets its mode\nyou load one by hand with $name or /skill name", len(m.session.Skills), len(m.session.Skills)-off)
}

func skillItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Skill mode", func(d *settingsDraft) string { return d.skillMode }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Skill mode").Description("how the model learns which skills there are").Options(
				huh.NewOption("full: every skill with what it is for, in the skill tool", string(tools.ModeFull)),
				huh.NewOption("compact: only their names, with skill_list and skill_search", string(tools.ModeCompact)),
				huh.NewOption("search: found with skill_list and skill_search", string(tools.ModeSearch)),
			).Value(&d.skillMode)
		}},
		{"More skill folders", func(d *settingsDraft) string { return orDefault(textValue(d.skillDirs), "none") }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title("More skill folders").
				Description("one per line, each holding skill folders, e.g. ~/.claude/skills\nbesides ~/.config/mo/skills and the project's .mo/skills").
				Lines(6).Value(&d.skillDirs)
		}},
	}
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
		{"Send reasoning back", func(d *settingsDraft) string { return reasoningChoice(d.keepReasoning) }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Send the model its reasoning back").
				Description("OpenAI-compatible servers get it as reasoning_content; Anthropic never does").
				Options(
					huh.NewOption("this turn's: what the model needs between its calls (Gemma and Qwen ask for it)", string(config.ReasoningTurn)),
					huh.NewOption("none: it is only shown, which saves tokens", string(config.ReasoningOff)),
					huh.NewOption("all of it", string(config.ReasoningAll)),
				).Value(&d.keepReasoning)
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

func agentItems(model, *settingsMenu) []settingItem {
	count := func(title, description string, value func(d *settingsDraft) *string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return *value(d) }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title(title).Description(description).Validate(atLeast(1)).Value(value(d))
		}}
	}
	return []settingItem{
		{"Subagents", func(d *settingsDraft) string { return onOff(d.agentsEnabled) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Subagents").Description("let the model start subagents").Affirmative("On").Negative("Off").Value(&d.agentsEnabled)
		}},
		count("Levels", "1: only the main agent starts them; 2: theirs may too; and so on", func(d *settingsDraft) *string { return &d.agentsDepth }),
		count("Working at once", "one waiting for its own does not count", func(d *settingsDraft) *string { return &d.agentsParallel }),
		count("Most per turn", "subagents started in one of your turns", func(d *settingsDraft) *string { return &d.agentsMax }),
		count("Steps each", "replies of the model before a subagent is stopped", func(d *settingsDraft) *string { return &d.agentsSteps }),
		{subagentsProvider, func(d *settingsDraft) string { return orDefault(d.agentsModel.provider, "the main agent's") }, nil},
		{subagentsModel, func(d *settingsDraft) string { return orDefault(d.agentsModel.model, "the main agent's") }, nil},
		{"Types in its description", func(d *settingsDraft) string { return typesListed(d.agentsListed) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Types in its description").
				Description("names the types in the agent tool's description, compactly; off: the model asks for them with action types").
				Affirmative("Names").Negative("Off").Value(&d.agentsListed)
		}},
	}
}

func peerItems(model, *settingsMenu) []settingItem {
	approval := func(title, description string, value func(d *settingsDraft) *string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return *value(d) }, func(d *settingsDraft) huh.Field {
			var options []huh.Option[string]
			for _, each := range config.PeerApprovals {
				options = append(options, huh.NewOption(each+": "+approvalDescription[each], each))
			}
			return huh.NewSelect[string]().Title(title).Description(description).Options(options...).Value(value(d))
		}}
	}
	return []settingItem{
		{"Peers", func(d *settingsDraft) string { return onOff(d.peersEnabled) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Peers").Description("let the model see the other mo windows and message them").Affirmative("On").Negative("Off").Value(&d.peersEnabled)
		}},
		{"Name", func(d *settingsDraft) string { return orDefault(d.peersName, "folder@branch") }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Name").Description("what the others call this window; auto: from the conversation's title; empty: folder@branch").Value(&d.peersName)
		}},
		approval("Incoming messages", "what asks before a peer's message reaches the model", func(d *settingsDraft) *string { return &d.peersIncoming }),
		approval("Outgoing messages", "what asks before the model sends a message", func(d *settingsDraft) *string { return &d.peersOutgoing }),
		{"Wake the model", func(d *settingsDraft) string { return onOff(d.peersWake) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Wake the model").Description("a message let in while the model is idle starts a turn; off, it waits for your next one").Affirmative("On").Negative("Off").Value(&d.peersWake)
		}},
		{"Network", func(d *settingsDraft) string { return d.peersNetwork }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Network").Description("peers on other computers too; it needs the secret").Options(
				huh.NewOption("off: this computer only", "off"),
				huh.NewOption("visible: found by the others on the local network", "visible"),
				huh.NewOption("hidden: not told of; reached only at its address", "hidden"),
			).Value(&d.peersNetwork)
		}},
		{"Port", func(d *settingsDraft) string {
			if strings.TrimSpace(d.peersPort) == "0" {
				return "any free one"
			}
			return d.peersPort
		}, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Port").Description("where this mo takes peers from the network; 0 for any free one, which only visible peers can be found on").Validate(func(text string) error {
				if port, err := parseCount(text); err != nil || port > 65535 {
					return fmt.Errorf("use 0, or 1 to 65535")
				}
				return nil
			}).Value(&d.peersPort)
		}},
		{"Addresses", func(d *settingsDraft) string { return ruleCount(d.peersAddresses) }, func(d *settingsDraft) huh.Field {
			return huh.NewText().Title("Addresses").Description("peers to ask directly, host:port a line: hidden ones, or across a VPN").Lines(6).Validate(validAddresses).Value(&d.peersAddresses)
		}},
		{"Secret", func(d *settingsDraft) string { return secretValue(d.peersSecret, d.peersSecretEnv) }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Secret").Description(fmt.Sprintf("shared by the team's computers; %d characters or more; /peers secret new makes one", config.PeerSecretMin)).
				EchoMode(huh.EchoModePassword).Validate(validSecret).Value(&d.peersSecret)
		}},
		{"Secret from a variable", func(d *settingsDraft) string { return orDefault(d.peersSecretEnv, "none") }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Secret from a variable").Description("the environment variable that holds it, when Secret is empty").Value(&d.peersSecretEnv)
		}},
	}
}

// secretValue says whether a secret is set and where, never what it is.
func secretValue(secret, variable string) string {
	switch {
	case secret != "":
		return "set"
	case variable != "":
		return "from $" + variable
	}
	return "none"
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
		count("Loop: steps in a turn", "replies of the model before the turn stops; 0 for no limit", 0, func(d *settingsDraft) *string { return &d.loopSteps }),
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
		rules("Allowed commands", "run without asking in every project, besides mo's own, which only read:\n"+strings.Join(defaultRulesShown(), " · "), func(d *settingsDraft) *string { return &d.allow }),
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

func appearanceItems(_ model, menu *settingsMenu) []settingItem {
	return append([]settingItem{
		{"Keys", func(d *settingsDraft) string { return d.keys }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Keys").Options(
				huh.NewOption("default: type and send, as in a chat", "default"),
				huh.NewOption("vim: insert, normal and visual modes; ctrl+h/j/k/l move between the panes; h and l go back and in through menus", "vim"),
			).Value(&d.keys)
		}},
		{"Side panel", func(d *settingsDraft) string { return d.sidebar }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Side panel").Options(
				huh.NewOption("auto: tasks and background jobs beside the conversation when the terminal is wide enough", "auto"),
				huh.NewOption("on: always beside the conversation, also in a narrow or vertical terminal", "on"),
				huh.NewOption("off: tasks above the input instead", "off"),
			).Value(&d.sidebar)
		}},
		{"Side panel width", func(d *settingsDraft) string { return d.sidebarWidth + " columns" }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Side panel width").
				Description(fmt.Sprintf("columns, %d to %d; %d by default · /sidebar width changes it for the session", config.MinSidebarWidth, config.MaxSidebarWidth, config.DefaultSidebarWidth)).
				Validate(validSidebarWidth).Value(&d.sidebarWidth)
		}},
		{"Status line", func(d *settingsDraft) string { return d.statusStyle }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Status line").Options(
				huh.NewOption("powerline: segments on backgrounds joined by arrows; needs a Nerd Font or a powerline font", statusPowerline),
				huh.NewOption("plain: text joined by │, for any font", statusPlain),
			).Value(&d.statusStyle)
		}},
		{"Context bar", func(d *settingsDraft) string {
			return barSample(barStyleNamed(d.bar)) + "  " + orDefault(d.bar, barStyles[0].name)
		}, func(d *settingsDraft) huh.Field {
			var options []huh.Option[string]
			for _, style := range barStyles {
				options = append(options, huh.NewOption(fmt.Sprintf("%s  %-12s %s", barSample(style), style.name, style.about), style.name))
			}
			return huh.NewSelect[string]().Title("Context bar").Description("how full the context is, as each looks at 65%").Options(options...).Value(&d.bar)
		}},
		{"Icons", func(d *settingsDraft) string { return d.icons }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Icons").Description("in the status line, the branch and its changes, and the file tree of /view").Options(
				huh.NewOption("nerd: Nerd Font icons \ue0a0 \U000f0752 \U000f11e7 \U000f1aa1 \U000f086f, and for files \ue627 \ue606 \ue609 \uf07b", iconsNerd),
				huh.NewOption("emoji: in the tree 📁 📂 🐹 🐍 📘 🔒, any terminal; the status line has plain marks", iconsEmoji),
				huh.NewOption("text: + ~ - » ? ! ✓ and ▸ ▾, for any font", iconsText),
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
		{"Cell size", func(d *settingsDraft) string { return d.cellSize }, func(d *settingsDraft) huh.Field {
			width, height := render.CellSize()
			return huh.NewInput().Title("Cell size").
				Description(fmt.Sprintf("pixels of a character cell, WIDTHxHEIGHT, to size kitty images; auto asks the terminal\nnow %dx%d; set it if images look stretched or blurry", width, height)).
				Validate(validCellSize).Value(&d.cellSize)
		}},
		{"Git blame in /view", func(d *settingsDraft) string { return onOff(d.blame) }, func(d *settingsDraft) huh.Field {
			return huh.NewConfirm().Title("Git blame in /view").
				Description("who last changed the cursor's line and in which commit, at its end; B turns it on and off there\nit reads the file's whole history, which takes seconds in a large repository").
				Affirmative("On").Negative("Off").Value(&d.blame)
		}},
	}, replyItems(menu)...)
}

// animationItems are the times between the frames of each animation.
func animationItems(model, *settingsMenu) []settingItem {
	frame := func(title, about, sample string, value func(d *settingsDraft) *string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return *value(d) + " ms  " + sample }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title(title).Description(fmt.Sprintf("%s: %s · ms between frames, %d or more", about, sample, config.MinFrameMS)).
				Validate(atLeast(config.MinFrameMS)).Value(value(d))
		}}
	}
	return []settingItem{
		frame("Status spinner", "the status line while the model works", "⠋⠙⠹", func(d *settingsDraft) *string { return &d.spinnerMS }),
		frame("Running", "a call, job or subagent at work", strings.Join(arcFrames, ""), func(d *settingsDraft) *string { return &d.runningMS }),
		frame("Writing a call", "a call the model is writing", strings.Join(formingFrames, ""), func(d *settingsDraft) *string { return &d.writingMS }),
		frame("Reasoning", "the model's reasoning as it comes", strings.Join(sparkFrames, ""), func(d *settingsDraft) *string { return &d.reasoningMS }),
		frame("Asking you", "what waits for your answer", strings.Join(askingFrames, ""), func(d *settingsDraft) *string { return &d.askingMS }),
	}
}

// replyItems are how often what the model writes is drawn, and how many
// milliseconds or tokens apart as the way chosen needs.
func replyItems(menu *settingsMenu) []settingItem {
	items := []settingItem{{"Reply refresh", func(d *settingsDraft) string { return d.replyRefresh }, func(d *settingsDraft) huh.Field {
		return huh.NewSelect[string]().Title("Reply refresh").
			Description("how often what the model writes is drawn; what asks, starts or ends is drawn at once. Drawing less leaves a local model's GPU to it").
			Options(
				huh.NewOption("live: every piece as it comes", config.RefreshLive),
				huh.NewOption("by time: every so many milliseconds", config.RefreshTime),
				huh.NewOption("by tokens: every so many pieces, a second apart at most", config.RefreshTokens),
			).Value(&d.replyRefresh)
	}}}
	if menu == nil || menu.draft == nil {
		return items
	}
	switch menu.draft.replyRefresh {
	case config.RefreshTime:
		items = append(items, settingItem{"Refresh every (ms)", func(d *settingsDraft) string { return d.replyEveryMS }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Refresh every (ms)").Description("e.g. 100; 10 or more").Validate(atLeast(10)).Value(&d.replyEveryMS)
		}})
	case config.RefreshTokens:
		items = append(items, settingItem{"Refresh every (tokens)", func(d *settingsDraft) string { return d.replyEveryTokens }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Refresh every (tokens)").Description("e.g. 10; 1 or more").Validate(atLeast(1)).Value(&d.replyEveryTokens)
		}})
	}
	return items
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

// reasoningChoice says how much reasoning goes back, for the settings list.
func reasoningChoice(choice string) string {
	switch config.Reasoning(choice) {
	case config.ReasoningOff:
		return "none"
	case config.ReasoningAll:
		return "all"
	}
	return "this turn's"
}

func typesListed(listed bool) string {
	if listed {
		return "names"
	}
	return "off: action types lists them"
}

// defaultRulesShown are mo's own rules for the shell commands run in.
func defaultRulesShown() []string {
	if tools.CurrentShell().PowerShell() {
		return config.DefaultAllowPowerShell
	}
	return config.DefaultAllow
}
