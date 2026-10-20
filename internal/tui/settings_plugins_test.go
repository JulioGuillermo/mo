package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | SP-01 | Happy    | Settings › Plugins lists each plugin with its state; one with settings opens them | P0  |
// | SP-02 | Happy    | Turning a plugin off writes enabled = false; on again takes it out           | P0       |
// | SP-03 | Security | A secret shows hidden in the list and is typed hidden                        | P0       |
// | SP-04 | Edge     | A value equal to the default is not written; a number is saved as a number   | P1       |
// | SP-05 | Error    | A number setting refuses text                                                | P1       |
// | SP-06 | Edge     | Esc in a plugin's setting puts its old value back                            | P0       |
// | SP-07 | Happy    | /plugins says which plugins are off                                          | P2       |
// | SP-08 | Edge     | Esc from a plugin's list goes back to Settings › Plugins on its row          | P1       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/plugins"
)

const jiraPlugin = `
mo.setting{ name = "site", default = "example.atlassian.net", description = "Your Jira site." }
mo.setting{ name = "token", secret = true }
mo.setting{ name = "limit", type = "number", default = 20 }
mo.tool{ name = "jira_search", run = function() return "" end }`

// pluginMenu is the settings menu with the plugins of sources loaded, and
// the user file holding file.
func pluginMenu(t *testing.T, sources map[string]string, file config.Config) (model, *settingsMenu) {
	t.Helper()
	dir := t.TempDir()
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name+".lua"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded := plugins.Load(dir, t.TempDir(), t.TempDir(), file.Plugins)
	t.Cleanup(loaded.Close)
	m, _ := menuModel()
	m.session.Plugins = loaded
	file.Provider = "llama-server"
	menu := &settingsMenu{loaded: settingsLoadedMsg{layer: file}, layer: file, draft: newSettingsDraft(file)}
	return m, menu
}

func screenText(m model) string {
	return ansi.Strip(m.settings.form.View())
}

func pluginRow(m model, menu *settingsMenu, title string) settingItem {
	for _, item := range pluginsSection.items(m, menu) {
		if item.title == title {
			return item
		}
	}
	return settingItem{}
}

func savedPlugins(t *testing.T, menu *settingsMenu) map[string]map[string]any {
	t.Helper()
	layer, err := menu.apply(menu.layer)
	if err != nil {
		t.Fatal(err)
	}
	return layer.Plugins
}

func TestPluginSettings(t *testing.T) {
	t.Run("SP-01 Settings › Plugins lists each plugin with its state; one with settings opens them", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"jira": jiraPlugin, "plain": `mo.command{ name = "hi", run = function() end }`, "quiet": ``},
			config.Config{Plugins: map[string]map[string]any{"quiet": {"enabled": false}}})

		group, _ := m.openMenuChoice(menu, groupPrefix+pluginsKey)
		list := screenText(group.(model))
		own, _ := group.(model).showSection(menu, pluginSection(m.session.Plugins.Status()[0]), "")
		settings := screenText(own.(model))

		for _, want := range []string{"Settings › Plugins", "jira ›", "on · 1 tool · 3 settings", "plain", "on · 1 command", "quiet", "off"} {
			if !strings.Contains(list, want) {
				t.Errorf("list misses %q:\n%s", want, list)
			}
		}
		for _, want := range []string{"Plugins › jira", "On", "site", "example.atlassian.net (default)", "token", "not set", "limit", "20 (default)", "jira_search"} {
			if !strings.Contains(settings, want) {
				t.Errorf("jira misses %q:\n%s", want, settings)
			}
		}
	})

	t.Run("SP-02 turning a plugin off writes enabled = false; on again takes it out", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"plain": ``}, config.Config{})
		switchOf := func() *pluginOn { return &pluginOn{draft: menu.draft, plugin: "plain"} }

		switchOf().Set(false)
		off := savedPlugins(t, menu)
		switchOf().Set(true)
		on := savedPlugins(t, menu)

		if off["plain"]["enabled"] != false || on != nil || pluginRow(m, menu, "plain").value(menu.draft) != "on" {
			t.Errorf("off = %v on = %v", off, on)
		}
	})

	t.Run("SP-03 a secret shows hidden in the list and is typed hidden", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"jira": jiraPlugin},
			config.Config{Plugins: map[string]map[string]any{"jira": {"token": "s3cret-value"}}})
		section := pluginSection(m.session.Plugins.Status()[0])

		listed, _ := m.showSection(menu, section, "")
		opened, _ := m.editItem(menu, section, section.items(m, menu)[2])

		if list := screenText(listed.(model)); strings.Contains(list, "s3cret") || !strings.Contains(list, "••••••") {
			t.Errorf("list:\n%s", list)
		}
		if field := screenText(opened.(model)); strings.Contains(field, "s3cret") {
			t.Errorf("field:\n%s", field)
		}
	})

	t.Run("SP-04 a value equal to the default is not written; a number is saved as a number", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"jira": jiraPlugin}, config.Config{})
		setting := func(name string) *pluginValue {
			for _, each := range m.session.Plugins.Status()[0].Settings {
				if each.Name == name {
					return &pluginValue{draft: menu.draft, plugin: "jira", setting: each}
				}
			}
			t.Fatalf("no setting %s", name)
			return nil
		}

		setting("site").Set("example.atlassian.net")
		setting("limit").Set(" 50 ")

		if saved := savedPlugins(t, menu); len(saved["jira"]) != 1 || saved["jira"]["limit"] != int64(50) {
			t.Errorf("saved = %#v", saved)
		}
	})

	t.Run("SP-05 a number setting refuses text", func(t *testing.T) {
		if validSettingNumber("ten") == nil || validSettingNumber("1.5") != nil || validSettingNumber("") != nil {
			t.Error("validSettingNumber takes text or refuses numbers")
		}
	})

	t.Run("SP-06 esc in a plugin's setting puts its old value back", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"jira": jiraPlugin}, config.Config{})
		section := pluginSection(m.session.Plugins.Status()[0])
		site := section.items(m, menu)[1]
		opened, _ := m.editItem(menu, section, site)
		menu.draft.setPluginValue("jira", "site", "other.atlassian.net")

		opened.(model).Update(keyEsc)

		if saved := savedPlugins(t, menu); saved != nil {
			t.Errorf("saved = %v", saved)
		}
	})

	t.Run("SP-07 /plugins says which plugins are off", func(t *testing.T) {
		m, _ := pluginMenu(t, map[string]string{"quiet": ``}, config.Config{Plugins: map[string]map[string]any{"quiet": {"enabled": false}}})

		if report := pluginReport(m.session.Plugins); !strings.Contains(report, "○ quiet: off") {
			t.Errorf("report = %q", report)
		}
	})

	t.Run("SP-08 esc from a plugin's list goes back to Settings › Plugins on its row", func(t *testing.T) {
		m, menu := pluginMenu(t, map[string]string{"jira": jiraPlugin}, config.Config{})
		group, _ := m.openMenuChoice(menu, groupPrefix+pluginsKey)

		own, _ := group.(model).settings.next(group.(model))
		back, _ := own.(model).Update(keyEsc)

		if !strings.Contains(screenText(own.(model)), "Plugins › jira") || !strings.Contains(screenText(back.(model)), "Settings › Plugins") {
			t.Errorf("opened:\n%s\nback:\n%s", screenText(own.(model)), screenText(back.(model)))
		}
	})
}
