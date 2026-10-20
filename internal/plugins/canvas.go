package plugins

import (
	"context"
	"fmt"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/tools"
)

const (
	defaultUIFPS   = 20
	redrawAskLimit = 50 * time.Millisecond
)

// canvasSpec is what a plugin's tree gives a canvas besides its fields:
// draw, which gives its lines at a size, and redraw, true or a function
// telling whether to draw it again by itself.
type canvasSpec struct {
	id     string
	draw   *lua.LFunction
	redraw lua.LValue
}

// canvas is a canvas of a dialog on screen: its code, and the lines last
// drawn at the size they were drawn at.
type canvas struct {
	plugin *Plugin
	dialog int
	canvasSpec
	fps int

	width, height  int
	lines          []string
	dirty, drawing bool
	lastFrame      time.Time
}

func canvasKey(dialog int, id string) string {
	return fmt.Sprintf("%d/%s", dialog, id)
}

// SetUIFPS sets how many times a second, at most, canvases are drawn again
// by themselves.
func (s *Set) SetUIFPS(fps int) {
	if s == nil || fps <= 0 {
		return
	}
	s.surface.mu.Lock()
	defer s.surface.mu.Unlock()
	s.surface.fps = fps
}

// setCanvases puts the canvases of a dialog's tree in place of those it had.
func (s *surface) setCanvases(plugin *Plugin, dialog int, specs []canvasSpec, tree tools.UINode) {
	s.mu.Lock()
	s.dropCanvases(dialog)
	ticking := false
	for _, spec := range specs {
		node, _ := findCanvas(tree, spec.id)
		s.canvases[canvasKey(dialog, spec.id)] = &canvas{plugin: plugin, dialog: dialog, canvasSpec: spec, fps: node.FPS, dirty: true}
		ticking = ticking || spec.redraw != lua.LNil && spec.redraw != nil && spec.redraw != lua.LFalse
	}
	start := ticking && !s.ticking
	s.ticking = s.ticking || ticking
	s.mu.Unlock()
	if start {
		go s.tick()
	}
}

// dropCanvases forgets the canvases of a dialog; the lock is held.
func (s *surface) dropCanvases(dialog int) {
	prefix := canvasKey(dialog, "")
	for key := range s.canvases {
		if strings.HasPrefix(key, prefix) {
			delete(s.canvases, key)
		}
	}
}

func findCanvas(n tools.UINode, id string) (tools.UINode, bool) {
	if n.Type == tools.UICanvas && n.ID == id {
		return n, true
	}
	for _, child := range n.Children {
		if found, ok := findCanvas(child, id); ok {
			return found, true
		}
	}
	return tools.UINode{}, false
}

// canvasLines gives a canvas's lines at once, and has it drawn anew, off
// the caller's goroutine, when its size changed or it asked to be.
func (s *surface) canvasLines(dialog int, id string, width, height int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.canvases[canvasKey(dialog, id)]
	if c == nil {
		return nil
	}
	resized := c.width != width || c.height != height
	if (resized || c.dirty) && !c.drawing {
		c.drawing, c.dirty = true, false
		go s.drawCanvas(c, width, height, resized && c.lines != nil)
	}
	return c.lines
}

// drawCanvas runs a canvas's draw at a size, telling on_event first when
// the size is new, and shows what it gave; an error shows under the dialog.
func (s *surface) drawCanvas(c *canvas, width, height int, resized bool) {
	if resized {
		s.deliver(tools.UIEvent{UI: c.dialog, Element: c.id, Event: "resize", Action: "resize", Value: fmt.Sprintf("%dx%d", width, height)})
	}
	result, err := c.plugin.callFunction(context.Background(), hookTimeout, c.draw, canvasText,
		func(*lua.LState) lua.LValue { return lua.LNumber(width) },
		func(*lua.LState) lua.LValue { return lua.LNumber(height) })
	lines, _ := result.([]string)
	if err != nil {
		lines = nil
		c.plugin.log("canvas", c.id+":", err.Error())
		s.dialogs.ShowError(c.dialog, err.Error())
	}
	s.mu.Lock()
	c.lines, c.width, c.height = lines, width, height
	c.drawing, c.lastFrame = false, time.Now()
	s.mu.Unlock()
	s.tell()
}

// canvasText reads what draw gives: a string of lines, or a list of them.
func canvasText(result lua.LValue) (any, error) {
	switch result := result.(type) {
	case lua.LString:
		return strings.Split(string(result), "\n"), nil
	case *lua.LTable:
		var lines []string
		for i := 1; i <= result.Len(); i++ {
			lines = append(lines, lua.LVAsString(result.RawGetInt(i)))
		}
		return lines, nil
	case *lua.LNilType:
		return []string(nil), nil
	}
	return nil, fmt.Errorf("draw gives a string or a list of lines, not a %s", result.Type())
}

// redrawCanvas has a dialog's canvases drawn again: one, or all with id "".
func (s *surface) redrawCanvas(dialog int, id string) {
	s.mu.Lock()
	found := false
	for _, c := range s.canvases {
		if c.dialog == dialog && (id == "" || c.id == id) {
			c.dirty, found = true, true
		}
	}
	s.mu.Unlock()
	if found {
		s.tell()
	}
}

// tick asks the canvases with redraw whether to draw again, as often as
// their fps and the set's say, while their dialog is on screen; it ends
// once no canvas has redraw.
func (s *surface) tick() {
	for {
		s.mu.Lock()
		fps := s.fps
		var due []*canvas
		left := false
		for _, c := range s.canvases {
			if c.redraw == nil || c.redraw == lua.LNil || c.redraw == lua.LFalse {
				continue
			}
			left = true
			own := fps
			if c.fps > 0 {
				own = min(c.fps, fps)
			}
			if time.Since(c.lastFrame) >= time.Second/time.Duration(own) {
				due = append(due, c)
			}
		}
		if !left {
			s.ticking = false
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		changed := false
		for _, c := range due {
			if _, shown := s.dialogs.Bounds(c.dialog); shown && c.wantsRedraw() {
				s.mu.Lock()
				c.dirty, c.lastFrame = true, time.Now()
				s.mu.Unlock()
				changed = true
			}
		}
		if changed {
			s.tell()
		}
		time.Sleep(time.Second / time.Duration(fps))
	}
}

// wantsRedraw is a canvas's redraw: true always, a function when it says
// so within redrawAskLimit.
func (c *canvas) wantsRedraw() bool {
	ask, isFunction := c.redraw.(*lua.LFunction)
	if !isFunction {
		return lua.LVAsBool(c.redraw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), redrawAskLimit)
	defer cancel()
	result, err := c.plugin.callFunction(ctx, redrawAskLimit, ask, func(value lua.LValue) (any, error) {
		return lua.LVAsBool(value), nil
	})
	yes, _ := result.(bool)
	return err == nil && yes
}

// takeCanvases copies a Lua tree without the functions of its canvases,
// draw and redraw, which it keeps by the canvas's id; a redraw of true is
// kept too. Other functions in the tree are dropped.
func takeCanvases(state *lua.LState, value lua.LValue, specs *[]canvasSpec) lua.LValue {
	table, ok := value.(*lua.LTable)
	if !ok {
		return value
	}
	copied := state.NewTable()
	var spec canvasSpec
	table.ForEach(func(key, each lua.LValue) {
		name := lua.LVAsString(key)
		switch {
		case name == "draw":
			spec.draw, _ = each.(*lua.LFunction)
		case name == "redraw":
			spec.redraw = each
		case each.Type() == lua.LTFunction:
		default:
			copied.RawSet(key, takeCanvases(state, each, specs))
		}
	})
	if spec.draw != nil {
		spec.id = lua.LVAsString(table.RawGetString("id"))
		*specs = append(*specs, spec)
	}
	return copied
}

// matchCanvases checks that every canvas of a tree has its draw, and every
// draw a canvas with its id.
func matchCanvases(tree tools.UINode, specs []canvasSpec) error {
	for _, spec := range specs {
		if spec.id == "" {
			return fmt.Errorf("a canvas needs an id")
		}
		if _, ok := findCanvas(tree, spec.id); !ok {
			return fmt.Errorf("%s has a draw function but is no canvas", spec.id)
		}
	}
	var check func(n tools.UINode) error
	check = func(n tools.UINode) error {
		if n.Type == tools.UICanvas && !slicesContainsSpec(specs, n.ID) {
			return fmt.Errorf("canvas %s needs a draw function", n.ID)
		}
		for _, child := range n.Children {
			if err := check(child); err != nil {
				return err
			}
		}
		return nil
	}
	return check(tree)
}

func slicesContainsSpec(specs []canvasSpec, id string) bool {
	for _, spec := range specs {
		if spec.id == id {
			return true
		}
	}
	return false
}
