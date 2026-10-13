package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
)

const (
	editorSave   = "\x00save"
	editorRemove = "\x00remove"
	editorBack   = "\x00back"
)

// entry is one field of an editor: a row with its value, and the field
// that edits it alone.
type entry struct {
	title string
	value func() string
	field func() huh.Field
}

// editor fills in one thing, a provider or an MCP server, a field at a
// time from a list of them with their values, then saves it. A field left
// with esc gets its old value back.
type editor struct {
	title       string
	entries     func() []entry
	save        func(m model) (tea.Model, tea.Cmd)
	saveLabel   string
	remove      func(m model) (tea.Model, tea.Cmd)
	removeLabel string
	back        func(m model) (tea.Model, tea.Cmd)
	snapshot    func() func()
	notice      string
}

func (m model) showEditor(e *editor, last string) (tea.Model, tea.Cmd) {
	entries := e.entries()
	width := 0
	for _, each := range entries {
		width = max(width, len(each.title))
	}
	var options []huh.Option[string]
	for _, each := range entries {
		options = append(options, huh.NewOption(fmt.Sprintf("%-*s   %s", width, each.title, dimStyle.Render(oneLine(each.value(), 60))), each.title))
	}
	options = append(options, huh.NewOption(keyStyle.Render(e.saveLabel), editorSave))
	if e.remove != nil {
		options = append(options, huh.NewOption(errorStyle.Render(e.removeLabel), editorRemove))
	}
	options = append(options, huh.NewOption("‹ back", editorBack))
	choice := last
	if choice == "" && len(entries) > 0 {
		choice = entries[0].title
	}
	description := "enter opens a field · " + e.saveLabel + " when it is filled in"
	if e.notice != "" {
		description, e.notice = e.notice, ""
	}
	list := huh.NewSelect[string]().Title(e.title).Description(description).Options(options...).Value(&choice)
	next := func(m model) (tea.Model, tea.Cmd) {
		switch choice {
		case editorSave:
			return e.save(m)
		case editorRemove:
			return m.confirmRemove(e)
		case editorBack:
			return e.back(m)
		}
		for _, each := range e.entries() {
			if each.title == choice {
				return m.editEntry(e, each)
			}
		}
		return m.showEditor(e, "")
	}
	form := huh.NewForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: e.back, help: menuHelp})
}

// editEntry opens one field; enter keeps what was typed, esc puts the old
// value back.
func (m model) editEntry(e *editor, field entry) (tea.Model, tea.Cmd) {
	restore := e.snapshot()
	input := field.field()
	toList := func(m model) (tea.Model, tea.Cmd) { return m.showEditor(e, field.title) }
	cancel := func(m model) (tea.Model, tea.Cmd) {
		restore()
		return toList(m)
	}
	form := huh.NewForm(huh.NewGroup(input).Title(e.title)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: toList, back: cancel, help: fieldHelp(input)})
}

func (m model) confirmRemove(e *editor) (tea.Model, tea.Cmd) {
	sure := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(e.removeLabel + "?").Affirmative("Yes").Negative("No").Value(&sure),
	).Title(e.title)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		if !sure {
			return m.showEditor(e, editorRemove)
		}
		return e.remove(m)
	}
	back := func(m model) (tea.Model, tea.Cmd) { return m.showEditor(e, editorRemove) }
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: menuHelp})
}

// saveEdit tries the change first and, when it works, keeps it in the menu
// or saves it to the user file; when it does not, the editor says why and
// keeps what was typed.
func (m model) saveEdit(e *editor, msg settingsLoadedMsg, menu *settingsMenu, what string, apply func(layer config.Config) (config.Config, error)) (tea.Model, tea.Cmd) {
	if _, err := apply(msg.layer); err != nil {
		e.notice = errorStyle.Render("not saved: " + err.Error())
		return m.showEditor(e, editorSave)
	}
	if menu != nil {
		return m.keepEdit(menu, what, apply)
	}
	return m.saveOrWait(&settingsScreen{layer: msg.layer, apply: apply})
}

// valueOr shows a value on one line, or what an empty one means.
func valueOr(value, empty string) string {
	if value = strings.TrimSpace(value); value == "" {
		return empty
	}
	return strings.Join(strings.Fields(value), " ")
}

// countOf says how many lines a box holds, naming the first ones.
func countOf(text, one, many string) string {
	rows := lines(text)
	switch len(rows) {
	case 0:
		return "none"
	case 1:
		return rows[0]
	}
	return fmt.Sprintf("%d %s: %s", len(rows), many, strings.Join(rows, " · "))
}
