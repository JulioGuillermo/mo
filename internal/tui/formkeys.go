package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
)

// newForm is a huh form with mo's keys: shift+enter makes a new line in a
// box, as it does in the input, besides alt+enter and ctrl+j.
func newForm(groups ...*huh.Group) *huh.Form {
	keys := huh.NewDefaultKeyMap()
	keys.Text.NewLine = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line"))
	return huh.NewForm(groups...).WithKeyMap(keys)
}
