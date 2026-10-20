package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | PK-01 | Happy    | A key a plugin takes in its own dialog never reaches the dialog; one it leaves does | P0 |
// | PK-02 | Happy    | A plugin allowed global events takes keys from the input; one not allowed hears nothing | P0 |
// | PK-03 | Security | mo's settings, menus and questions are never heard, nor esc and ctrl+c     | P0       |
// | PK-04 | Happy    | The status line ends in a yellow dot when a plugin asks and is not allowed, a red one when one hears, none otherwise | P0 |
// | PK-05 | Happy    | A click on the dot opens Settings › Plugins                                | P1       |
// | PK-06 | Security | Global events shows in a plugin's settings only when it asks, saying what it asks for; on writes global_events = true | P0 |
// | PK-07 | Happy    | /plugins says what a plugin asks to hear anywhere and whether it is off     | P2       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/plugins"
)

// settingsPluginModel is a conversation with the plugins of sources loaded
// with settings.
func settingsPluginModel(t *testing.T, sources map[string]string, settings map[string]map[string]any) model {
	t.Helper()
	dir := t.TempDir()
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name+".lua"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded := plugins.Load(dir, t.TempDir(), t.TempDir(), settings)
	t.Cleanup(loaded.Close)
	m := conversationModel()
	m.session.Plugins = loaded
	m.width, m.height = 160, 40
	return m
}

func dotIn(color string) string {
	return lipgloss.NewStyle().Foreground(uiColor(color)).Render("●")
}

const globalTaker = `mo.global.on("key", "ctrl+g", function(e) mo.sidebar.set{ id = "got", text = "got " .. e.key } return true end)`

func TestPluginKeys(t *testing.T) {
	t.Run("PK-01 a key a plugin takes in its own dialog never reaches the dialog; one it leaves does", func(t *testing.T) {
		m := openedDialog(t, `
mo.on("key", "x", function() return true end)
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "input", id = "name" } }
end }`)

		m = pressed(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"}, tea.KeyPressMsg{Code: 'y', Text: "y"})

		ui, _ := m.session.Plugins.Dialogs().Get(m.uiOpen)
		if ui.Values["name"] != "y" {
			t.Errorf("typed %q", ui.Values["name"])
		}
	})

	t.Run("PK-02 a plugin allowed global events takes keys from the input; one not allowed hears nothing", func(t *testing.T) {
		allowed := settingsPluginModel(t, map[string]string{"grab": globalTaker}, map[string]map[string]any{"grab": {plugins.GlobalEventsKey: true}})
		asked := settingsPluginModel(t, map[string]string{"grab": globalTaker}, nil)
		ctrlG := tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}

		allowed = pressed(t, allowed, ctrlG, tea.KeyPressMsg{Code: 'a', Text: "a"})
		asked = pressed(t, asked, ctrlG)

		if items := allowed.session.Plugins.SidebarItems(); len(items) != 1 || items[0].Text != "got ctrl+g" || allowed.input.Value() != "a" {
			t.Errorf("allowed: items %+v input %q", items, allowed.input.Value())
		}
		if len(asked.session.Plugins.SidebarItems()) != 0 {
			t.Error("a plugin not allowed heard the key")
		}
	})

	t.Run("PK-03 mo's settings, menus and questions are never heard, nor esc and ctrl+c", func(t *testing.T) {
		all := `mo.global.on("key", function(e) mo.sidebar.set{ id = e.key, text = e.key } return false end)`
		m := settingsPluginModel(t, map[string]string{"spy": all}, map[string]map[string]any{"spy": {plugins.GlobalEventsKey: true}})
		inSettings, _ := menuModel()
		inSettings.session.Plugins = m.session.Plugins

		pressed(t, inSettings, tea.KeyPressMsg{Code: 's', Text: "s"})
		m.pluginsTookKey(keyEsc)
		m.pluginsTookKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		m.pluginsTookKey(tea.KeyPressMsg{Code: 'k', Text: "k"})

		items := m.session.Plugins.SidebarItems()
		if len(items) != 1 || items[0].Text != "k" {
			t.Errorf("heard %+v", items)
		}
	})

	t.Run("PK-04 the status line ends in a yellow dot when a plugin asks and is not allowed, a red one when one hears, none otherwise", func(t *testing.T) {
		asked := settingsPluginModel(t, map[string]string{"grab": globalTaker}, nil)
		heard := settingsPluginModel(t, map[string]string{"grab": globalTaker, "other": globalTaker},
			map[string]map[string]any{"grab": {plugins.GlobalEventsKey: true}})
		quiet := settingsPluginModel(t, map[string]string{"plain": `mo.on("key", function() end)`}, nil)

		yellow, _ := asked.globalEventsDot()
		red, _ := heard.globalEventsDot()
		_, shown := quiet.globalEventsDot()

		if !strings.Contains(yellow, "●") || yellow != dotIn("warning") || red != dotIn("error") || shown {
			t.Errorf("yellow %q red %q quiet shown %v", yellow, red, shown)
		}
		if line, _ := asked.statusLayout(); !strings.HasSuffix(strings.TrimSpace(ansi.Strip(line)), "●") {
			t.Errorf("status line: %s", ansi.Strip(line))
		}
	})

	t.Run("PK-05 a click on the dot opens Settings › Plugins", func(t *testing.T) {
		m := settingsPluginModel(t, map[string]string{"grab": globalTaker}, nil)
		_, regions := m.statusLayout()

		var open func(model) (tea.Model, tea.Cmd)
		for _, each := range regions {
			if each.item == "global events" {
				open = each.do
			}
		}
		if open == nil {
			t.Fatal("the dot is not clickable")
		}
		updated, cmd := open(m)

		if updated.(model).openSection != groupPrefix+pluginsKey || !updated.(model).settingsInFlight || cmd == nil {
			t.Errorf("section %q", updated.(model).openSection)
		}
	})

	t.Run("PK-06 Global events shows in a plugin's settings only when it asks, saying what it asks for; on writes global_events = true", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"grab": globalTaker, "plain": `mo.setting{ name = "x" }`}, config.Config{})
		statuses := m.session.Plugins.Status()

		grab, _ := m.showSection(menu, pluginSection(statuses[0]), "")
		plain, _ := m.showSection(menu, pluginSection(statuses[1]), "")
		(&pluginGlobal{draft: menu.draft, plugin: "grab"}).Set(true)

		if text := screenText(grab.(model)); !strings.Contains(text, "Global events") || !strings.Contains(text, "off · asks for ctrl+g") {
			t.Errorf("grab:\n%s", text)
		}
		if strings.Contains(screenText(plain.(model)), "Global events") {
			t.Error("a plugin that asks nothing has the switch")
		}
		if saved := savedPlugins(t, menu); saved["grab"][plugins.GlobalEventsKey] != true {
			t.Errorf("saved = %v", saved)
		}
	})

	t.Run("PK-07 /plugins says what a plugin asks to hear anywhere and whether it is off", func(t *testing.T) {
		m := settingsPluginModel(t, map[string]string{"grab": globalTaker}, nil)

		report := pluginReport(m.session.Plugins)

		if !strings.Contains(report, "! asks to hear keys anywhere in mo (ctrl+g): off · /settings › Plugins › grab") {
			t.Errorf("report:\n%s", report)
		}
	})
}
