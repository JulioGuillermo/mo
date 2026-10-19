package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	firstPluginUI     = 1001
	pluginUIs         = "plugins"
	pluginEventBuffer = 256
)

// NewPluginUIs keeps the interfaces plugins open: while mo runs, never
// saved, numbered from firstPluginUI so they do not mix with the model's.
// What the user does in them goes to deliver, one event after another, and
// never to the model.
func NewPluginUIs(deliver func(UIEvent)) *UIs {
	u := &UIs{conversation: pluginUIs, firstID: firstPluginUI, deliver: make(chan UIEvent, pluginEventBuffer)}
	go func() {
		for event := range u.deliver {
			deliver(event)
		}
	}()
	return u
}

// toPlugin hands an event to the plugin of the interface, with every
// value; close and submit hide it and remove takes it away. The lock is held.
func (u *UIs) toPlugin(ui *UI, event UIEvent, handler string) UIOutcome {
	event.Action = handler
	event.Values = inputValuesOf(ui.Shown())
	select {
	case u.deliver <- event:
	default:
	}
	switch handler {
	case UIClose:
		return UIOutcome{Hidden: true}
	case UISubmit:
		return UIOutcome{Submitted: true}
	case UIRemove:
		return UIOutcome{Removed: true}
	}
	return UIOutcome{}
}

// Open shows a plugin's interface and gives its number.
func (u *UIs) Open(owner, title string, tree UINode) (int, error) {
	if err := validateUI(tree); err != nil {
		return 0, err
	}
	id, err := u.create(orDefaultString(title, owner), "", tree)
	if err != nil {
		return 0, err
	}
	u.mu.Lock()
	u.find(id).Owner = owner
	u.mu.Unlock()
	u.changed()
	return id, nil
}

// Replace changes a plugin's interface: its title when given, its tree when
// given; what the user typed stays.
func (u *UIs) Replace(id int, title string, tree *UINode) error {
	if tree != nil {
		if err := validateUI(*tree); err != nil {
			return err
		}
	}
	if err := u.update(id, title, tree, nil); err != nil {
		return err
	}
	u.changed()
	return nil
}

// Close takes a plugin's interface away.
func (u *UIs) Close(id int) error {
	if err := u.Remove(id); err != nil {
		return err
	}
	u.changed()
	return nil
}

// ShowError shows under an interface what went wrong in the code behind it,
// or clears it with "".
func (u *UIs) ShowError(id int, text string) {
	u.mu.Lock()
	found := u.find(id)
	if found != nil {
		found.Error = strings.TrimSpace(text)
	}
	u.mu.Unlock()
	if found != nil {
		u.changed()
	}
}

// PluginTree reads a tree a plugin gave as plain values, as Lua tables turn
// out, mended as a model's is.
func PluginTree(value any) (*UINode, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}
	tree, err := ParseUITree(encoded)
	if err == nil && tree == nil {
		return nil, fmt.Errorf("a dialog needs a tree, e.g. %s", uiTreeExample)
	}
	return tree, err
}
