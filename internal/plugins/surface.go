package plugins

import (
	"context"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/tools"
)

// surface is what the plugins show the user: items in the side panel and
// dialogs, shared by every plugin of a Set.
type surface struct {
	mu       sync.Mutex
	items    []*sidebarEntry
	handlers map[int]dialogHandler
	dialogs  *tools.UIs
	changed  func()
	open     func(id int)
	send     func(SentMessage)
	view     func(path string)
	// screen is the terminal's width and height, zero until told.
	screen [2]int
	// canvases are those of the dialogs open, by dialog and id; fps is
	// the most times a second they are drawn again by themselves, ticking
	// whether some are.
	canvases map[string]*canvas
	fps      int
	ticking  bool
}

type sidebarEntry struct {
	plugin  *Plugin
	item    SidebarItem
	onClick *lua.LFunction
}

type dialogHandler struct {
	plugin  *Plugin
	onEvent *lua.LFunction
}

// SidebarItem is a line a plugin keeps in the side panel.
type SidebarItem struct {
	// Key names the item among every plugin's: the plugin, then its id.
	Key, Plugin, Text, Detail, Color, Icon string
	Clickable                              bool
}

func newSurface() *surface {
	s := &surface{handlers: map[int]dialogHandler{}, canvases: map[string]*canvas{}, fps: defaultUIFPS}
	s.dialogs = tools.NewPluginUIs(s.deliver)
	s.dialogs.OnChange(s.tell)
	s.dialogs.SetCanvasSource(s.canvasLines)
	return s
}

func (s *surface) tell() {
	s.mu.Lock()
	changed := s.changed
	s.mu.Unlock()
	if changed != nil {
		changed()
	}
}

// deliver gives a dialog's event to the plugin that opened it; what fails
// shows under the dialog and goes to the plugin's log.
func (s *surface) deliver(event tools.UIEvent) {
	s.mu.Lock()
	handler, ok := s.handlers[event.UI]
	s.mu.Unlock()
	defer s.forgetClosed(event.UI)
	if canvasEvents[event.Event] {
		defer s.redrawCanvas(event.UI, event.Element)
	}
	if !ok || handler.onEvent == nil {
		return
	}
	_, err := handler.plugin.callFunction(context.Background(), runTimeout, handler.onEvent, optionalText, func(state *lua.LState) lua.LValue {
		return eventTable(state, event)
	})
	if err != nil {
		handler.plugin.log("dialog event:", err.Error())
		s.dialogs.ShowError(event.UI, err.Error())
	}
}

// canvasEvents are the events after which a canvas is drawn again.
var canvasEvents = map[string]bool{"key": true, "click": true, "wheel": true, "focus": true, "blur": true}

// forgetClosed drops the handler and the canvases of a dialog that is gone.
func (s *surface) forgetClosed(id int) {
	if _, open := s.dialogs.Get(id); open {
		return
	}
	s.mu.Lock()
	delete(s.handlers, id)
	s.dropCanvases(id)
	s.mu.Unlock()
}

func eventTable(state *lua.LState, event tools.UIEvent) *lua.LTable {
	table := state.NewTable()
	table.RawSetString("dialog", lua.LNumber(event.UI))
	table.RawSetString("element", lua.LString(event.Element))
	table.RawSetString("event", lua.LString(event.Event))
	table.RawSetString("action", lua.LString(event.Action))
	table.RawSetString("value", lua.LString(event.Value))
	if event.Text != "" {
		table.RawSetString("text", lua.LString(event.Text))
	}
	if event.At {
		table.RawSetString("x", lua.LNumber(event.X))
		table.RawSetString("y", lua.LNumber(event.Y))
	}
	values := state.NewTable()
	for name, value := range event.Values {
		values.RawSetString(name, lua.LString(value))
	}
	table.RawSetString("values", values)
	return table
}

// Follow tells changed when an item or a dialog of a plugin changes, and
// open when a plugin opens a dialog, to show it at once.
func (s *Set) Follow(changed func(), open func(id int)) {
	if s == nil {
		return
	}
	s.surface.mu.Lock()
	defer s.surface.mu.Unlock()
	s.surface.changed, s.surface.open = changed, open
}

// Dialogs are the dialogs the plugins opened.
func (s *Set) Dialogs() *tools.UIs {
	if s == nil {
		return nil
	}
	return s.surface.dialogs
}

// SidebarItems are the items the plugins keep in the side panel, in the
// order they were first set.
func (s *Set) SidebarItems() []SidebarItem {
	if s == nil {
		return nil
	}
	s.surface.mu.Lock()
	defer s.surface.mu.Unlock()
	var items []SidebarItem
	for _, entry := range s.surface.items {
		if !entry.plugin.stuck.Load() {
			items = append(items, entry.item)
		}
	}
	return items
}

// ClickSidebar runs what an item does when clicked: the text it returns is
// for the user.
func (s *Set) ClickSidebar(ctx context.Context, key string) (string, error) {
	s.surface.mu.Lock()
	var found *sidebarEntry
	for _, entry := range s.surface.items {
		if entry.item.Key == key {
			found = entry
		}
	}
	s.surface.mu.Unlock()
	if found == nil || found.onClick == nil {
		return "", nil
	}
	result, err := found.plugin.callFunction(ctx, runTimeout, found.onClick, optionalText)
	text, _ := result.(string)
	return text, err
}

// sidebarTable is mo.sidebar: set and remove.
func (p *Plugin) sidebarTable() *lua.LTable {
	table := p.state.NewTable()
	table.RawSetString("set", p.state.NewFunction(p.setSidebarItem))
	table.RawSetString("remove", p.state.NewFunction(p.removeSidebarItem))
	return table
}

// setSidebarItem is mo.sidebar.set{ id, text, detail, color, icon,
// on_click }: it adds the item, or changes the one with that id.
func (p *Plugin) setSidebarItem(state *lua.LState) int {
	spec := state.CheckTable(1)
	id := luaString(spec.RawGetString("id"))
	text := luaString(spec.RawGetString("text"))
	if id == "" || text == "" {
		state.RaiseError("mo.sidebar.set needs an id and a text")
	}
	onClick, _ := spec.RawGetString("on_click").(*lua.LFunction)
	item := SidebarItem{
		Key: p.name + "/" + id, Plugin: p.name, Text: text,
		Detail: luaString(spec.RawGetString("detail")), Color: luaString(spec.RawGetString("color")),
		Icon: luaString(spec.RawGetString("icon")), Clickable: onClick != nil,
	}
	surface := p.surface
	surface.mu.Lock()
	entry := &sidebarEntry{plugin: p, item: item, onClick: onClick}
	replaced := false
	for i, each := range surface.items {
		if each.item.Key == item.Key {
			surface.items[i], replaced = entry, true
		}
	}
	if !replaced {
		surface.items = append(surface.items, entry)
	}
	surface.mu.Unlock()
	surface.tell()
	return 0
}

func (p *Plugin) removeSidebarItem(state *lua.LState) int {
	key := p.name + "/" + state.CheckString(1)
	surface := p.surface
	surface.mu.Lock()
	before := len(surface.items)
	kept := surface.items[:0]
	for _, entry := range surface.items {
		if entry.item.Key != key {
			kept = append(kept, entry)
		}
	}
	surface.items = kept
	removed := len(kept) != before
	surface.mu.Unlock()
	if removed {
		surface.tell()
	}
	return 0
}

// openDialog is mo.dialog{ title, tree, on_event, size, listed, frame }: it shows
// a dialog of the nodes the ui tool takes, and the plugin's components, and
// gives { id, update, close }. on_event gets what the user does in it.
func (p *Plugin) openDialog(state *lua.LState) int {
	spec := state.CheckTable(1)
	tree, canvases := p.dialogTree(state, spec.RawGetString("tree"))
	onEvent, _ := spec.RawGetString("on_event").(*lua.LFunction)
	options := tools.DialogOptions{
		Size:   luaString(spec.RawGetString("size")),
		Listed: lua.LVAsBool(spec.RawGetString("listed")),
		Bare:   spec.RawGetString("frame") == lua.LFalse,
	}
	surface := p.surface
	id, err := surface.dialogs.Open(p.name, luaString(spec.RawGetString("title")), *tree, options)
	if err != nil {
		state.RaiseError("mo.dialog: %v", err)
	}
	surface.setCanvases(p, id, canvases, *tree)
	surface.mu.Lock()
	surface.handlers[id] = dialogHandler{plugin: p, onEvent: onEvent}
	open := surface.open
	surface.mu.Unlock()
	if open != nil {
		open(id)
	}
	state.Push(p.dialogHandle(id))
	return 1
}

func (p *Plugin) dialogTree(state *lua.LState, value lua.LValue) (*tools.UINode, []canvasSpec) {
	var canvases []canvasSpec
	plain, err := toGo(takeCanvases(state, value, &canvases))
	if err != nil {
		state.RaiseError("mo.dialog: tree: %v", err)
	}
	plain, err = p.expandComponents(state, plain, 0, &canvases)
	if err != nil {
		state.RaiseError("mo.dialog: %v", err)
	}
	tree, err := tools.PluginTree(plain)
	if err != nil {
		state.RaiseError("mo.dialog: %v", err)
	}
	if err := matchCanvases(*tree, canvases); err != nil {
		state.RaiseError("mo.dialog: %v", err)
	}
	return tree, canvases
}

// dialogHandle is what mo.dialog gives: the dialog's id, update{ title,
// tree }, redraw(canvas), bounds() and close().
func (p *Plugin) dialogHandle(id int) *lua.LTable {
	state := p.state
	handle := state.NewTable()
	handle.RawSetString("id", lua.LNumber(id))
	handle.RawSetString("update", state.NewFunction(func(state *lua.LState) int {
		spec := state.CheckTable(1)
		var tree *tools.UINode
		var canvases []canvasSpec
		if value := spec.RawGetString("tree"); value != lua.LNil {
			tree, canvases = p.dialogTree(state, value)
		}
		if err := p.surface.dialogs.Replace(id, luaString(spec.RawGetString("title")), tree); err != nil {
			state.RaiseError("dialog update: %v", err)
		}
		if tree != nil {
			p.surface.setCanvases(p, id, canvases, *tree)
		}
		return 0
	}))
	handle.RawSetString("redraw", state.NewFunction(func(state *lua.LState) int {
		p.surface.redrawCanvas(id, state.OptString(1, ""))
		return 0
	}))
	handle.RawSetString("bounds", state.NewFunction(p.dialogBounds(id)))
	handle.RawSetString("close", state.NewFunction(func(*lua.LState) int {
		p.surface.mu.Lock()
		delete(p.surface.handlers, id)
		p.surface.dropCanvases(id)
		p.surface.mu.Unlock()
		_ = p.surface.dialogs.Close(id)
		return 0
	}))
	return handle
}
