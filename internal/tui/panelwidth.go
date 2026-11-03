package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
)

// Where /sidebar width and /tree width keep a width.
const (
	scopeSession = "session"
	scopeGlobal  = "global"
	scopeProject = "project"
)

var widthScopes = []string{scopeSession, scopeGlobal, scopeProject}

// panelSize is what the width of a panel at a side takes: the command that
// sets it, its bounds, and where the session and the settings keep it.
type panelSize struct {
	command, name         string
	least, most, standard int
	offered               []int
	session               func(m *model) *int
	setting               func(r *config.Render) *int
}

var sidebarSize = panelSize{
	command: "sidebar", name: "side panel",
	least: config.MinSidebarWidth, most: config.MaxSidebarWidth, standard: config.DefaultSidebarWidth,
	offered: []int{28, 44, 56, 72},
	session: func(m *model) *int { return &m.sidebarSessionWidth },
	setting: func(r *config.Render) *int { return &r.SidebarWidth },
}

var treeSize = panelSize{
	command: "tree", name: "file tree",
	least: config.MinTreeWidth, most: config.MaxTreeWidth, standard: config.DefaultTreeWidth,
	offered: []int{22, 38, 48, 60},
	session: func(m *model) *int { return &m.treeSessionWidth },
	setting: func(r *config.Render) *int { return &r.TreeWidth },
}

// panelWidthSaved brings how keeping a width in a settings file went.
type panelWidthSaved struct {
	panel panelSize
	width int
	where string
	err   error
}

// panelWidth is a panel's columns: those its command set for the session,
// else the setting's, else the default.
func (m model) panelWidth(panel panelSize) int {
	if width := *panel.session(&m); width > 0 {
		return width
	}
	if width := *panel.setting(&m.session.Config.Render); width > 0 {
		return width
	}
	return panel.standard
}

// parseWidth reads a panel's columns, within its bounds.
func (panel panelSize) parseWidth(text string) (int, error) {
	width, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || width < panel.least || width > panel.most {
		return 0, fmt.Errorf("use %d to %d columns", panel.least, panel.most)
	}
	return width, nil
}

// widthCommand is /sidebar width or /tree width: alone it says the width
// and where it comes from; with columns it sets it for the session, or
// keeps it in the user file (global) or the project's (project).
func (m model) widthCommand(panel panelSize, words []string) (tea.Model, tea.Cmd) {
	if len(words) == 0 {
		return m.emit(m.printer.Note(m.widthNote(panel)))
	}
	width, err := panel.parseWidth(words[0])
	if err != nil {
		return m.emit(m.printer.Note(fmt.Sprintf("/%s width takes %d to %d columns, then session, global or project", panel.command, panel.least, panel.most)))
	}
	scope := scopeSession
	if len(words) > 1 {
		scope = words[1]
	}
	switch scope {
	case scopeSession:
		*panel.session(&m) = width
		return m, tea.Batch(m.fitWidth(), printText(m.printer.Note(fmt.Sprintf("%s %d columns · this session", panel.name, width))))
	case scopeGlobal, scopeProject:
		*panel.session(&m) = 0
		*panel.setting(&m.session.Config.Render) = width
		workDir := m.session.WorkDir
		save := func() tea.Msg {
			return panelWidthSaved{panel: panel, width: width, where: scope, err: saveWidth(panel, workDir, scope, width)}
		}
		return m, tea.Batch(m.fitWidth(), save)
	}
	return m.emit(m.printer.Note(fmt.Sprintf("/%s width %s takes session, global or project after it", panel.command, words[0])))
}

func (m model) panelWidthSaved(msg panelWidthSaved) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(fmt.Errorf("keeping the %s's width: %w", msg.panel.name, msg.err))
	}
	where := "every project"
	if msg.where == scopeProject {
		where = "this project, in " + config.ProjectFile(m.session.WorkDir)
	}
	return m.emit(m.printer.Note(fmt.Sprintf("%s %d columns · kept for %s", msg.panel.name, msg.width, where)))
}

// widthNote says a panel's width and where it comes from.
func (m model) widthNote(panel panelSize) string {
	from := "the default"
	switch {
	case *panel.session(&m) > 0:
		from = "this session"
	case *panel.setting(&m.session.Config.Render) > 0 && *panel.setting(&m.session.Config.Render) != panel.standard:
		from = "the settings"
	}
	return fmt.Sprintf("%s %d columns, from %s · /%s width <%d-%d> [session|global|project]",
		panel.name, m.panelWidth(panel), from, panel.command, panel.least, panel.most)
}

// saveWidth keeps a panel's width in the user file or the project's, the
// default left out so the file says nothing of it.
func saveWidth(panel panelSize, workDir, scope string, width int) error {
	if width == panel.standard {
		width = 0
	}
	if scope == scopeProject {
		project, err := config.ReadProject(workDir)
		if err != nil {
			return err
		}
		layer := project.Layer
		*panel.setting(&layer.Render) = width
		return config.SaveProject(workDir, layer)
	}
	layer, err := config.ReadUserLayer()
	if err != nil {
		return err
	}
	*panel.setting(&layer.Render) = width
	return config.SaveUserLayer(layer)
}

// widthSuggestions are the widths a panel's width command offers, the one
// in use first, and once one is typed where to keep it.
func (m model) widthSuggestions(panel panelSize, partial string) []command {
	prefix := panel.command + " width "
	width, scope, spaced := strings.Cut(partial, " ")
	if spaced {
		descriptions := map[string]string{scopeSession: "until mo closes, the default", scopeGlobal: "in your settings, for every project", scopeProject: "in this project's .mo/settings.toml"}
		var matches []command
		for _, each := range widthScopes {
			if strings.HasPrefix(each, scope) {
				matches = append(matches, command{name: prefix + width + " " + each, description: descriptions[each]})
			}
		}
		return matches
	}
	var matches []command
	for _, each := range append([]int{m.panelWidth(panel), panel.standard}, panel.offered...) {
		name := strconv.Itoa(each)
		if !strings.HasPrefix(name, width) || slices.ContainsFunc(matches, func(c command) bool { return c.name == prefix+name+" " }) {
			continue
		}
		description := "columns"
		switch {
		case each == m.panelWidth(panel):
			description = "now"
		case each == panel.standard:
			description = "the default"
		}
		matches = append(matches, command{name: prefix + name + " ", description: description, more: true})
	}
	return matches
}
