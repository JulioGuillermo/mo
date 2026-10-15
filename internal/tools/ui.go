package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/sessions"
)

// UI node types.
const (
	UIPanel  = "panel"
	UICard   = "card"
	UILabel  = "label"
	UIButton = "button"
	UIInput  = "input"
	// A grid lays its children in columns, row by row, as a table.
	UIGrid = "grid"
	// Controls holding a value the user picks.
	UISelect   = "select"
	UISlider   = "slider"
	UICheckbox = "checkbox"
	UISwitch   = "switch"
	UIRadio    = "radio"
)

// UITypes are every node type.
var UITypes = []string{UIPanel, UICard, UIGrid, UILabel, UIButton, UIInput, UISelect, UISlider, UICheckbox, UISwitch, UIRadio}

// UI event actions an element can name: send the event to the model, send
// every input's value, close the dialog, or remove the UI and say so.
const (
	UINotify = "notify"
	UISubmit = "submit"
	UIClose  = "close"
	UIRemove = "remove"
)

// UINode is one element of an interface the model draws: a panel or card
// laying out its children in a row or a column, a label, a button, an
// input, a select, a slider, a checkbox, a switch or a radio. Every one
// takes colours, margin and padding.
type UINode struct {
	Type        string   `json:"type"`
	ID          string   `json:"id,omitempty"`
	Text        string   `json:"text,omitempty"`
	Label       string   `json:"label,omitempty"`
	Value       string   `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []string `json:"options,omitempty"`
	Min         float64  `json:"min,omitempty"`
	Max         float64  `json:"max,omitempty"`
	Step        float64  `json:"step,omitempty"`
	Title       string   `json:"title,omitempty"`
	Direction   string   `json:"direction,omitempty"`
	Columns     int      `json:"columns,omitempty"`
	Widths      []int    `json:"widths,omitempty"`
	Justify     string   `json:"justify,omitempty"`
	Align       string   `json:"align,omitempty"`
	Gap         int      `json:"gap,omitempty"`
	Grow        int      `json:"grow,omitempty"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	Padding     []int    `json:"padding,omitempty"`
	Margin      []int    `json:"margin,omitempty"`
	BG          string   `json:"bg,omitempty"`
	FG          string   `json:"fg,omitempty"`
	Border      string   `json:"border,omitempty"`
	Bold        bool     `json:"bold,omitempty"`
	Visible     *bool    `json:"visible,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
	OnClick     string   `json:"on_click,omitempty"`
	OnInput     string   `json:"on_input,omitempty"`
	OnChange    string   `json:"on_change,omitempty"`
	Children    []UINode `json:"children,omitempty"`
}

// Shown tells whether a node is drawn.
func (n UINode) Shown() bool {
	return n.Visible == nil || *n.Visible
}

// UIColors are the colour names a node may take besides #rrggbb.
var UIColors = []string{"text", "dim", "accent", "error", "warning", "success", "surface", "background"}

var (
	hexColor     = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	luaName      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	uiJustify    = []string{"", "start", "center", "end", "space-between", "space-around"}
	uiAlign      = []string{"", "start", "center", "end", "stretch"}
	uiBorders    = []string{"", "none", "rounded", "normal", "thick", "double"}
	uiDirections = []string{"", "row", "column"}
)

// validateUI checks a tree before it is shown, naming the first problem by
// its path, so the model can mend it.
func validateUI(root UINode) error {
	ids := map[string]bool{}
	var check func(n UINode, path string) error
	check = func(n UINode, path string) error {
		where := path + " (" + n.Type + ")"
		if !slices.Contains(UITypes, n.Type) {
			return fmt.Errorf("%s: type %q: use %s, e.g. %s", path, n.Type, strings.Join(UITypes, ", "), uiTreeExample)
		}
		if n.Focusable() && n.ID == "" {
			return fmt.Errorf("%s needs an id", where)
		}
		if err := checkValue(n, where); err != nil {
			return err
		}
		if n.ID != "" {
			if ids[n.ID] {
				return fmt.Errorf("%s: id %q is used twice", where, n.ID)
			}
			ids[n.ID] = true
		}
		if len(n.Children) > 0 && n.Type != UIPanel && n.Type != UICard && n.Type != UIGrid {
			return fmt.Errorf("%s: only a panel, a card or a grid has children", where)
		}
		if n.Type == UIGrid && n.Columns < 1 && len(n.Widths) == 0 {
			return fmt.Errorf(`%s needs columns, e.g. "columns":2 for labels beside their controls`, where)
		}
		if slices.ContainsFunc(n.Widths, func(w int) bool { return w < 0 }) {
			return fmt.Errorf("%s: widths are cells, 0 for as wide as needed", where)
		}
		for name, value := range map[string]string{"bg": n.BG, "fg": n.FG} {
			if value != "" && !hexColor.MatchString(value) && !slices.Contains(UIColors, value) {
				return fmt.Errorf("%s: %s %q: use #rrggbb or one of %s", where, name, value, strings.Join(UIColors, ", "))
			}
		}
		for name, value := range map[string]struct {
			value string
			valid []string
		}{"justify": {n.Justify, uiJustify}, "align": {n.Align, uiAlign}, "border": {n.Border, uiBorders}, "direction": {n.Direction, uiDirections}} {
			if !slices.Contains(value.valid, value.value) {
				return fmt.Errorf("%s: %s %q: use %s", where, name, value.value, strings.Join(value.valid[1:], ", "))
			}
		}
		for name, sides := range map[string][]int{"padding": n.Padding, "margin": n.Margin} {
			if len(sides) != 0 && len(sides) != 1 && len(sides) != 2 && len(sides) != 4 {
				return fmt.Errorf("%s: %s takes 1, 2 or 4 numbers, as in CSS", where, name)
			}
		}
		for name, handler := range map[string]string{"on_click": n.OnClick, "on_input": n.OnInput, "on_change": n.OnChange} {
			if handler != "" && !luaName.MatchString(handler) {
				return fmt.Errorf("%s: %s %q: use notify, submit, close or the name of a function of the script", where, name, handler)
			}
		}
		for i, child := range n.Children {
			if err := check(child, fmt.Sprintf("%s.children[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	}
	return check(root, "tree")
}

// UI is an interface the model made, in a conversation, with what the user
// typed and what its script changed.
type UI struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Tree   UINode `json:"tree"`
	Script string `json:"script,omitempty"`
	// Values are what the user typed, by input; Changes what the script
	// set, by element; State the script's own table.
	Values  map[string]string         `json:"values,omitempty"`
	Changes map[string]map[string]any `json:"changes,omitempty"`
	State   map[string]any            `json:"state,omitempty"`
	// Error is the script's last error, shown under the interface.
	Error string `json:"error,omitempty"`
	// Version counts the model's updates, Seen whether the user opened it
	// since the last.
	Version      int    `json:"version"`
	Seen         bool   `json:"seen"`
	Conversation string `json:"-"`
	script       *uiScript
}

// Shown is the tree as it is now: the script's changes and what the user
// typed put in.
func (ui UI) Shown() UINode {
	return shownNode(ui.Tree, ui.Changes, ui.Values)
}

func shownNode(n UINode, changes map[string]map[string]any, values map[string]string) UINode {
	if n.ID != "" {
		if props, ok := changes[n.ID]; ok {
			_ = applyProps(&n, props)
		}
		if value, ok := values[n.ID]; ok && n.HasValue() {
			n.Value = value
		}
	}
	if len(n.Children) > 0 {
		children := make([]UINode, len(n.Children))
		for i, child := range n.Children {
			children[i] = shownNode(child, changes, values)
		}
		n.Children = children
	}
	return n
}

// UIEvent is what the user did in an interface that the model is to hear.
type UIEvent struct {
	UI      int
	Title   string
	Action  string
	Event   string
	Element string
	Value   string
	Values  map[string]string
	// Data is what a script passed to notify or submit, as JSON.
	Data string
	conv string
}

// UIError is the action of an event that says a script failed.
const UIError = "error"

// Note is how the model hears of an event.
func (e UIEvent) Note() string {
	var b strings.Builder
	fmt.Fprintf(&b, "UI %d %q: ", e.UI, e.Title)
	switch e.Action {
	case UISubmit:
		fmt.Fprintf(&b, "submitted with %s", e.Element)
	case UIRemove:
		fmt.Fprintf(&b, "removed by the user with %s", e.Element)
	case UIError:
		fmt.Fprintf(&b, "its script failed on %s %s: %s", e.Event, e.Element, e.Value)
		return b.String()
	default:
		fmt.Fprintf(&b, "%s on %s", e.Event, e.Element)
		if e.Value != "" {
			fmt.Fprintf(&b, ", value %q", e.Value)
		}
	}
	if len(e.Values) > 0 {
		keys := make([]string, 0, len(e.Values))
		for key := range e.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var pairs []string
		for _, key := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%q", key, e.Values[key]))
		}
		b.WriteString("; values: " + strings.Join(pairs, ", "))
	}
	if e.Data != "" {
		b.WriteString("; data: " + e.Data)
	}
	return b.String()
}

// UIOutcome is what an event led to: the model told of it, the interface
// submitted, its dialog closed, or the interface removed.
type UIOutcome struct {
	Told, Submitted, Hidden, Removed bool
}

// UIs holds the interfaces of the conversation in progress and what the
// user did in them, for the model to hear at its next step. With a root,
// each conversation keeps its own beside its file.
type UIs struct {
	mu           sync.Mutex
	root         string
	conversation string
	uis          []*UI
	events       []UIEvent
	onChange     func()
}

func NewUIs(root string) *UIs {
	return &UIs{root: root}
}

// OnChange sets what is told when the model creates, updates or removes an
// interface, so the side panel shows it.
func (u *UIs) OnChange(notify func()) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.onChange = notify
}

func (u *UIs) changed() {
	u.mu.Lock()
	notify := u.onChange
	u.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// SetConversation names the conversation in progress, and brings back the
// interfaces it kept, scripts running again with their state.
func (u *UIs) SetConversation(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if id == u.conversation {
		return
	}
	u.conversation = id
	if id == "" || u.root == "" || slices.ContainsFunc(u.uis, func(each *UI) bool { return each.Conversation == id }) {
		return
	}
	data, err := os.ReadFile(sessions.UIFile(u.root, id))
	if err != nil {
		return
	}
	var kept []*UI
	if json.Unmarshal(data, &kept) != nil {
		return
	}
	for _, ui := range kept {
		ui.Conversation = id
		if ui.Script != "" {
			if script, err := loadScript(ui); err == nil {
				ui.script = script
			} else {
				ui.Error = err.Error()
			}
		}
		u.uis = append(u.uis, ui)
	}
}

// save writes the interfaces of the conversation in progress to its
// folder; the lock is held.
func (u *UIs) save() {
	if u.root == "" || u.conversation == "" {
		return
	}
	var kept []*UI
	for _, each := range u.uis {
		if each.Conversation == u.conversation {
			kept = append(kept, each)
		}
	}
	path := sessions.UIFile(u.root, u.conversation)
	if len(kept) == 0 {
		os.Remove(path)
		return
	}
	data, err := json.Marshal(kept)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// List returns the interfaces of the conversation in progress, the oldest
// first.
func (u *UIs) List() []UI {
	u.mu.Lock()
	defer u.mu.Unlock()
	var list []UI
	for _, each := range u.uis {
		if each.Conversation == u.conversation {
			list = append(list, each.copied())
		}
	}
	return list
}

// copied is an interface with maps of its own, safe to read while the
// script or the user changes the one kept.
func (ui *UI) copied() UI {
	out := *ui
	out.Values = maps.Clone(ui.Values)
	out.State = maps.Clone(ui.State)
	if ui.Changes != nil {
		out.Changes = map[string]map[string]any{}
		for id, props := range ui.Changes {
			out.Changes[id] = maps.Clone(props)
		}
	}
	return out
}

// Get returns an interface of the conversation in progress.
func (u *UIs) Get(id int) (UI, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if found := u.find(id); found != nil {
		return found.copied(), true
	}
	return UI{}, false
}

func (u *UIs) find(id int) *UI {
	for _, each := range u.uis {
		if each.Conversation == u.conversation && each.ID == id {
			return each
		}
	}
	return nil
}

// MarkSeen notes the user opened an interface as it is now.
func (u *UIs) MarkSeen(id int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if found := u.find(id); found != nil && !found.Seen {
		found.Seen = true
		u.save()
	}
}

// SetValue keeps what the user typed in an input.
func (u *UIs) SetValue(id int, element, value string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if found := u.find(id); found != nil {
		if found.Values == nil {
			found.Values = map[string]string{}
		}
		found.Values[element] = value
	}
}

func (u *UIs) create(title, script string, tree UINode) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.conversation == "" {
		return 0, errors.New("interfaces are not available here")
	}
	next := 1
	for _, each := range u.uis {
		if each.Conversation == u.conversation {
			next = max(next, each.ID+1)
		}
	}
	ui := &UI{ID: next, Title: title, Tree: tree, Script: script, Conversation: u.conversation, Version: 1}
	if err := ui.startScript(); err != nil {
		return 0, err
	}
	u.uis = append(u.uis, ui)
	u.save()
	return next, nil
}

func (u *UIs) update(id int, title string, tree *UINode, script *string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	found := u.find(id)
	if found == nil {
		return fmt.Errorf("no UI %d; create one first", id)
	}
	next := *found
	if title != "" {
		next.Title = title
	}
	if tree != nil {
		next.Tree = *tree
		next.Changes = nil
	}
	if script != nil {
		next.Script = *script
	}
	if tree != nil || script != nil {
		if err := next.startScript(); err != nil {
			return err
		}
	}
	next.Version++
	next.Seen = false
	next.Error = ""
	*found = next
	u.save()
	return nil
}

// startScript runs the script anew, its state kept, after checking every
// handler the tree names is one of its functions.
func (ui *UI) startScript() error {
	var names []string
	collectHandlers(ui.Tree, &names)
	if ui.Script == "" {
		if len(names) > 0 {
			return fmt.Errorf("%s names a function, and there is no script", names[0])
		}
		ui.script = nil
		return nil
	}
	script, err := loadScript(ui)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, ok := script.state.GetGlobal(name).(*lua.LFunction); !ok {
			return fmt.Errorf("the script defines no function %q", name)
		}
	}
	ui.script = script
	return nil
}

func collectHandlers(n UINode, names *[]string) {
	for _, handler := range []string{n.OnClick, n.OnInput, n.OnChange} {
		switch handler {
		case "", UINotify, UISubmit, UIClose, UIRemove:
		default:
			*names = append(*names, handler)
		}
	}
	for _, child := range n.Children {
		collectHandlers(child, names)
	}
}

// Remove takes an interface away, as the model or the user asks.
func (u *UIs) Remove(id int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.find(id) == nil {
		return fmt.Errorf("no UI %d", id)
	}
	u.uis = slices.DeleteFunc(u.uis, func(each *UI) bool { return each.Conversation == u.conversation && each.ID == id })
	u.save()
	return nil
}

// Fire does what an element names for an event: tell the model, send it
// every value, close the interface, or call the script's function, which
// may do any of those. A button that names nothing tells the model.
func (u *UIs) Fire(id int, element, event string) UIOutcome {
	u.mu.Lock()
	defer u.mu.Unlock()
	ui := u.find(id)
	if ui == nil {
		return UIOutcome{}
	}
	node, ok := findUINode(ui.Shown(), element)
	if !ok {
		return UIOutcome{}
	}
	handler := map[string]string{"click": node.OnClick, "input": node.OnInput, "change": node.OnChange}[event]
	if handler == "" && event == "click" {
		handler = UINotify
	}
	value := node.Current()
	base := UIEvent{UI: ui.ID, Title: ui.Title, Event: event, Element: element, Value: value, conv: u.conversation}
	var outcome UIOutcome
	switch handler {
	case "":
		return outcome
	case UINotify, UISubmit, UIClose, UIRemove:
		outcome = u.act(ui, base, handler, "")
	default:
		outcome = u.runHandler(ui, base, handler)
	}
	if outcome.Removed {
		u.uis = slices.DeleteFunc(u.uis, func(each *UI) bool { return each == ui })
	}
	u.save()
	return outcome
}

// act queues what an action tells the model; the lock is held.
func (u *UIs) act(ui *UI, base UIEvent, action, data string) UIOutcome {
	event := base
	event.Action, event.Data = action, data
	switch action {
	case UISubmit:
		event.Values = inputValuesOf(ui.Shown())
		u.events = append(u.events, event)
		return UIOutcome{Told: true, Submitted: true}
	case UIClose:
		return UIOutcome{Hidden: true}
	case UIRemove:
		u.events = append(u.events, event)
		return UIOutcome{Told: true, Removed: true}
	}
	u.events = append(u.events, event)
	return UIOutcome{Told: true}
}

// Event queues what the user did for the model.
func (u *UIs) Event(event UIEvent) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if found := u.find(event.UI); found != nil {
		event.Title = found.Title
	}
	event.conv = u.conversation
	u.events = append(u.events, event)
}

// TakeEvents returns the events of the conversation in progress the model
// has not heard of.
func (u *UIs) TakeEvents() []UIEvent {
	u.mu.Lock()
	defer u.mu.Unlock()
	var taken, kept []UIEvent
	for _, event := range u.events {
		if event.conv == u.conversation {
			taken = append(taken, event)
		} else {
			kept = append(kept, event)
		}
	}
	u.events = kept
	return taken
}

// HasEvents tells whether the model has events of the conversation in
// progress to hear.
func (u *UIs) HasEvents() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.ContainsFunc(u.events, func(event UIEvent) bool { return event.conv == u.conversation })
}

func findUINode(n UINode, id string) (UINode, bool) {
	if n.ID == id {
		return n, true
	}
	for _, child := range n.Children {
		if found, ok := findUINode(child, id); ok {
			return found, true
		}
	}
	return UINode{}, false
}

func inputValuesOf(n UINode) map[string]string {
	values := map[string]string{}
	var walk func(n UINode)
	walk = func(n UINode) {
		if n.HasValue() {
			values[n.ID] = n.Current()
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(n)
	return values
}

var uiHelp = []helpTopic{
	{"create", "title, tree, script (optional): shows the interface and returns its id. What the user does in it comes to you as notes."},
	{"update", "id, and any of title, tree, script: replaces them. What the user set and the script's state stay."},
	{"remove", "id: removes the interface."},
	{"nodes", `Every node is {"type": ..., more fields}; panel, card and grid hold nodes in "children". Buttons and controls need an id.
panel     children, direction row|column, gap, justify start|center|end|space-between|space-around, align start|center|end|stretch
card      a panel with a border and a title
grid      children row by row in columns (a number), each column as wide as its widest cell, like a table; gap between columns,
          widths [12,0] fixes some (0: auto), align start|center|end|stretch in the cells, a cell's grow widens its column.
          Use it to line labels up beside their controls.
label     text
button    label, on_click, disabled
input     value, placeholder, on_input, on_change
select    options ["a","b"], value, label, on_change
slider    min, max, step (0, 100, 1 unless set), value, label, on_change
checkbox  label, value "true"|"false", on_change
switch    as checkbox, drawn as a switch
radio     options, value, label, direction row|column, on_change
any node  id, fg, bg (#rrggbb or text|dim|accent|error|warning|success|surface|background), bold, visible, border rounded|normal|thick|double|none,
          padding, margin (1, 2 or 4 numbers, as in CSS), width, height, grow (share of the room left in a row)`},
	{"events", `on_click (button), on_input (each key in an input), on_change (an input when left or on enter; any other control at each change) take:
notify   tell you of this event (only when you must act on it)
submit   tell you every control's value, and close the dialog
close    close the dialog; the interface stays
remove   remove the interface, telling you
<name>   call that function of the script
A button without on_click notifies.`},
	{"script", `Lua (base, table, string, math), run on the user's side. A function gets e.id, e.type (click|input|change), e.value.
ui.set shows its change at once: updating the interface needs no notify. Tell yourself only what you must act on, usually once, with submit.
ui.set(id, {text=..., ...})  change a node: text, label, value, options, min, max, step, placeholder, title, fg, bg, border, bold, visible, disabled, width, height
ui.get(id)                   a node's fields, and checked
ui.value(id)                 a control's value; a checkbox's is "true" or "false"
ui.state                     a table kept between events
ui.notify(data)              optional: tell you, with data; ui.submit(data) also sends every value
ui.close()                   close the dialog; ui.remove() removes the interface
Validate in on_input, and submit from a button's function once valid.`},
	{"example", `{"action":"create","title":"Login","tree":` + uiTreeExample + `}
with a script:
{"action":"create","title":"Name","script":"function check(e) ui.set(\"ok\", {disabled = e.value == \"\"}) end","tree":{"type":"panel","children":[{"type":"input","id":"name","on_input":"check"},{"type":"button","id":"ok","label":"Send","disabled":true,"on_click":"submit"}]}}
controls, labels lined up in a grid:
{"action":"create","title":"Prefs","tree":{"type":"card","title":"Prefs","children":[{"type":"grid","columns":2,"gap":2,"children":[{"type":"label","text":"Size"},{"type":"select","id":"size","options":["S","M","L"],"value":"M"},{"type":"label","text":"Volume"},{"type":"slider","id":"volume","min":0,"max":10,"value":"4"},{"type":"label","text":"Dark mode"},{"type":"switch","id":"dark","value":"true"},{"type":"label","text":"Plan"},{"type":"radio","id":"plan","options":["free","pro"],"direction":"row"}]},{"type":"checkbox","id":"terms","label":"I agree"},{"type":"button","id":"go","label":"Save","on_click":"submit"}]}}`},
}

func uiTool() Tool {
	return Tool{
		Name: "ui",
		Description: "Show the user an interface in a dialog: labels, buttons, inputs, selects, sliders, checkboxes, switches and radios, laid out in panels, cards and grids. " +
			`Its Lua script updates it on the user's side by itself; you hear only what it notifies or submits. Unsure how? Call {"action":"help"}. A tree: ` + uiTreeExample,
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["create","update","remove","help"]},
"id":{"type":"integer","description":"for update and remove"},
"title":{"type":"string"},
"tree":{"type":"object","description":"the root node, {\"type\":...,\"children\":[...]}; help topic nodes"},
"script":{"type":"string","description":"Lua functions the events name; help topic script"},
"topic":{"type":"string","description":"for help: nodes, events, script, example, create, update or remove; none for all"}},"required":["action"]}`),
		Run:       runUI,
		LooseArgs: false,
		Summary: func(args json.RawMessage) string {
			return strings.TrimSpace(argField(args, "action") + " " + argField(args, "title") + argField(args, "topic"))
		},
	}
}

func runUI(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Action string          `json:"action"`
		ID     int             `json:"id"`
		Title  string          `json:"title"`
		Tree   json.RawMessage `json:"tree"`
		Script *string         `json:"script"`
		Topic  string          `json:"topic"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Action == "help" {
		return helpOn(uiHelp, args.Topic)
	}
	if env.UIs == nil {
		return "", errors.New("interfaces are not available here")
	}
	tree, err := ParseUITree(args.Tree)
	if err != nil {
		return "", err
	}
	if tree != nil {
		if err := validateUI(*tree); err != nil {
			return "", err
		}
	}
	switch args.Action {
	case "create":
		if tree == nil {
			return "", fmt.Errorf("create needs a tree, e.g. %s", uiTreeExample)
		}
		script := ""
		if args.Script != nil {
			script = *args.Script
		}
		id, err := env.UIs.create(orDefaultString(args.Title, "Interface"), script, *tree)
		if err != nil {
			return "", err
		}
		env.UIs.changed()
		return fmt.Sprintf("UI %d created; it is listed in the user's side panel, and what they do in it comes to you as a note", id), nil
	case "update":
		if err := env.UIs.update(args.ID, args.Title, tree, args.Script); err != nil {
			return "", err
		}
		env.UIs.changed()
		return fmt.Sprintf("UI %d updated", args.ID), nil
	case "remove":
		if err := env.UIs.Remove(args.ID); err != nil {
			return "", err
		}
		env.UIs.changed()
		return fmt.Sprintf("UI %d removed", args.ID), nil
	}
	return "", fmt.Errorf(`action %q: use create, update, remove or help; {"action":"help"} explains them`, args.Action)
}

func orDefaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
