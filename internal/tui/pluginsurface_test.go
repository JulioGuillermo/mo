package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PU-01 | Happy    | The side panel shows a Plugins section with each item, its detail and the dialogs open | P0 |
// | PU-02 | Happy    | A click on an item runs it and shows what it said                         | P0       |
// | PU-03 | Happy    | A dialog a plugin opens shows at once with the plugin's name, and a click in it reaches the plugin | P0 |
// | PU-04 | Edge     | Without plugins there is no Plugins section and nothing breaks            | P2       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/tools"
)

func pluginModel(t *testing.T, source string) model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.lua"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := plugins.Load(dir, t.TempDir(), t.TempDir(), nil)
	t.Cleanup(loaded.Close)
	m := conversationModel()
	m.session.Plugins = loaded
	m.width, m.height = 160, 40
	return m
}

func sidebarText(m model) string {
	lines, _ := m.sidebarLines()
	return ansi.Strip(strings.Join(lines, "\n"))
}

func clickSidebarItem(t *testing.T, m model, item string) model {
	t.Helper()
	_, regions := m.sidebarLines()
	for _, each := range regions {
		if each.item == item {
			updated, cmd := each.do(m)
			return deliver(updated.(model), cmd, 2)
		}
	}
	t.Fatalf("no item %q in the side panel", item)
	return m
}

func TestPluginSurface(t *testing.T) {
	t.Run("PU-01 the side panel shows a Plugins section with each item, its detail and the dialogs open", func(t *testing.T) {
		m := pluginModel(t, `
mo.sidebar.set{ id = "jira", text = "Jira · logged in", detail = "12 issues", color = "success", icon = "●" }
mo.sidebar.set{ id = "plain", text = "a label" }`)
		m.session.Plugins.Dialogs().Open("demo", "Board", mustTree(t))

		text := sidebarText(m)

		for _, want := range []string{"Plugins", "● Jira · logged in", "  12 issues", "• a label", "▣ Board  demo"} {
			if !strings.Contains(text, want) {
				t.Errorf("no %q in:\n%s", want, text)
			}
		}
	})

	t.Run("PU-02 a click on an item runs it and shows what it said", func(t *testing.T) {
		m := pluginModel(t, `mo.sidebar.set{ id = "hi", text = "say hi", on_click = function() return "hello from the plugin" end }`)

		m = clickSidebarItem(t, m, "plugin demo/hi")

		if !strings.Contains(shown(m), "hello from the plugin") {
			t.Errorf("shown:\n%s", shown(m))
		}
	})

	t.Run("PU-03 a dialog a plugin opens shows at once with the plugin's name, and a click in it reaches the plugin", func(t *testing.T) {
		m := pluginModel(t, `
mo.command{ name = "board", run = function()
  mo.dialog{ title = "Board", tree = { type = "button", id = "ok", label = "OK" }, on_event = function(event)
    mo.sidebar.set{ id = "last", text = "pressed " .. event.element }
  end }
end }`)
		run, _ := m.session.Plugins.Command("board")
		opened := make(chan int, 1)
		m.session.Plugins.Follow(func() {}, func(id int) { opened <- id })
		run(t.Context(), "")
		updated, _ := m.Update(pluginDialogMsg(<-opened))
		m = updated.(model)

		frame, ok := m.uiFrame()
		updated, _ = m.uiFire(m.uiOpen, "ok", "click")
		m = updated.(model)

		if !ok || !strings.Contains(ansi.Strip(frame.lines[0]), "Board") || !strings.Contains(ansi.Strip(frame.lines[0]), "demo ·") {
			t.Fatalf("frame:\n%s", ansi.Strip(strings.Join(frame.lines, "\n")))
		}
		for range 100 {
			if strings.Contains(sidebarText(m), "pressed ok") {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Errorf("the click did not reach the plugin:\n%s", sidebarText(m))
	})

	t.Run("PU-04 without plugins there is no Plugins section and nothing breaks", func(t *testing.T) {
		m := conversationModel()
		m.session.Plugins = nil

		updated, _ := m.Update(pluginDialogMsg(1001))

		if strings.Contains(sidebarText(m), "Plugins") || updated.(model).uiOpen != 0 {
			t.Errorf("side panel:\n%s", sidebarText(m))
		}
	})
}

func mustTree(t *testing.T) tools.UINode {
	t.Helper()
	tree, err := tools.PluginTree(map[string]any{"type": "label", "text": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	return *tree
}
