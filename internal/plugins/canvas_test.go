package plugins

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | CV-01 | Happy    | A canvas's draw gets its size and gives its lines, a string or a list, drawn off the caller's goroutine | P0 |
// | CV-02 | Happy    | A new size draws it again and tells on_event resize; the same size does not | P0      |
// | CV-03 | Happy    | A key, click or wheel on the canvas reaches on_event with its key, text and cell, then draws it again | P0 |
// | CV-04 | Happy    | dialog.redraw() draws the canvas again                                     | P1       |
// | CV-05 | Happy    | redraw = true draws it again at the fps while shown; redraw() decides; none stays still | P0 |
// | CV-06 | Error    | A canvas without draw, or a draw without a canvas, fails mo.dialog          | P1       |
// | CV-07 | Error    | A draw that fails shows its error under the dialog                         | P1       |
// | CV-08 | Happy    | A component whose render gives { size, draw } is a canvas of the sizes size gives, which takes no focus unless asked | P0 |
// | CV-09 | Happy    | mo.style colours text and mo.width counts its cells without the colours     | P1       |
// | CV-10 | Edge     | Closing the dialog forgets its canvases                                    | P1       |

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

// openCanvas loads source, runs its command open, and gives the set and
// the dialog it opened.
func openCanvas(t *testing.T, source string) (*Set, int) {
	t.Helper()
	set := loadSource(t, map[string]string{"demo": source}).set
	if _, err := runCommand(t, set, "open"); err != nil {
		t.Fatal(err)
	}
	return set, 1001
}

// linesSoon asks for a canvas's lines until they are those wanted.
func linesSoon(t *testing.T, set *Set, dialog int, id string, width, height int, want string) {
	t.Helper()
	eventually(t, "the canvas draws "+want, func() bool {
		return strings.Join(set.Dialogs().CanvasLines(dialog, id, width, height), "|") == want
	})
}

const counterCanvas = `
local draws, heard = 0, ""
mo.command{ name = "open", run = function()
  dialog = mo.dialog{ tree = { type = "canvas", id = "c", draw = function(w, h)
    draws = draws + 1
    return { w .. "x" .. h, "draw " .. draws, heard }
  end }, on_event = function(e)
    heard = e.event .. ":" .. (e.value or "") .. ":" .. (e.text or "") .. ":" .. tostring(e.x) .. "," .. tostring(e.y)
  end }
end }
mo.command{ name = "again", run = function() dialog.redraw() end }`

func TestCanvas(t *testing.T) {
	t.Run("CV-01 a canvas's draw gets its size and gives its lines, a string or a list, drawn off the caller's goroutine", func(t *testing.T) {
		set, id := openCanvas(t, counterCanvas)
		text, textID := openCanvas(t, `
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "canvas", id = "t", draw = function(w, h) return "a\nb" end } }
end }`)

		first := set.Dialogs().CanvasLines(id, "c", 30, 5)

		if first != nil {
			t.Errorf("drawn at once: %q", first)
		}
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 1|")
		linesSoon(t, text, textID, "t", 10, 2, "a|b")
	})

	t.Run("CV-02 a new size draws it again and tells on_event resize; the same size does not", func(t *testing.T) {
		set, id := openCanvas(t, counterCanvas)
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 1|")

		set.Dialogs().CanvasLines(id, "c", 30, 5)
		time.Sleep(50 * time.Millisecond)
		same := set.Dialogs().CanvasLines(id, "c", 30, 5)

		if strings.Join(same, "|") != "30x5|draw 1|" {
			t.Errorf("same size drew again: %q", same)
		}
		linesSoon(t, set, id, "c", 40, 8, "40x8|draw 2|resize:40x8::nil,nil")
	})

	t.Run("CV-03 a key, click or wheel on the canvas reaches on_event with its key, text and cell, then draws it again", func(t *testing.T) {
		set, id := openCanvas(t, counterCanvas)
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 1|")

		set.Dialogs().Send(tools.UIEvent{UI: id, Element: "c", Event: "key", Value: "j", Text: "j"})
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 2|key:j:j:nil,nil")
		set.Dialogs().Send(tools.UIEvent{UI: id, Element: "c", Event: "click", X: 3, Y: 2, At: true})
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 3|click:::3,2")
		set.Dialogs().Send(tools.UIEvent{UI: id, Element: "c", Event: "wheel", Value: "up", X: 0, Y: 4, At: true})
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 4|wheel:up::0,4")
	})

	t.Run("CV-04 dialog.redraw() draws the canvas again", func(t *testing.T) {
		set, id := openCanvas(t, counterCanvas)
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 1|")

		_, err := runCommand(t, set, "again")

		if err != nil {
			t.Fatal(err)
		}
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 2|")
	})

	t.Run("CV-05 redraw = true draws it again at the fps while shown; redraw() decides; none stays still", func(t *testing.T) {
		set, id := openCanvas(t, `
local frames, asked, draws = 0, 0, 0
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "panel", children = {
    { type = "canvas", id = "always", fps = 50, redraw = true, draw = function() frames = frames + 1 return tostring(frames) end },
    { type = "canvas", id = "asked", redraw = function() asked = asked + 1 return asked == 2 end, draw = function() draws = draws + 1 return "draws " .. draws end },
    { type = "canvas", id = "still", draw = function() return "still" end },
  } } }
end }`)
		set.SetUIFPS(50)
		var told atomic.Int32
		set.Follow(func() { told.Add(1) }, nil)
		draw := func(name string) string {
			return strings.Join(set.Dialogs().CanvasLines(id, name, 10, 1), "")
		}
		linesSoon(t, set, id, "still", 10, 1, "still")
		linesSoon(t, set, id, "asked", 10, 1, "draws 1")

		before := told.Load()
		time.Sleep(200 * time.Millisecond)
		hidden := told.Load()
		set.Dialogs().SetShown(id, tools.UIBounds{Width: 10, Height: 3})

		eventually(t, "always keeps drawing", func() bool { draw("always"); return draw("always") > "3" })
		eventually(t, "asked draws when its redraw says so", func() bool { return draw("asked") == "draws 2" })
		time.Sleep(200 * time.Millisecond)
		if draw("asked") != "draws 2" {
			t.Errorf("asked drew again unasked: %q", draw("asked"))
		}
		if hidden != before {
			t.Errorf("a dialog off screen was drawn again %d times", hidden-before)
		}
		if draw("still") != "still" {
			t.Errorf("still = %q", draw("still"))
		}
	})

	t.Run("CV-06 a canvas without draw, or a draw without a canvas, fails mo.dialog", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "bare", run = function() mo.dialog{ tree = { type = "canvas", id = "c" } } end }
mo.command{ name = "nameless", run = function() mo.dialog{ tree = { type = "canvas", draw = function() end } } end }
mo.command{ name = "stray", run = function() mo.dialog{ tree = { type = "label", id = "l", text = "x", draw = function() end } } end }`}).set

		_, bare := runCommand(t, set, "bare")
		_, nameless := runCommand(t, set, "nameless")
		_, stray := runCommand(t, set, "stray")

		if bare == nil || !strings.Contains(bare.Error(), "canvas c needs a draw function") {
			t.Errorf("bare = %v", bare)
		}
		if nameless == nil || !strings.Contains(nameless.Error(), "id") {
			t.Errorf("nameless = %v", nameless)
		}
		if stray == nil || !strings.Contains(stray.Error(), "l has a draw function but is no canvas") {
			t.Errorf("stray = %v", stray)
		}
	})

	t.Run("CV-07 a draw that fails shows its error under the dialog", func(t *testing.T) {
		set, id := openCanvas(t, `
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "canvas", id = "c", draw = function() error("no buffer", 0) end } }
end }`)

		set.Dialogs().CanvasLines(id, "c", 10, 2)

		eventually(t, "the error shows", func() bool {
			ui, _ := set.Dialogs().Get(id)
			return strings.Contains(ui.Error, "no buffer")
		})
	})

	t.Run("CV-08 a component whose render gives { size, draw } is a canvas of the sizes size gives, which takes no focus unless asked", func(t *testing.T) {
		set, id := openCanvas(t, `
mo.component{ name = "meter", render = function(props)
  return {
    size = function(most_w, most_h) return { w = 20, h = 1, max_w = most_w } end,
    draw = function(w, h) return props.label .. " " .. w end,
  }
end }
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "panel", children = { { type = "meter", label = "cpu", grow = 1 } } } }
end }`)

		ui, _ := set.Dialogs().Get(id)

		meter := ui.Tree.Children[0]
		if meter.Type != tools.UICanvas || meter.ID != "meter-1" || meter.Width.Cells() != 20 || meter.Height.Cells() != 1 || meter.MaxWidth != 80 || meter.Grow != 1 || meter.Focusable() {
			t.Errorf("meter = %+v", meter)
		}
		linesSoon(t, set, id, "meter-1", 20, 1, "cpu 20")
	})

	t.Run("CV-09 mo.style colours text and mo.width counts its cells without the colours", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "open", run = function()
  local styled = mo.style("hi 世界", { fg = "accent", bold = true })
  return styled .. "|" .. mo.width(styled)
end }`}).set

		output, err := runCommand(t, set, "open")

		styled, width, _ := strings.Cut(output, "|")
		if err != nil || ansi.Strip(styled) != "hi 世界" || styled == "hi 世界" || width != "7" {
			t.Errorf("output = %q err = %v", output, err)
		}
	})

	t.Run("CV-10 closing the dialog forgets its canvases", func(t *testing.T) {
		set, id := openCanvas(t, counterCanvas)
		linesSoon(t, set, id, "c", 30, 5, "30x5|draw 1|")

		set.Dialogs().Dismiss(id)

		eventually(t, "the canvas goes", func() bool {
			set.surface.mu.Lock()
			defer set.surface.mu.Unlock()
			return len(set.surface.canvases) == 0
		})
		if lines := set.Dialogs().CanvasLines(id, "c", 30, 5); lines != nil {
			t.Errorf("lines = %q", lines)
		}
	})
}
