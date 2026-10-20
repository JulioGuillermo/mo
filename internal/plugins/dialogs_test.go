package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | DG-01 | Happy    | mo.dialog takes size = "full", listed and frame = false; unlisted and framed are the default | P0 |
// | DG-02 | Error    | A size other than full fails mo.dialog                                   | P2       |
// | DG-03 | Happy    | A dialog dismissed sends on_event a close event with every value, then its handler is dropped | P0 |
// | DG-04 | Happy    | A component's nodes become what its render gives, with its fields and children as props, nested ones too | P0 |
// | DG-05 | Error    | A component named as a node type of mo's, declared twice or after loading fails | P1 |
// | DG-06 | Error    | A component that renders itself forever, or fails, fails mo.dialog with its name | P1 |
// | DG-07 | Happy    | mo.view hands the path, resolved in the working directory, to who shows it; false with no one | P1 |
// | DG-08 | Happy    | A textarea with a language is a node a dialog takes                      | P2       |
// | DG-09 | Edge     | mo.screen is nil until mo tells the size; bounds is nil while the dialog is not on screen | P1 |

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jg/mo/internal/tools"
)

func runCommand(t *testing.T, set *Set, name string) (string, error) {
	t.Helper()
	run, ok := set.Command(name)
	if !ok {
		t.Fatalf("no command %s", name)
	}
	return run(context.Background(), "")
}

func TestDialogs(t *testing.T) {
	t.Run("DG-01 mo.dialog takes size = \"full\", listed and frame = false; unlisted and framed are the default", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Big", size = "full", listed = true, frame = false, tree = { type = "label", text = "hi" } }
  mo.dialog{ title = "Plain", tree = { type = "label", text = "hi" } }
end }`}).set

		_, err := runCommand(t, set, "open")

		big, _ := set.Dialogs().Get(1001)
		plain, _ := set.Dialogs().Get(1002)
		if err != nil || big.Size != tools.DialogFull || !big.Listed || !big.Bare || plain.Size != "" || plain.Listed || plain.Bare {
			t.Errorf("err = %v big = %+v plain = %+v", err, big, plain)
		}
	})

	t.Run("DG-02 a size other than full fails mo.dialog", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "open", run = function() mo.dialog{ size = "huge", tree = { type = "label", text = "hi" } } end }`}).set

		_, err := runCommand(t, set, "open")

		if err == nil || !strings.Contains(err.Error(), `size "huge"`) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("DG-03 a dialog dismissed sends on_event a close event with every value, then its handler is dropped", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "input", id = "name", value = "ana" }, on_event = function(event)
    mo.sidebar.set{ id = "heard", text = event.event .. " " .. event.action .. " " .. event.values.name }
  end }
end }`}).set
		_, _ = runCommand(t, set, "open")

		set.Dialogs().Dismiss(1001)

		eventually(t, "the plugin hears the close", func() bool {
			items := set.SidebarItems()
			return len(items) == 1 && items[0].Text == "close close ana"
		})
		_, open := set.Dialogs().Get(1001)
		eventually(t, "the handler goes", func() bool {
			set.surface.mu.Lock()
			defer set.surface.mu.Unlock()
			return len(set.surface.handlers) == 0
		})
		if open {
			t.Error("the dialog is still open")
		}
	})

	t.Run("DG-04 a component's nodes become what its render gives, with its fields and children as props, nested ones too", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.component{ name = "labelled", render = function(props)
  return { type = "panel", direction = "row", children = {
    { type = "label", text = props.label },
    { type = "input", id = props.id, value = props.value },
  } }
end }
mo.component{ name = "issue_card", render = function(props)
  return { type = "card", title = props.title, children = props.children }
end }
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "issue_card", title = "Issue", children = {
    { type = "labelled", id = "summary", label = "Summary", value = "fails" },
  } } }
end }`}).set

		_, err := runCommand(t, set, "open")

		dialog, _ := set.Dialogs().Get(1001)
		card := dialog.Tree
		if err != nil || card.Type != tools.UICard || card.Title != "Issue" || len(card.Children) != 1 {
			t.Fatalf("err = %v tree = %+v", err, card)
		}
		row := card.Children[0]
		if row.Direction != "row" || row.Children[0].Text != "Summary" || row.Children[1].ID != "summary" || row.Children[1].Value != "fails" {
			t.Errorf("row = %+v", row)
		}
	})

	t.Run("DG-05 a component named as a node type of mo's, declared twice or after loading fails", func(t *testing.T) {
		set := loadSource(t, map[string]string{
			"typed": `mo.component{ name = "button", render = function() end }`,
			"alias": `mo.component{ name = "dropdown", render = function() end }`,
			"twice": `mo.component{ name = "kv", render = function() end } mo.component{ name = "kv", render = function() end }`,
			"late":  `mo.command{ name = "late", run = function() mo.component{ name = "kv", render = function() end } end }`,
		}).set

		_, lateErr := runCommand(t, set, "late")

		for _, status := range set.Status() {
			if status.Name != "late" && status.Err == nil {
				t.Errorf("plugin %s loaded", status.Name)
			}
		}
		if lateErr == nil || !strings.Contains(lateErr.Error(), "only be called while the plugin loads") {
			t.Errorf("late err = %v", lateErr)
		}
	})

	t.Run("DG-06 a component that renders itself forever, or fails, fails mo.dialog with its name", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.component{ name = "loop", render = function(props) return { type = "loop" } end }
mo.component{ name = "broken", render = function() error("no data", 0) end }
mo.command{ name = "loop", run = function() mo.dialog{ tree = { type = "loop" } } end }
mo.command{ name = "broken", run = function() mo.dialog{ tree = { type = "panel", children = { { type = "broken" } } } } end }`}).set

		_, loopErr := runCommand(t, set, "loop")
		_, brokenErr := runCommand(t, set, "broken")

		if loopErr == nil || !strings.Contains(loopErr.Error(), "component loop: components nested deeper") {
			t.Errorf("loop err = %v", loopErr)
		}
		if brokenErr == nil || !strings.Contains(brokenErr.Error(), "component broken: no data") {
			t.Errorf("broken err = %v", brokenErr)
		}
	})

	t.Run("DG-07 mo.view hands the path, resolved in the working directory, to who shows it; false with no one", func(t *testing.T) {
		f := loadSource(t, map[string]string{"demo": `
mo.tool{ name = "show", run = function() return tostring(mo.view("notes/today.md")) end }`})
		adapted := f.set.Tools(nil)[0]

		alone, _ := adapted.Run(context.Background(), tools.Env{}, json.RawMessage(`{}`))
		var shown atomic.Value
		f.set.OnView(func(path string) { shown.Store(path) })
		told, _ := adapted.Run(context.Background(), tools.Env{}, json.RawMessage(`{}`))

		if alone != "false" || told != "true" || shown.Load() != filepath.Join(f.workDir, "notes/today.md") {
			t.Errorf("alone = %q told = %q shown = %v", alone, told, shown.Load())
		}
	})

	t.Run("DG-08 a textarea with a language is a node a dialog takes", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "open", run = function()
  mo.dialog{ tree = { type = "textarea", id = "body", language = "lua", height = 4, value = "print(1)" } }
end }`}).set

		_, err := runCommand(t, set, "open")

		dialog, _ := set.Dialogs().Get(1001)
		if err != nil || dialog.Tree.Type != tools.UITextarea || dialog.Tree.Language != "lua" || dialog.Tree.Height != 4 {
			t.Errorf("err = %v tree = %+v", err, dialog.Tree)
		}
	})

	t.Run("DG-09 mo.screen is nil until mo tells the size; bounds is nil while the dialog is not on screen", func(t *testing.T) {
		set := loadSource(t, map[string]string{"demo": `
mo.command{ name = "open", run = function()
  local screen = mo.screen()
  local bounds = mo.dialog{ tree = { type = "label", text = "hi" } }.bounds()
  return tostring(screen and screen.width) .. " " .. tostring(bounds)
end }`}).set

		before, _ := runCommand(t, set, "open")
		set.SetScreen(100, 40)
		after, _ := runCommand(t, set, "open")

		if before != "nil nil" || after != "100 nil" {
			t.Errorf("before = %q after = %q", before, after)
		}
	})
}
