package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SM-01 | Happy    | Esc in a section goes back to the menu and keeps what was typed      | P0       |
// | SM-02 | Happy    | A provider edited from the menu waits for save and becomes the model's provider | P0 |
// | SM-03 | Edge     | Leaving with changes asks first; leaving without them just closes    | P0       |
// | SM-04 | Error    | Saving a bad value keeps the menu open and names the field           | P1       |
// | SM-05 | Happy    | Esc in a provider form goes back to the list, then to the menu       | P1       |
// | SM-06 | Happy    | A section lists its settings with their values; one opens on its own | P0       |
// | SM-07 | Edge     | Esc in a setting puts its old value back; enter keeps the new one    | P0       |
// | SM-09 | Happy    | An MCP server is one row of the tool modes; its list has its mode, and its tools unless it is a group | P0 |
// | SM-10 | Happy    | A choice opens in a box over the list it came from; a click outside it cancels | P1 |
// | SM-11 | Edge     | Every section with items of its own is in a group of the menu, Subagents too | P0  |
// | SM-08 | Happy    | The menu opens groups; a group lists its sections; esc goes back a level at a time | P0 |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
)

func menuModel() (model, *settingsMenu) {
	session := Session{Agent: &agent.Agent{}, Provider: "llama-server", Config: config.Defaults()}
	m := model{session: session, printer: newPrinter(), input: newInput(), width: 100, height: 40}
	file := config.Config{Provider: "llama-server"}
	menu := &settingsMenu{loaded: settingsLoadedMsg{layer: file}, layer: file, draft: newSettingsDraft(file)}
	updated, _ := m.showMenu(menu)
	return updated.(model), menu
}

func TestSettingsMenu(t *testing.T) {
	t.Run("SM-01 esc in a section goes back to the menu and keeps what was changed", func(t *testing.T) {
		m, menu := menuModel()
		updated, _ := m.openMenuChoice(menu, "loop")
		menu.draft.loopEnabled = false

		updated, _ = updated.(model).Update(keyEsc)

		after := updated.(model)
		if after.settings == nil || !strings.HasPrefix(after.settings.help, menuHelp) || !menu.changed() {
			t.Errorf("menu shown = %v changed = %v", after.settings != nil, menu.changed())
		}
	})

	t.Run("SM-02 a provider edited from the menu waits for save and becomes the model's provider", func(t *testing.T) {
		m, menu := menuModel()
		draft := newProviderDraft("ollama", config.UserSettings(menu.layer), nil)
		draft.use = true

		updated, cmd := m.keepEdit(menu, "provider ollama", draft.applyTo)

		layer, _ := menu.apply(menu.layer)
		if updated.(model).settingsInFlight || cmd == nil || menu.draft.provider != "ollama" || layer.Provider != "ollama" || menu.loaded.layer.Provider != "llama-server" {
			t.Errorf("draft provider = %q layer = %+v", menu.draft.provider, layer)
		}
	})

	t.Run("SM-03 leaving with changes asks first; leaving without them just closes", func(t *testing.T) {
		m, menu := menuModel()
		untouched, _ := m.Update(keyEsc)
		menu.draft.search = "brave"
		reshown, _ := m.showMenu(menu)
		asked, _ := reshown.(model).Update(keyEsc)

		if untouched.(model).settings != nil {
			t.Error("an untouched menu asked before closing")
		}
		if asked.(model).settings == nil || !strings.Contains(asked.(model).settings.form.View(), "Close without saving?") {
			t.Error("closed without asking")
		}
	})

	t.Run("SM-04 saving a bad value keeps the menu open and names the field", func(t *testing.T) {
		m, menu := menuModel()
		menu.draft.maxOutput = "lots"

		updated, _ := m.openMenuChoice(menu, menuSave)

		after := updated.(model)
		if after.settings == nil || after.settingsInFlight || !strings.Contains(after.settings.form.View(), "not saved") {
			t.Errorf("menu shown = %v in flight = %v", after.settings != nil, after.settingsInFlight)
		}
	})

	t.Run("SM-05 esc in a provider form goes back to the list, then to the menu", func(t *testing.T) {
		m, menu := menuModel()
		list, _ := m.openMenuChoice(menu, menuProviders)
		form, _ := list.(model).settings.next(list.(model))

		backToList, _ := form.(model).Update(keyEsc)
		backToMenu, _ := backToList.(model).Update(keyEsc)

		if !strings.Contains(backToList.(model).settings.form.View(), "Provider to edit") || !strings.Contains(backToMenu.(model).settings.form.View(), "Settings") {
			t.Error("esc did not walk back through the menus")
		}
	})
	t.Run("SM-06 a section lists its settings with their values; one opens on its own", func(t *testing.T) {
		m, menu := menuModel()
		section, _ := m.openMenuChoice(menu, "loop")
		list := section.(model).settings.form.View()

		opened, _ := section.(model).settings.next(section.(model))

		view := opened.(model).settings.form.View()
		if !strings.Contains(list, "Loop: same text in a row") || !strings.Contains(list, "‹ back") || strings.Contains(view, "Loop: same text in a row") || !strings.Contains(view, "Watch for loops") {
			t.Errorf("list:\n%s\nopened:\n%s", list, view)
		}
	})

	t.Run("SM-07 esc in a setting puts its old value back; enter keeps the new one", func(t *testing.T) {
		m, menu := menuModel()
		section := settingsSections[4]
		items := section.items(m, menu)
		opened, _ := m.editItem(menu, section, items[1])
		menu.draft.loopRepeats = "9"
		cancelled, _ := opened.(model).Update(keyEsc)
		afterEsc := menu.draft.loopRepeats
		opened, _ = m.editItem(menu, section, items[1])
		menu.draft.loopRepeats = "7"
		opened.(model).settings.next(opened.(model))

		if afterEsc == "9" || menu.draft.loopRepeats != "7" || !strings.Contains(cancelled.(model).settings.form.View(), "Settings › Safety") {
			t.Errorf("after esc = %q after enter = %q", afterEsc, menu.draft.loopRepeats)
		}
	})

	t.Run("SM-08 the menu opens groups; a group lists its settings and its long lists; esc goes back a level at a time", func(t *testing.T) {
		m, menu := menuModel()
		top := ansi.Strip(m.settings.form.View())

		group, _ := m.openMenuChoice(menu, groupPrefix+"tools")
		listed := ansi.Strip(group.(model).settings.form.View())
		section, _ := group.(model).openMenuChoice(menu, "descriptions")
		toGroup, _ := section.(model).Update(keyEsc)
		toMenu, _ := toGroup.(model).Update(keyEsc)

		for _, want := range []string{"Model ›", "Conversation ›", "Tools ›", "Skills ›", "Safety ›", "Interface ›"} {
			if !strings.Contains(top, want) {
				t.Errorf("menu misses %q:\n%s", want, top)
			}
		}
		for _, want := range []string{"Settings › Tools", "Tool mode", "Max tool output", "Web search", "Mode of each tool ›", "Descriptions ›", "MCP servers ›", "‹ back"} {
			if !strings.Contains(listed, want) {
				t.Errorf("group misses %q:\n%s", want, listed)
			}
		}
		if !strings.Contains(ansi.Strip(toGroup.(model).settings.form.View()), "Settings › Tools") || !strings.Contains(ansi.Strip(toMenu.(model).settings.form.View()), "Conversation ›") {
			t.Error("esc should go back to the group, then to the menu")
		}
	})

	t.Run("SM-09 an MCP server is one row of the tool modes; its list has its mode, and its tools unless it is a group", func(t *testing.T) {
		m, menu := menuModel()
		m.session.ToolNames = []string{"read", "bash", "mcp__playwright__browser_click", "mcp__playwright__browser_type"}
		menu.layer.MCP = map[string]config.MCPServer{"playwright": {Command: "npx"}}

		modes, _ := m.openMenuChoice(menu, "toolmodes")
		list := ansi.Strip(modes.(model).settings.form.View())
		server, _ := m.showSection(menu, serverModeSection("playwright"), "")
		tools := ansi.Strip(server.(model).settings.form.View())
		*menu.draft.modeOf(mcpModes, "playwright") = "group"
		grouped, _ := m.showSection(menu, serverModeSection("playwright"), "")
		alone := ansi.Strip(grouped.(model).settings.form.View())
		back, _ := grouped.(model).Update(keyEsc)

		if !strings.Contains(list, "playwright (MCP server) ›") || strings.Contains(list, "browser_click") || !strings.Contains(list, "bash") {
			t.Errorf("tool modes:\n%s", list)
		}
		if !strings.Contains(tools, "browser_click") || !strings.Contains(tools, "browser_type") || !strings.Contains(tools, "Mode of each tool › playwright") {
			t.Errorf("server not grouped:\n%s", tools)
		}
		if strings.Contains(alone, "browser_click") || !strings.Contains(alone, "group") {
			t.Errorf("server grouped:\n%s", alone)
		}
		if !strings.Contains(ansi.Strip(back.(model).settings.form.View()), "playwright (MCP server) ›") {
			t.Error("esc should go back to the tool modes")
		}
		opened, _ := grouped.(model).settings.next(grouped.(model))
		if view := ansi.Strip(opened.(model).settings.form.View()); !strings.Contains(view, "group: one entry for the server") {
			t.Errorf("the server's mode should open to be changed:\n%s", view)
		}
	})

	t.Run("SM-10 a choice opens in a box over the list it came from; a click outside it cancels", func(t *testing.T) {
		m, menu := menuModel()
		m.width, m.height = 110, 30
		before := menu.draft.keys
		group, _ := m.openMenuChoice(menu, groupPrefix+"interface")
		updated, cmd := group.(model).Update(keyEnter)
		opened := deliver(updated.(model), cmd, 3)

		screen := ansi.Strip(opened.View().Content)
		if opened.settings == nil || !opened.settings.floating || !strings.Contains(screen, "vim: insert") || !strings.Contains(screen, "Status line") {
			t.Fatalf("screen:\n%s", screen)
		}
		closed, _ := opened.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))
		if after := closed.(model); after.settings == nil || after.settings.floating || menu.draft.keys != before {
			t.Errorf("a click outside should cancel back to the list: keys = %q", menu.draft.keys)
		}
	})
}

func TestSettingsGroups(t *testing.T) {
	t.Run("SM-11 every section with items of its own is in a group of the menu, Subagents too", func(t *testing.T) {
		for _, section := range settingsSections {
			if section.parent != "" {
				continue
			}
			if _, ok := groupOf(section); !ok {
				t.Errorf("section %q is in no group, so the menu never shows it", section.key)
			}
		}
		m, menu := menuModel()
		updated, _ := m.openMenuChoice(menu, groupPrefix+"agents")
		if view := ansi.Strip(updated.(model).settings.form.View()); !strings.Contains(view, "Subagents") || !strings.Contains(view, "Levels") {
			t.Errorf("agents group:\n%s", view)
		}
	})
}
