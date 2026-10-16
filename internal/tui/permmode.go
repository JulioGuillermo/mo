package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
)

// modeSaveDelay is how long the permission mode must stay put before it is
// kept, so cycling through the modes writes the settings once.
const modeSaveDelay = 800 * time.Millisecond

type (
	modeSaveMsg  int
	modeSavedMsg struct{ err error }
)

// setMode switches the permission mode, and keeps it as the one to start
// in once the switching stops.
func (m model) setMode(mode permissions.Mode) (tea.Model, tea.Cmd) {
	policy := m.session.Agent.Permissions
	if policy == nil {
		return m, nil
	}
	policy.SetMode(mode)
	m.modeSaves++
	number := m.modeSaves
	return m, tea.Tick(modeSaveDelay, func(time.Time) tea.Msg { return modeSaveMsg(number) })
}

// saveMode writes the mode in force, unless it was switched again since.
func (m model) saveMode(number modeSaveMsg) (tea.Model, tea.Cmd) {
	policy := m.session.Agent.Permissions
	if int(number) != m.modeSaves || policy == nil {
		return m, nil
	}
	workDir, mode := m.session.WorkDir, policy.Mode()
	return m, func() tea.Msg { return modeSavedMsg{err: saveStartMode(workDir, mode)} }
}

// saveStartMode keeps mode as the one to start in, where the user's
// settings say it now: the rules set for this project when they set one,
// else for every project. Project files cannot set it.
func saveStartMode(workDir string, mode permissions.Mode) error {
	layer, err := config.ReadUserLayer()
	if err != nil {
		return err
	}
	key := config.ProjectKey(workDir)
	if rules, ok := layer.Permissions.Projects[key]; ok && rules.Mode != "" {
		rules.Mode = string(mode)
		layer.Permissions.Projects[key] = rules
		return config.SaveUserLayer(layer)
	}
	layer.Permissions.Mode = unlessDefault(string(mode), config.Defaults().Permissions.Mode)
	return config.SaveUserLayer(layer)
}
