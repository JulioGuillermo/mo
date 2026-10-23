package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
)

// Where /sidebar width keeps a width.
const (
	scopeSession = "session"
	scopeGlobal  = "global"
	scopeProject = "project"
)

var widthScopes = []string{scopeSession, scopeGlobal, scopeProject}

// sidebarWidthSaved brings how keeping a width in a settings file went.
type sidebarWidthSaved struct {
	width int
	where string
	err   error
}

// sidebarWidthCommand is /sidebar width: alone it says the width and where
// it comes from; with columns it sets it for the session, or keeps it in
// the user file (global) or the project's (project).
func (m model) sidebarWidthCommand(words []string) (tea.Model, tea.Cmd) {
	if len(words) == 0 {
		return m.emit(m.printer.Note(m.sidebarWidthNote()))
	}
	width, err := strconv.Atoi(words[0])
	if err != nil || width < config.MinSidebarWidth || width > config.MaxSidebarWidth {
		return m.emit(m.printer.Note(fmt.Sprintf("/sidebar width takes %d to %d columns, then session, global or project", config.MinSidebarWidth, config.MaxSidebarWidth)))
	}
	scope := scopeSession
	if len(words) > 1 {
		scope = words[1]
	}
	switch scope {
	case scopeSession:
		m.sidebarSessionWidth = width
		return m, tea.Batch(m.fitWidth(), printText(m.printer.Note(fmt.Sprintf("side panel %d columns · this session", width))))
	case scopeGlobal, scopeProject:
		m.sidebarSessionWidth = 0
		m.session.Config.Render.SidebarWidth = width
		workDir := m.session.WorkDir
		save := func() tea.Msg {
			return sidebarWidthSaved{width: width, where: scope, err: saveSidebarWidth(workDir, scope, width)}
		}
		return m, tea.Batch(m.fitWidth(), save)
	}
	return m.emit(m.printer.Note("/sidebar width " + words[0] + " takes session, global or project after it"))
}

func (m model) sidebarWidthSaved(msg sidebarWidthSaved) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(fmt.Errorf("keeping the side panel's width: %w", msg.err))
	}
	where := "every project"
	if msg.where == scopeProject {
		where = "this project, in " + config.ProjectFile(m.session.WorkDir)
	}
	return m.emit(m.printer.Note(fmt.Sprintf("side panel %d columns · kept for %s", msg.width, where)))
}

// sidebarWidthNote says the width and where it comes from.
func (m model) sidebarWidthNote() string {
	from := "the default"
	switch {
	case m.sidebarSessionWidth > 0:
		from = "this session"
	case m.session.Config.Render.SidebarWidth != config.DefaultSidebarWidth:
		from = "the settings"
	}
	return fmt.Sprintf("side panel %d columns, from %s · /sidebar width <%d-%d> [session|global|project]",
		m.sidebarWidth(), from, config.MinSidebarWidth, config.MaxSidebarWidth)
}

// saveSidebarWidth keeps a width in the user file or the project's, the
// default left out so the file says nothing of it.
func saveSidebarWidth(workDir, scope string, width int) error {
	if width == config.DefaultSidebarWidth {
		width = 0
	}
	if scope == scopeProject {
		project, err := config.ReadProject(workDir)
		if err != nil {
			return err
		}
		layer := project.Layer
		layer.Render.SidebarWidth = width
		return config.SaveProject(workDir, layer)
	}
	layer, err := config.ReadUserLayer()
	if err != nil {
		return err
	}
	layer.Render.SidebarWidth = width
	return config.SaveUserLayer(layer)
}

// sidebarWidthSuggestions are the widths /sidebar width offers, the one in
// use first, and once one is typed where to keep it.
func (m model) sidebarWidthSuggestions(partial string) []command {
	width, scope, spaced := strings.Cut(partial, " ")
	if spaced {
		descriptions := map[string]string{scopeSession: "until mo closes, the default", scopeGlobal: "in your settings, for every project", scopeProject: "in this project's .mo/settings.toml"}
		var matches []command
		for _, each := range widthScopes {
			if strings.HasPrefix(each, scope) {
				matches = append(matches, command{name: "sidebar width " + width + " " + each, description: descriptions[each]})
			}
		}
		return matches
	}
	var matches []command
	for _, each := range []int{m.sidebarWidth(), config.DefaultSidebarWidth, 28, 44, 56, 72} {
		name := strconv.Itoa(each)
		if !strings.HasPrefix(name, width) || slices.ContainsFunc(matches, func(c command) bool { return c.name == "sidebar width "+name+" " }) {
			continue
		}
		description := "columns"
		switch {
		case each == m.sidebarWidth():
			description = "now"
		case each == config.DefaultSidebarWidth:
			description = "the default"
		}
		matches = append(matches, command{name: "sidebar width " + name + " ", description: description, more: true})
	}
	return matches
}
