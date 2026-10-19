package tui

import (
	"context"
	"image/color"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/tools"
)

// pluginDialogMsg says a plugin opened a dialog, to show at once.
type pluginDialogMsg int

// pluginClickMsg brings what an item of a plugin said when clicked.
type pluginClickMsg struct {
	text string
	err  error
}

// clickPluginItem runs what a plugin's item in the side panel does, off the
// screen's goroutine.
func (m model) clickPluginItem(key string) (tea.Model, tea.Cmd) {
	loaded := m.session.Plugins
	return m, func() tea.Msg {
		text, err := loaded.ClickSidebar(context.Background(), key)
		return pluginClickMsg{text: text, err: err}
	}
}

func (m model) pluginClicked(msg pluginClickMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		return m.emitError(msg.err)
	case msg.text != "":
		return m.emit(m.printer.Note(msg.text))
	}
	return m, nil
}

// pluginDialogs are the dialogs the plugins opened, none without plugins.
func (m model) pluginDialogs() []tools.UI {
	dialogs := m.session.Plugins.Dialogs()
	if dialogs == nil {
		return nil
	}
	return dialogs.List()
}

// pluginColor is the colour of an item a plugin names, as the nodes of a
// dialog name theirs.
func pluginColor(name string, colors sidebarColors) color.Color {
	switch name {
	case "dim":
		return colors.dim
	case "accent":
		return colors.title
	case "success":
		return colors.done
	case "warning":
		return colors.active
	case "error":
		return colors.failed
	}
	return colors.text
}

// pluginRows are the plugins' items and dialogs in the side panel, starting
// at row first, with what a click on each does.
func (m model) pluginRows(items []plugins.SidebarItem, width, first int, base lipgloss.Style, colors sidebarColors) ([]string, []region) {
	var lines []string
	var regions []region
	for _, item := range items {
		icon := item.Icon
		if icon == "" {
			icon = "•"
		}
		key := item.Key
		click := func(m model) (tea.Model, tea.Cmd) { return m.clickPluginItem(key) }
		rows := 1
		lines = append(lines, base.Foreground(pluginColor(item.Color, colors)).Render(icon+" ")+base.Render(oneLine(item.Text, width-3)))
		if item.Detail != "" {
			lines = append(lines, base.Foreground(colors.dim).Render("  "+oneLine(item.Detail, width-3)))
			rows++
		}
		if !item.Clickable {
			continue
		}
		for y := range rows {
			regions = append(regions, region{y: first + len(lines) - rows + y, x0: 0, x1: 1 << 30, do: click, item: "plugin " + key})
		}
	}
	for _, dialog := range m.pluginDialogs() {
		id := dialog.ID
		open := func(m model) (tea.Model, tea.Cmd) { return m.openPluginDialog(id) }
		regions = append(regions, region{y: first + len(lines), x0: 0, x1: 1 << 30, do: open, item: "plugin dialog"})
		lines = append(lines, base.Foreground(colors.text).Render("▣ ")+base.Render(oneLine(dialog.Title, width-4-len(dialog.Owner)))+base.Foreground(colors.dim).Render("  "+dialog.Owner))
	}
	return lines, regions
}
