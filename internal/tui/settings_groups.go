package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

const groupPrefix = "group:"

// settingsGroup is a row of the settings menu that opens a few sections on
// one subject, and the provider or MCP server lists that belong with them.
type settingsGroup struct {
	key      string
	title    string
	sections []string
	extras   []string
}

var settingsGroups = []settingsGroup{
	{key: groupPrefix + "model", title: "Model", sections: []string{"model"}, extras: []string{menuProviders}},
	{key: groupPrefix + "conversation", title: "Conversation", sections: []string{"context", "instructions", "memory"}},
	{key: groupPrefix + "tools", title: "Tools", sections: []string{"tools", "toolmodes", "descriptions", "web"}, extras: []string{menuMCP}},
	{key: groupPrefix + "skills", title: "Skills", sections: []string{"skills", "skillmodes"}},
	{key: groupPrefix + "safety", title: "Safety", sections: []string{"permissions", "loop"}},
	{key: groupPrefix + "interface", title: "Interface", sections: []string{"appearance", "notify"}},
}

func sectionByKey(key string) (settingsSection, bool) {
	for _, section := range settingsSections {
		if section.key == key {
			return section, true
		}
	}
	return settingsSection{}, false
}

// groupOf is the group a section is in.
func groupOf(section settingsSection) (settingsGroup, bool) {
	for _, group := range settingsGroups {
		for _, key := range group.sections {
			if key == section.key {
				return group, true
			}
		}
	}
	return settingsGroup{}, false
}

// fullTitle names a section with its group when the group has more than
// it, as "Tools › Descriptions", and a nested one after its parent.
func fullTitle(section settingsSection) string {
	if parent, ok := sectionByKey(section.parent); ok {
		return fullTitle(parent) + " › " + section.title
	}
	if group, ok := groupOf(section); ok && len(group.sections) > 1 {
		return group.title + " › " + section.title
	}
	return section.title
}

// groupSummary is what a group row says: the values of its first
// section, which are what is most often looked for there.
func (m model) groupSummary(menu *settingsMenu, group settingsGroup) string {
	section, _ := sectionByKey(group.sections[0])
	return section.summary(menu.draft)
}

// listSections are the sections that stay a list of their own inside a
// group: long, and as long as the tools or skills there are.
var listSections = []string{"toolmodes", "skillmodes", "descriptions"}

// inGroup is the group a section's settings are shown in, directly: every
// section but the long lists.
func inGroup(section settingsSection) (settingsGroup, bool) {
	group, ok := groupOf(section)
	return group, ok && !slices.Contains(listSections, section.key)
}

const itemSeparator = "\x00"

func itemKey(section settingsSection, title string) string {
	return section.key + itemSeparator + title
}

// showGroup lists a group's settings with their values, then the lists
// that open on their own: the long ones, providers and MCP servers.
func (m model) showGroup(menu *settingsMenu, group settingsGroup, last string) (tea.Model, tea.Cmd) {
	type row struct{ title, value, key string }
	var rows, lists []row
	var notes []string
	items := map[string]struct {
		section settingsSection
		item    settingItem
	}{}
	for _, key := range group.sections {
		section, _ := sectionByKey(key)
		if _, direct := inGroup(section); !direct {
			lists = append(lists, row{section.title + " ›", section.summary(menu.draft), section.key})
			continue
		}
		if section.note != nil {
			if note := section.note(m, menu); note != "" {
				notes = append(notes, note)
			}
		}
		for _, item := range section.items(m, menu) {
			key := itemKey(section, item.title)
			items[key] = struct {
				section settingsSection
				item    settingItem
			}{section, item}
			rows = append(rows, row{item.title, item.value(menu.draft), key})
		}
	}
	for _, extra := range group.extras {
		switch extra {
		case menuProviders:
			lists = append(lists, row{"Providers ›", "add, edit, delete", menuProviders})
		case menuMCP:
			lists = append(lists, row{"MCP servers ›", fmt.Sprintf("%d set up · add, edit, delete", len(menu.layer.MCP)), menuMCP})
		}
	}
	rows = append(rows, lists...)
	width := 0
	for _, r := range rows {
		width = max(width, len(r.title))
	}
	var options []huh.Option[string]
	for _, r := range rows {
		options = append(options, huh.NewOption(fmt.Sprintf("%-*s   %s", width, r.title, dimStyle.Render(oneLine(r.value, shownValueLength))), r.key))
	}
	options = append(options, huh.NewOption("‹ back", menuBack))
	choice := last
	if choice == "" {
		choice = menu.lastIn[group.key]
	}
	if choice == "" && len(rows) > 0 {
		choice = rows[0].key
	}
	description := strings.Join(notes, "\n")
	list := huh.NewSelect[string]().Title("Settings › " + group.title).Description(description).Options(options...).Value(&choice)
	if m.height > 0 {
		list.Height(min(len(options)+2+lipgloss.Height(description), max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) {
		if choice == menuBack {
			return m.showMenu(menu)
		}
		if menu.lastIn == nil {
			menu.lastIn = map[string]string{}
		}
		menu.lastIn[group.key] = choice
		if picked, ok := items[choice]; ok {
			return m.editItem(menu, picked.section, picked.item)
		}
		return m.openMenuChoice(menu, choice)
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

// sectionBack is where esc from a section goes: the list a nested one was
// opened from, its group in the global menu, the menu in the project one.
func (m model) sectionBack(menu *settingsMenu, section settingsSection) (tea.Model, tea.Cmd) {
	if parent, ok := sectionByKey(section.parent); ok {
		return m.showSection(menu, parent, section.from)
	}
	if group, ok := groupOf(section); ok && menu.scope != projectScope {
		return m.showGroup(menu, group, section.key)
	}
	return m.showMenu(menu)
}
