package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SF-01 | Happy    | mo.sidebar.set adds items in order, the same id replaces, remove takes it away, each change told | P0 |
// | SF-02 | Happy    | A click on an item runs its on_click and gives its text; one without on_click is not clickable | P0 |
// | SF-03 | Happy    | mo.dialog opens a dialog numbered apart; the user's events reach on_event with every value, which can update and close it | P0 |
// | SF-04 | Error    | A tree the dialog cannot draw fails mo.dialog; an on_event that fails shows under the dialog | P1 |

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jg/mo/internal/tools"
)

func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	for range 100 {
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func TestSurface(t *testing.T) {
	ctx := context.Background()

	t.Run("SF-01 mo.sidebar.set adds items in order, the same id replaces, remove takes it away, each change told", func(t *testing.T) {
		set := loadSource(t, map[string]string{"panel": `
mo.sidebar.set{ id = "a", text = "first", color = "success", icon = "●" }
mo.sidebar.set{ id = "b", text = "second", detail = "more" }
mo.tool{ name = "go", run = function()
  mo.sidebar.set{ id = "a", text = "first again" }
  mo.sidebar.remove("b")
end }`}).set
		var told atomic.Int32
		set.Follow(func() { told.Add(1) }, nil)
		before := set.SidebarItems()

		set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		after := set.SidebarItems()
		if len(before) != 2 || before[0].Text != "first" || before[0].Color != "success" || before[0].Icon != "●" || before[1].Detail != "more" || before[0].Key != "panel/a" {
			t.Errorf("before = %+v", before)
		}
		if len(after) != 1 || after[0].Text != "first again" || told.Load() != 2 {
			t.Errorf("after = %+v told %d", after, told.Load())
		}
	})

	t.Run("SF-02 a click on an item runs its on_click and gives its text; one without on_click is not clickable", func(t *testing.T) {
		set := loadSource(t, map[string]string{"panel": `
local count = 0
mo.sidebar.set{ id = "counter", text = "clicks", on_click = function() count = count + 1 return "clicked " .. count end }
mo.sidebar.set{ id = "label", text = "just a label" }`}).set

		first, firstErr := set.ClickSidebar(ctx, "panel/counter")
		second, _ := set.ClickSidebar(ctx, "panel/counter")
		none, noneErr := set.ClickSidebar(ctx, "panel/label")

		items := set.SidebarItems()
		if first != "clicked 1" || second != "clicked 2" || firstErr != nil || none != "" || noneErr != nil || !items[0].Clickable || items[1].Clickable {
			t.Errorf("first = %q second = %q none = %q items = %+v", first, second, none, items)
		}
	})

	t.Run("SF-03 mo.dialog opens a dialog numbered apart; the user's events reach on_event with every value, which can update and close it", func(t *testing.T) {
		set := loadSource(t, map[string]string{"form": `
mo.command{ name = "ask", run = function()
  local dialog
  dialog = mo.dialog{
    title = "New issue",
    tree = { type = "card", title = "New issue", children = {
      { type = "input", id = "summary", value = "" },
      { type = "button", id = "save", label = "Save" },
      { type = "button", id = "done", label = "Done" },
    } },
    on_event = function(event)
      if event.element == "save" then
        dialog.update{ title = "Saved: " .. event.values.summary }
      elseif event.element == "done" then
        dialog.close()
      end
    end,
  }
  return tostring(dialog.id)
end }`}).set
		var opened atomic.Int32
		set.Follow(func() {}, func(id int) { opened.Store(int32(id)) })
		run, _ := set.Command("ask")

		id, err := run(ctx, "")

		dialogs := set.Dialogs()
		number := int(opened.Load())
		if err != nil || id != "1001" || number != 1001 {
			t.Fatalf("id = %q opened %d err = %v", id, number, err)
		}
		dialog, _ := dialogs.Get(number)
		if dialog.Owner != "form" || dialog.Title != "New issue" {
			t.Errorf("dialog = %+v", dialog)
		}
		dialogs.SetValue(number, "summary", "login fails")
		dialogs.Fire(number, "save", "click")
		eventually(t, "the title follows the save", func() bool { got, _ := dialogs.Get(number); return got.Title == "Saved: login fails" })
		dialogs.Fire(number, "done", "click")
		eventually(t, "the dialog closes", func() bool { _, open := dialogs.Get(number); return !open })
	})

	t.Run("SF-04 a tree the dialog cannot draw fails mo.dialog; an on_event that fails shows under the dialog", func(t *testing.T) {
		set := loadSource(t, map[string]string{"bad": `
mo.tool{ name = "broken_tree", run = function() mo.dialog{ tree = { type = "button" } } end }
mo.tool{ name = "failing_handler", run = function()
  return mo.dialog{ tree = { type = "button", id = "go", label = "Go" }, on_event = function() error("could not save", 0) end }.id
end }`}).set
		adapted := set.Tools(nil)

		_, treeErr := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		id, _ := adapted[1].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		set.Dialogs().Fire(1001, "go", "click")

		if treeErr == nil || !strings.Contains(treeErr.Error(), "mo.dialog") || id != "1001" {
			t.Fatalf("treeErr = %v id = %q", treeErr, id)
		}
		eventually(t, "the error shows", func() bool {
			dialog, _ := set.Dialogs().Get(1001)
			return strings.Contains(dialog.Error, "could not save")
		})
	})
}
