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

import (
	"strings"
	"testing"

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
		if !strings.Contains(list, "Same text in a row") || !strings.Contains(list, "‹ back") || strings.Contains(view, "Same text in a row") || !strings.Contains(view, "Watch for loops") {
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

		if afterEsc == "9" || menu.draft.loopRepeats != "7" || !strings.Contains(cancelled.(model).settings.form.View(), "Settings › Loop guard") {
			t.Errorf("after esc = %q after enter = %q", afterEsc, menu.draft.loopRepeats)
		}
	})
}
