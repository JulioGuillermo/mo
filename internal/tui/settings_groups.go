package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
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
// it, as "Tools › Descriptions".
func fullTitle(section settingsSection) string {
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

// showGroup lists the sections of a group with their values, and the
// lists that go with them.
func (m model) showGroup(menu *settingsMenu, group settingsGroup) (tea.Model, tea.Cmd) {
	type row struct{ title, summary, key string }
	var rows []row
	for _, key := range group.sections {
		section, _ := sectionByKey(key)
		rows = append(rows, row{section.title, section.summary(menu.draft), section.key})
	}
	for _, extra := range group.extras {
		switch extra {
		case menuProviders:
			rows = append(rows, row{"Providers", "add, edit, delete", menuProviders})
		case menuMCP:
			rows = append(rows, row{"MCP servers", fmt.Sprintf("%d set up · add, edit, delete", len(menu.layer.MCP)), menuMCP})
		}
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r.title))
	}
	var options []huh.Option[string]
	for _, r := range rows {
		options = append(options, huh.NewOption(fmt.Sprintf("%-*s   %s", width, r.title, dimStyle.Render(r.summary)), r.key))
	}
	options = append(options, huh.NewOption("‹ back", menuBack))
	choice := menu.lastIn[group.key]
	if choice == "" {
		choice = rows[0].key
	}
	list := huh.NewSelect[string]().Title("Settings › " + group.title).Options(options...).Value(&choice)
	if m.height > 0 {
		list.Height(min(len(options)+2, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) {
		if choice == menuBack {
			return m.showMenu(menu)
		}
		if menu.lastIn == nil {
			menu.lastIn = map[string]string{}
		}
		menu.lastIn[group.key] = choice
		return m.openMenuChoice(menu, choice)
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	form := huh.NewForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

// sectionBack is where esc from a section goes: its group in the global
// menu, the menu in the project one.
func (m model) sectionBack(menu *settingsMenu, section settingsSection) (tea.Model, tea.Cmd) {
	if group, ok := groupOf(section); ok && menu.scope != projectScope {
		return m.showGroup(menu, group)
	}
	return m.showMenu(menu)
}
