package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PM-01 | Happy    | The permission mode picked is kept as the one to start in, written once switching stops | P0 |
// | PM-02 | Happy    | A mode the user set for this project is kept there; the default is written as nothing | P1 |

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
)

func modeModel(t *testing.T) model {
	t.Helper()
	m := sidebarModel(t, 140)
	m.session.WorkDir = t.TempDir()
	m.session.Agent.Permissions, _ = permissions.New(permissions.ModeAsk, nil, nil)
	// The settings are shared by the package's tests: start from none.
	layer, _ := config.ReadUserLayer()
	layer.Permissions = config.Permissions{}
	if err := config.SaveUserLayer(layer); err != nil {
		t.Fatal(err)
	}
	return m
}

// settle delivers a command's message and the ones it leads to, ticks
// included.
func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m
		}
		updated, next := m.Update(msg)
		m, cmd = updated.(model), next
	}
	return m
}

func TestPermissionModeKept(t *testing.T) {
	t.Run("PM-01 the permission mode picked is kept as the one to start in, written once switching stops", func(t *testing.T) {
		m := modeModel(t)
		var ticks []tea.Cmd
		for range 2 {
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			m = updated.(model)
			ticks = append(ticks, cmd)
		}
		mode := m.session.Agent.Permissions.Mode()
		m = settle(t, m, ticks[0])
		early, _ := config.ReadUserLayer()
		m = settle(t, m, ticks[1])
		layer, err := config.ReadUserLayer()

		if early.Permissions.Mode != "" {
			t.Errorf("a mode switched again should not be written: %q", early.Permissions.Mode)
		}
		if err != nil || layer.Permissions.Mode != string(mode) || mode == permissions.ModeAsk {
			t.Errorf("saved %q, in force %q (%v)", layer.Permissions.Mode, mode, err)
		}
	})

	t.Run("PM-02 a mode the user set for this project is kept there; the default is written as nothing", func(t *testing.T) {
		m := modeModel(t)
		key := config.ProjectKey(m.session.WorkDir)
		layer, _ := config.ReadUserLayer()
		layer.Permissions.Mode = "auto"
		layer.Permissions.Projects = map[string]config.ProjectRules{key: {Mode: "edits", Allow: []string{"ls *"}}}
		if err := config.SaveUserLayer(layer); err != nil {
			t.Fatal(err)
		}
		updated, cmd := m.setMode(permissions.ModeAll)
		m = settle(t, updated.(model), cmd)
		project, _ := config.ReadUserLayer()
		if err := saveStartMode(t.TempDir(), permissions.ModeEdits); err != nil {
			t.Fatal(err)
		}
		general, _ := config.ReadUserLayer()

		if rules := project.Permissions.Projects[key]; rules.Mode != "all" || len(rules.Allow) != 1 || project.Permissions.Mode != "auto" {
			t.Errorf("project rules = %+v, general = %q", rules, project.Permissions.Mode)
		}
		if general.Permissions.Mode != "" {
			t.Errorf("the default mode should be written as nothing: %q", general.Permissions.Mode)
		}
	})
}
