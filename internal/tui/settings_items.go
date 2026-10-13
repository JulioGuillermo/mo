package tui

import (
	"fmt"
	"os"
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
}

var settingsSections = []settingsSection{
	{key: "model", title: "Model", summary: func(d *settingsDraft) string {
		return orDefault(d.provider, "auto") + " · " + modelValue(d)
	}, note: modelNote, items: modelItems},
	{key: "tools", title: "Tools", summary: func(d *settingsDraft) string {
		return d.mode + " · ask before: " + listValue(d.confirm)
	}, items: toolItems},
	{key: "context", title: "Context", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("window %s · make room at %s · %s", windowValue(d), d.compactAt, d.strategy)
	}, note: func(m model, _ *settingsMenu) string {
		return "Sent with every request now " + budgetText(m.session.Agent.Budget())
	}, items: contextItems},
	{key: "descriptions", title: "Tool descriptions", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("%d tools", len(d.descriptions))
	}, note: func(model, *settingsMenu) string { return "an empty one sends the tool without it" }, items: descriptionItems},
	{key: "loop", title: "Loop guard", summary: func(d *settingsDraft) string {
		if !d.loopEnabled {
			return "off"
		}
		return fmt.Sprintf("on · same call %s× · same text %s×", d.loopRepeats, d.loopStreamRepeats)
	}, items: loopItems},
	{key: "web", title: "Web search", summary: func(d *settingsDraft) string { return d.search }, items: webItems},
	{key: "permissions", title: "Permissions", summary: func(d *settingsDraft) string {
		return fmt.Sprintf("%s · %d allowed · %d never", d.permMode, len(ruleLines(d.allow))+len(ruleLines(d.projectAllow)), len(ruleLines(d.deny))+len(ruleLines(d.projectDeny)))
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
	{key: "display", title: "Memory and display", summary: func(d *settingsDraft) string {
		return "images " + d.images
	}, items: displayItems},
}

func modelItems(m model, menu *settingsMenu) []settingItem {
	cfg := menu.config(m.session.Config)
	return []settingItem{
		{"Provider", func(d *settingsDraft) string { return orDefault(d.provider, "auto") }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Provider").Options(providerOptions(cfg, menu.loaded.findings, d.initialProvider)...).Value(&d.provider)
		}},
		{"Model", modelValue, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Model").Description("empty: the first model the server lists").
				Suggestions(menu.loaded.models).Value(&d.model)
		}},
		{"Max output tokens", countValue(func(d *settingsDraft) string { return d.maxTokens }, "provider's default"), func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Max output tokens").Description("0: the provider's default").Validate(validCount).Value(&d.maxTokens)
		}},
	}
}

func modelNote(_ model, menu *settingsMenu) string {
	var notes []string
	if note := unusableNote(menu.loaded.findings); note != "" {
		notes = append(notes, "Found but not usable: "+strings.ReplaceAll(note, "\n", " · "))
	}
	if _, err := os.Stat(config.ProjectFile(".")); err == nil {
		notes = append(notes, "This project's .mo.toml also applies on top of these settings.")
	}
	return strings.Join(notes, "\n")
}

func toolItems(m model, _ *settingsMenu) []settingItem {
	names := toolNames(m.session)
	choice := func(title string, value func(d *settingsDraft) *[]string) settingItem {
		return settingItem{title, func(d *settingsDraft) string { return listValue(*value(d)) }, func(d *settingsDraft) huh.Field {
			return toolChoice(title, names, value(d))
		}}
	}
	return []settingItem{
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
		{"Max tool output (bytes)", func(d *settingsDraft) string { return d.maxOutput }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Max tool output (bytes)").Validate(validCount).Value(&d.maxOutput)
		}},
	}
}

func contextItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Context window (tokens)", windowValue, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Context window (tokens)").Description("0: ask the server, else 32768 / 200000").
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
		count("Same tool call, same result", "times before it is not run", 2, func(d *settingsDraft) *string { return &d.loopRepeats }),
		count("Within the last", "replies of the model in this turn", 1, func(d *settingsDraft) *string { return &d.loopReplies }),
		count("Same text in a row", "times before the reply is cut", 2, func(d *settingsDraft) *string { return &d.loopStreamRepeats }),
		count("Warnings to the model", "per turn; the next loop stops the turn", 0, func(d *settingsDraft) *string { return &d.loopWarnings }),
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
		{"Mode to start in", func(d *settingsDraft) string { return d.permMode }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Mode to start in").Options(modeOptions...).Value(&d.permMode)
		}},
		rules("Allowed commands", "run without asking in every project", func(d *settingsDraft) *string { return &d.allow }),
		rules("Never without asking", "ask in every mode, also in all; wins over allowed", func(d *settingsDraft) *string { return &d.deny }),
	}
	items = append(items,
		settingItem{"Mode in this project", func(d *settingsDraft) string { return orDefault(d.projectMode, "same") }, func(d *settingsDraft) huh.Field {
			options := append([]huh.Option[string]{huh.NewOption("same as the mode to start in", "")}, modeOptions...)
			return huh.NewSelect[string]().Title("Mode in this project").Description(dimProject()).Options(options...).Value(&d.projectMode)
		}},
		rules("Allowed in this project", "added to the allowed commands here only", func(d *settingsDraft) *string { return &d.projectAllow }),
		rules("Never without asking here", "added to the never list here only", func(d *settingsDraft) *string { return &d.projectDeny }),
		settingItem{"Try a command", func(d *settingsDraft) string { return orDefault(d.tryCommand, "see which rule decides") }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Try a command").DescriptionFunc(d.tryRules, &d.tryCommand).Value(&d.tryCommand)
		}},
	)
	return items
}

func dimProject() string {
	return "kept in your settings for this folder; the project's own .mo.toml can only ask more"
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
		flag("When a turn ends", "also when the model ends asking you something", func(d *settingsDraft) *bool { return &d.notifyFinish }),
		flag("When it asks for permission", "", func(d *settingsDraft) *bool { return &d.notifyApproval }),
		{"How", func(d *settingsDraft) string { return d.notifyMethod }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("How").Options(
				huh.NewOption("auto: the terminal's own when it has them, else the desktop's, else the bell", string(notify.MethodAuto)),
				huh.NewOption("terminal: Ghostty, kitty, WezTerm, iTerm2, foot; works over ssh", string(notify.MethodTerminal)),
				huh.NewOption("desktop: notify-send or macOS notifications", string(notify.MethodDesktop)),
				huh.NewOption("bell", string(notify.MethodBell)),
				huh.NewOption("off", string(notify.MethodOff)),
			).Value(&d.notifyMethod)
		}},
		flag("Also while you look at mo", "no: only when the terminal is not focused", func(d *settingsDraft) *bool { return &d.notifyFocused }),
	}
}

func displayItems(model, *settingsMenu) []settingItem {
	return []settingItem{
		{"Memory folder", func(d *settingsDraft) string { return d.memoryDir }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Memory folder").Value(&d.memoryDir)
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
