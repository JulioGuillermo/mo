package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// UI node types.
const (
	UIPanel  = "panel"
	UICard   = "card"
	UILabel  = "label"
	UIButton = "button"
	UIInput  = "input"
)

// UI event actions an element can name: send the event to the model, send
// every input's value, or close the UI.
const (
	UINotify = "notify"
	UISubmit = "submit"
	UIClose  = "close"
)

// UINode is one element of an interface the model draws: a panel or card
// laying out its children in a row or a column, a label, a button, an
// input. Every one takes colours, margin and padding.
type UINode struct {
	Type        string   `json:"type"`
	ID          string   `json:"id,omitempty"`
	Text        string   `json:"text,omitempty"`
	Label       string   `json:"label,omitempty"`
	Value       string   `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Title       string   `json:"title,omitempty"`
	Direction   string   `json:"direction,omitempty"`
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
		switch n.Type {
		case UIPanel, UICard, UILabel, UIButton, UIInput:
		default:
			return fmt.Errorf("%s: type %q: use panel, card, label, button or input", path, n.Type)
		}
		if (n.Type == UIButton || n.Type == UIInput) && n.ID == "" {
			return fmt.Errorf("%s needs an id", where)
		}
		if n.ID != "" {
			if ids[n.ID] {
				return fmt.Errorf("%s: id %q is used twice", where, n.ID)
			}
			ids[n.ID] = true
		}
		if len(n.Children) > 0 && n.Type != UIPanel && n.Type != UICard {
			return fmt.Errorf("%s: only a panel or a card has children", where)
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
			switch handler {
			case "", UINotify, UISubmit, UIClose:
			default:
				return fmt.Errorf("%s: %s %q: use notify, submit or close (script handlers are not there yet)", where, name, handler)
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

// UI is an interface the model made, in a conversation.
type UI struct {
	ID           int
	Title        string
	Tree         UINode
	Conversation string
	// Version counts the model's updates, Seen whether the user opened it
	// since the last.
	Version int
	Seen    bool
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
	conv    string
}

// Note is how the model hears of an event.
func (e UIEvent) Note() string {
	var b strings.Builder
	fmt.Fprintf(&b, "UI %d %q: ", e.UI, e.Title)
	switch e.Action {
	case UISubmit:
		fmt.Fprintf(&b, "submitted with %s", e.Element)
	case UIClose:
		fmt.Fprintf(&b, "closed by the user with %s", e.Element)
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
	return b.String()
}

// UIs holds the interfaces of the conversation in progress and what the
// user did in them, for the model to hear at its next step.
type UIs struct {
	mu           sync.Mutex
	conversation string
	uis          []*UI
	events       []UIEvent
	onChange     func()
}

func NewUIs() *UIs {
	return &UIs{}
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

// SetConversation names the conversation in progress.
func (u *UIs) SetConversation(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.conversation = id
}

// List returns the interfaces of the conversation in progress, the oldest
// first.
func (u *UIs) List() []UI {
	u.mu.Lock()
	defer u.mu.Unlock()
	var list []UI
	for _, each := range u.uis {
		if each.Conversation == u.conversation {
			list = append(list, *each)
		}
	}
	return list
}

// Get returns an interface of the conversation in progress.
func (u *UIs) Get(id int) (UI, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if found := u.find(id); found != nil {
		return *found, true
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
	if found := u.find(id); found != nil {
		found.Seen = true
	}
}

func (u *UIs) create(title string, tree UINode) (int, error) {
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
	u.uis = append(u.uis, &UI{ID: next, Title: title, Tree: tree, Conversation: u.conversation, Version: 1})
	return next, nil
}

func (u *UIs) update(id int, title string, tree *UINode) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	found := u.find(id)
	if found == nil {
		return fmt.Errorf("no UI %d; create one first", id)
	}
	if title != "" {
		found.Title = title
	}
	if tree != nil {
		found.Tree = *tree
	}
	found.Version++
	found.Seen = false
	return nil
}

// Remove takes an interface away, as the model or the user asks.
func (u *UIs) Remove(id int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.find(id) == nil {
		return fmt.Errorf("no UI %d", id)
	}
	u.uis = slices.DeleteFunc(u.uis, func(each *UI) bool { return each.Conversation == u.conversation && each.ID == id })
	return nil
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

var uiHelp = []helpTopic{
	{"create", "title, tree: show an interface in the user's side panel, which opens it as a dialog; returns its id. What the user does in it comes to you as a note."},
	{"update", "id, and tree or title: replace it; inputs keep what the user typed."},
	{"remove", "id: take it away."},
	{"nodes", "panel: children in a row or column (direction), justify start|center|end|space-between|space-around, align start|center|end|stretch, gap. " +
		"card: a panel with a border and a title. label: text. button: label, disabled. input: value, placeholder, width. " +
		"Every node: id, bg, fg (#rrggbb or text, dim, accent, error, warning, success, surface, background), padding and margin (1, 2 or 4 numbers, as in CSS), " +
		"border (rounded, normal, thick, double, none), bold, visible, width, height, grow (a share of the room left in a row)."},
	{"events", "on_click (button), on_input (each key in an input), on_change (an input when left or on enter): notify sends you the event; submit sends you every input's value; close takes the interface away and tells you. A button without on_click notifies."},
}

func uiTool() Tool {
	return Tool{
		Name:        "ui",
		Description: "Show the user an interface of panels, cards, labels, buttons and inputs, in their side panel; what they do in it comes back to you. help explains the nodes.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["create","update","remove","help"]},"id":{"type":"integer"},"title":{"type":"string"},
"tree":{"type":"object"},"topic":{"type":"string"}},"required":["action"]}`),
		Run:       runUI,
		LooseArgs: false,
		Summary: func(args json.RawMessage) string {
			return strings.TrimSpace(argField(args, "action") + " " + argField(args, "title") + argField(args, "topic"))
		},
	}
}

func runUI(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Action string  `json:"action"`
		ID     int     `json:"id"`
		Title  string  `json:"title"`
		Tree   *UINode `json:"tree"`
		Topic  string  `json:"topic"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Action == "help" {
		return help(uiHelp, args.Topic)
	}
	if env.UIs == nil {
		return "", errors.New("interfaces are not available here")
	}
	if args.Tree != nil {
		if err := validateUI(*args.Tree); err != nil {
			return "", err
		}
	}
	switch args.Action {
	case "create":
		if args.Tree == nil {
			return "", errors.New("create needs a tree")
		}
		id, err := env.UIs.create(orDefaultString(args.Title, "Interface"), *args.Tree)
		if err != nil {
			return "", err
		}
		env.UIs.changed()
		return fmt.Sprintf("UI %d created; it is listed in the user's side panel, and what they do in it comes to you as a note", id), nil
	case "update":
		if err := env.UIs.update(args.ID, args.Title, args.Tree); err != nil {
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
	return "", fmt.Errorf("action %q: use create, update, remove or help", args.Action)
}

func orDefaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
