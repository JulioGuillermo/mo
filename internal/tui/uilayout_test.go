package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | UL-01 | Happy    | In a row, width "full" takes what the others leave; height "full" the room given | P0  |
// | UL-02 | Happy    | In a column with a height to give, grow shares the rows left, as flex-grow  | P0       |
// | UL-03 | Edge     | In a column with no height to give, grow and height "full" keep their own height | P1   |
// | UL-04 | Happy    | min_width and min_height widen and pad a node; max_width wraps it, max_height cuts it | P0 |
// | UL-05 | Happy    | At the root, "full" is the whole dialog: its width and the rows it shows    | P0       |
// | UL-06 | Edge     | A node taller than the dialog is never cut to it: it scrolls                | P1       |
// | UL-07 | Edge     | Hits below a node's cut go with the rows cut                                | P2       |
// | UL-08 | Happy    | A column beside others in a row of a set height gives that height to its children that grow | P0 |

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

func layoutTree(t *testing.T, tree string) tools.UINode {
	t.Helper()
	parsed, err := tools.ParseUITree(json.RawMessage(tree))
	if err != nil {
		t.Fatal(err)
	}
	return *parsed
}

// hitOf is where a node is drawn, by its id.
func hitOf(t *testing.T, box uiBox, id string) uiHit {
	t.Helper()
	for _, hit := range box.hits {
		if hit.id == id {
			return hit
		}
	}
	t.Fatalf("no %s drawn", id)
	return uiHit{}
}

func TestUILayout(t *testing.T) {
	t.Run("UL-01 in a row, width \"full\" takes what the others leave; height \"full\" the room given", func(t *testing.T) {
		tree := layoutTree(t, `{"type":"panel","direction":"row","height":"full","children":[
		  {"type":"panel","width":20,"children":[{"type":"button","id":"a","label":"A"}]},
		  {"type":"textarea","id":"body","width":"full","height":"full"}]}`)

		box := uiRenderer{}.render(tree, uiRoom{width: 80, height: 15}, "")

		body := hitOf(t, box, "body")
		if body.x0 != 20 || body.x1 != 80 || body.y1-body.y0 != 15 || box.height() != 15 || box.width != 80 {
			t.Errorf("body = %+v box %dx%d", body, box.width, box.height())
		}
	})

	t.Run("UL-02 in a column with a height to give, grow shares the rows left, as flex-grow", func(t *testing.T) {
		tree := layoutTree(t, `{"type":"panel","height":"full","children":[
		  {"type":"label","text":"title"},
		  {"type":"textarea","id":"one","grow":1},
		  {"type":"textarea","id":"two","grow":2},
		  {"type":"button","id":"ok","label":"OK"}]}`)

		box := uiRenderer{}.render(tree, uiRoom{width: 60, height: 20}, "")

		one, two, ok := hitOf(t, box, "one"), hitOf(t, box, "two"), hitOf(t, box, "ok")
		// 20 rows: 1 title, 1 button, two textareas of 6 each, 6 rows left shared 1:2.
		if rows := one.y1 - one.y0; rows != 8 {
			t.Errorf("one has %d rows", rows)
		}
		if rows := two.y1 - two.y0; rows != 10 {
			t.Errorf("two has %d rows", rows)
		}
		if ok.y0 != 19 || box.height() != 20 {
			t.Errorf("ok at %d, box %d rows", ok.y0, box.height())
		}
	})

	t.Run("UL-03 in a column with no height to give, grow and height \"full\" keep their own height", func(t *testing.T) {
		tree := layoutTree(t, `{"type":"panel","children":[
		  {"type":"textarea","id":"one","grow":1},
		  {"type":"textarea","id":"two","height":"full"}]}`)

		box := uiRenderer{}.render(tree, uiRoom{width: 60}, "")

		one, two := hitOf(t, box, "one"), hitOf(t, box, "two")
		if one.y1-one.y0 != uiTextareaHeight || two.y1-two.y0 != uiTextareaHeight {
			t.Errorf("one = %+v two = %+v", one, two)
		}
	})

	t.Run("UL-04 min_width and min_height widen and pad a node; max_width wraps it, max_height cuts it", func(t *testing.T) {
		small := layoutTree(t, `{"type":"label","text":"hi","min_width":10,"min_height":3}`)
		long := layoutTree(t, `{"type":"label","text":"one two three four five six","max_width":9,"max_height":2}`)

		smallBox := uiRenderer{}.render(small, uiRoom{width: 60}, "")
		longBox := uiRenderer{}.render(long, uiRoom{width: 60}, "")

		if smallBox.width != 10 || smallBox.height() != 3 {
			t.Errorf("small is %dx%d", smallBox.width, smallBox.height())
		}
		first := strings.TrimSpace(ansi.Strip(longBox.lines[0]))
		if longBox.width > 9 || longBox.height() != 2 || first != "one two" {
			t.Errorf("long is %dx%d: %q", longBox.width, longBox.height(), longBox.lines)
		}
	})

	t.Run("UL-05 at the root, \"full\" is the whole dialog: its width and the rows it shows", func(t *testing.T) {
		m := openedDialog(t, `
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Note", tree = { type = "textarea", id = "body", width = "full", height = "full" } }
end }`)

		frame, _ := m.uiFrame()

		body := frame.hits[1]
		if body.id != "body" || body.x1-body.x0 != uiDialogMaxWidth-4 || body.y1-body.y0 != m.height-4 {
			t.Errorf("body = %+v on a %dx%d screen", body, m.width, m.height)
		}
	})

	t.Run("UL-06 a node taller than the dialog is never cut to it: it scrolls", func(t *testing.T) {
		tree := layoutTree(t, `{"type":"textarea","id":"body","height":50}`)

		box := uiRenderer{}.render(tree, uiRoom{width: 60, height: 20}, "")

		if box.height() != 50 {
			t.Errorf("box has %d rows", box.height())
		}
	})

	t.Run("UL-07 hits below a node's cut go with the rows cut", func(t *testing.T) {
		tree := layoutTree(t, `{"type":"panel","max_height":2,"children":[
		  {"type":"button","id":"a","label":"A"},{"type":"button","id":"b","label":"B"},{"type":"button","id":"c","label":"C"}]}`)

		box := uiRenderer{}.render(tree, uiRoom{width: 60}, "")

		for _, hit := range box.hits {
			if hit.id == "c" {
				t.Errorf("c is clickable though cut: %+v", box.hits)
			}
		}
	})

	t.Run("UL-08 a column beside others in a row of a set height gives that height to its children that grow", func(t *testing.T) {
		tree := layoutTree(t, `{"type":"panel","direction":"row","height":"full","children":[
		  {"type":"card","title":"Files","width":20,"height":"full","children":[{"type":"label","text":"a.txt"}]},
		  {"type":"panel","width":"full","children":[
		    {"type":"input","id":"subject","width":"full"},
		    {"type":"textarea","id":"body","grow":1},
		    {"type":"button","id":"send","label":"Send"}]}]}`)

		box := uiRenderer{}.render(tree, uiRoom{width: 100, height: 24}, "")

		body, send := hitOf(t, box, "body"), hitOf(t, box, "send")
		if body.y1-body.y0 != 22 || send.y0 != 23 || box.height() != 24 {
			t.Errorf("body = %+v send = %+v rows %d", body, send, box.height())
		}
	})
}
