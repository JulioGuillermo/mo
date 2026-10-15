package tui

// Test Traceability Table
// | ID    | Category | Description                                                  | Priority |
// | FK-01 | Happy    | shift+enter, alt+enter and ctrl+j make a new line in a form's box; enter does not | P1 |

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func TestFormKeys(t *testing.T) {
	t.Run("FK-01 shift+enter, alt+enter and ctrl+j make a new line in a form's box; enter does not", func(t *testing.T) {
		for name, newLine := range map[string]tea.KeyPressMsg{
			"shift+enter": {Code: tea.KeyEnter, Mod: tea.ModShift},
			"alt+enter":   {Code: tea.KeyEnter, Mod: tea.ModAlt},
			"ctrl+j":      {Code: 'j', Mod: tea.ModCtrl},
		} {
			value := ""
			form := newForm(huh.NewGroup(huh.NewText().Title("Arguments").Value(&value)))
			form.Init()
			for _, key := range []tea.KeyPressMsg{{Code: 'a', Text: "a"}, newLine, {Code: 'b', Text: "b"}, {Code: tea.KeyEnter}} {
				updated, _ := form.Update(key)
				form = updated.(*huh.Form)
			}
			if value != "a\nb" {
				t.Errorf("%s: value = %q, want \"a\\nb\"", name, value)
			}
		}
	})
}
