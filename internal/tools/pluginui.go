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
func (u *UIs) Open(owner, title string, tree UINode, options DialogOptions) (int, error) {
	if err := validateUI(tree); err != nil {
		return 0, err
	}
	if options.Size != "" && options.Size != DialogFull {
		return 0, fmt.Errorf("size %q: leave it out, or use full", options.Size)
	}
	id, err := u.create(orDefaultString(title, owner), "", tree)
	if err != nil {
		return 0, err
	}
	u.mu.Lock()
	opened := u.find(id)
	opened.Owner, opened.Size, opened.Listed, opened.Bare = owner, options.Size, options.Listed, options.Bare
	u.mu.Unlock()
	u.changed()
	return id, nil
}

// DialogFull is the size of a dialog filling the screen.
const DialogFull = "full"

// DialogOptions are how a plugin's dialog shows: its size, and whether it
// is listed in the side panel.
type DialogOptions struct {
	Size   string
	Listed bool
	// Bare drops the dialog's frame: no edges, title, × or keys line.
	Bare bool
}

// Dismiss closes a plugin's dialog for good, as esc does to one not
// listed, and tells the plugin with a close event.
func (u *UIs) Dismiss(id int) {
	u.mu.Lock()
	ui := u.find(id)
	if ui == nil {
		u.mu.Unlock()
		return
	}
	event := UIEvent{UI: id, Event: UIClose, Action: UIClose, Values: inputValuesOf(ui.Shown())}
	u.mu.Unlock()
	if u.deliver != nil {
		select {
		case u.deliver <- event:
		default:
		}
	}
	_ = u.Close(id)
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

// UIBounds is where a dialog is drawn, in cells of the screen.
type UIBounds struct {
	X, Y, Width, Height int
}

// SetShown says which interface is on screen and where; 0 for none.
func (u *UIs) SetShown(id int, bounds UIBounds) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.shown, u.shownBounds = id, bounds
}

// Bounds is where an interface is drawn, when it is on screen.
func (u *UIs) Bounds(id int) (UIBounds, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if id == 0 || u.shown != id {
		return UIBounds{}, false
	}
	return u.shownBounds, true
}
