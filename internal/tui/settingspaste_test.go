package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | SP-01 | Edge     | A paste into a settings field goes in once, from ctrl+v and the terminal both | P1     |

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func TestSettingsPaste(t *testing.T) {
	t.Run("SP-01 a paste into a settings field goes in once, from ctrl+v and the terminal both", func(t *testing.T) {
		for name, field := range map[string]func(*string) huh.Field{
			"input": func(v *string) huh.Field { return huh.NewInput().Title("Key").Value(v) },
			"box":   func(v *string) huh.Field { return huh.NewText().Title("Rules").Value(v) },
		} {
			value := ""
			m := sidebarModel(t, 120)
			m.settings = &settingsScreen{form: newForm(huh.NewGroup(field(&value)))}
			m.settings.form.Init()
			send := func(msg tea.Msg) tea.Cmd {
				updated, cmd := m.Update(msg)
				m = updated.(model)
				return cmd
			}

			read := send(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
			send(settingsPasteMsg("sk-123"))
			send(tea.PasteMsg{Content: "sk-123"})
			once := value
			send(tea.PasteMsg{Content: "-more"})

			if read == nil {
				t.Errorf("%s: ctrl+v should read the clipboard", name)
			}
			if once != "sk-123" || value != "sk-123-more" {
				t.Errorf("%s: pasted %q then %q", name, once, value)
			}
		}
	})
}
