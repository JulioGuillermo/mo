package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | ED-01 | Happy    | A new MCP server picks its type first and shows only that type's fields | P0    |
// | ED-02 | Happy    | Filled in field by field and saved, the server waits in the menu      | P0       |
// | ED-03 | Error    | Saving without what is required says why and keeps what was typed    | P1       |
// | ED-04 | Happy    | Esc in a field puts its value back; delete asks, then removes         | P1       |
// | ED-05 | Happy    | A provider is filled in field by field the same way                  | P1       |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
)

func typeIn(t *testing.T, m model, text string) model {
	t.Helper()
	for _, r := range text {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(model)
	}
	return m
}

func enter(t *testing.T, m model) model {
	t.Helper()
	updated, cmd := m.Update(keyEnter)
	return deliver(updated.(model), cmd, 3)
}

func screen(m model) string {
	return ansi.Strip(m.settings.form.View())
}

func newServerEditor(t *testing.T, file config.Config) (model, *settingsMenu) {
	t.Helper()
	m, menu := menuModel()
	menu.layer, menu.loaded.layer = file, file
	updated, _ := m.editMCPServer(menu.loadedNow(), newServerChoice, menu)
	return updated.(model), menu
}

func TestEditor(t *testing.T) {
	t.Run("ED-01 a new MCP server picks its type first and shows only that type's fields", func(t *testing.T) {
		m, _ := newServerEditor(t, config.Config{})
		asCommand := screen(m)

		m = clickOn(t, m, "Type")
		m = keys(t, m, keyDown)
		m = enter(t, m)
		asURL := screen(m)

		if !strings.Contains(asCommand, "Command") || !strings.Contains(asCommand, "Environment") || strings.Contains(asCommand, "Headers") {
			t.Errorf("command fields:\n%s", asCommand)
		}
		if !strings.Contains(asURL, "URL") || !strings.Contains(asURL, "Headers") || strings.Contains(asURL, "Environment") {
			t.Errorf("URL fields:\n%s", asURL)
		}
	})

	t.Run("ED-02 filled in field by field and saved, the server waits in the menu", func(t *testing.T) {
		m, menu := newServerEditor(t, config.Config{})
		m = clickOn(t, m, "Name")
		m = enter(t, typeIn(t, m, "github"))
		m = clickOn(t, m, "Command   ")
		m = enter(t, typeIn(t, m, "npx"))
		m = clickOn(t, m, "   Save")

		server, saved := menu.layer.MCP["github"]
		if !saved || server.Command != "npx" || server.URL != "" || !strings.Contains(m.settings.form.View(), "MCP server") {
			t.Errorf("saved = %v server = %+v", saved, server)
		}
	})

	t.Run("ED-03 saving without what is required says why and keeps what was typed", func(t *testing.T) {
		m, menu := newServerEditor(t, config.Config{})
		m = clickOn(t, m, "Name")
		m = enter(t, typeIn(t, m, "half"))

		m = clickOn(t, m, "   Save")

		if _, saved := menu.layer.MCP["half"]; saved || !strings.Contains(screen(m), "not saved") || !strings.Contains(screen(m), "half") {
			t.Errorf("saved = %v screen:\n%s", saved, screen(m))
		}
	})

	t.Run("ED-04 esc in a field puts its value back; delete asks, then removes", func(t *testing.T) {
		file := config.Config{MCP: map[string]config.MCPServer{"docs": {Command: "uvx", Args: []string{"docs-mcp"}}}}
		m, menu := menuModel()
		menu.layer, menu.loaded.layer = file, file
		updated, _ := m.editMCPServer(menu.loadedNow(), "docs", menu)
		m = updated.(model)

		m = clickOn(t, m, "Command   ")
		m = typeIn(t, m, "-changed")
		m = keys(t, m, keyEsc)
		kept := strings.Contains(screen(m), "uvx") && !strings.Contains(screen(m), "uvx-changed")
		m = clickOn(t, m, "Delete this server")
		asked := strings.Contains(screen(m), "Delete this server?")
		m = clickOn(t, m, "Yes")

		if _, left := menu.layer.MCP["docs"]; !kept || !asked || left {
			t.Errorf("kept = %v asked = %v left = %v", kept, asked, left)
		}
	})

	t.Run("ED-05 a provider is filled in field by field the same way", func(t *testing.T) {
		m, menu := menuModel()
		updated, _ := m.editProvider(menu.loadedNow(), config.UserSettings(menu.layer), newProviderChoice, menu)
		m = updated.(model)

		m = clickOn(t, m, "Name")
		m = enter(t, typeIn(t, m, "gpu-box"))
		m = clickOn(t, m, "Base URL")
		m = enter(t, typeIn(t, m, "x"))
		m = clickOn(t, m, "   Save")

		if saved, ok := menu.layer.Providers["gpu-box"]; !ok || !strings.HasSuffix(saved.BaseURL, "x") {
			t.Errorf("providers = %+v", menu.layer.Providers)
		}
	})
}
