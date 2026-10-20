package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | PD-01 | Happy    | Esc closes a plugin's dialog not listed for good, off the screen's goroutine, and the plugin hears close | P0 |
// | PD-02 | Happy    | A listed dialog shows in the side panel; esc only hides it, to open again  | P0       |
// | PD-03 | Edge     | An unlisted dialog's own close button closes it for good too               | P1       |
// | PD-04 | Happy    | A full dialog fills the screen, its body as wide as the screen             | P0       |
// | PD-05 | Happy    | A textarea takes several lines, enter starting one, coloured as its language | P0     |
// | PD-06 | Edge     | A textarea's keys line says enter makes a new line; esc closes or hides as the dialog does | P2 |
// | PD-07 | Happy    | mo.view opens the file in the viewer, over the dialog, which keys no longer reach | P1  |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

// openedDialog runs the plugin command open of source and shows the dialog
// it opened.
func openedDialog(t *testing.T, source string) model {
	t.Helper()
	m := pluginModel(t, source)
	opened := make(chan int, 1)
	m.session.Plugins.Follow(func() {}, func(id int) { opened <- id })
	run, ok := m.session.Plugins.Command("open")
	if !ok {
		t.Fatalf("no command open: %+v", m.session.Plugins.Status())
	}
	if _, err := run(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(pluginDialogMsg(<-opened))
	return updated.(model)
}

const heardClose = `
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Form", listed = LISTED, tree = { type = "panel", children = {
    { type = "input", id = "name", value = "ana" },
    { type = "button", id = "cancel", label = "Cancel", on_click = "close" },
  } }, on_event = function(event)
    mo.sidebar.set{ id = "heard", text = "heard " .. event.event }
  end }
end }`

func pressed(t *testing.T, m model, keys ...tea.KeyPressMsg) model {
	t.Helper()
	for _, key := range keys {
		updated, _ := m.Update(key)
		m = updated.(model)
	}
	return m
}

func TestPluginDialogs(t *testing.T) {
	t.Run("PD-01 esc closes a plugin's dialog not listed for good, off the screen's goroutine, and the plugin hears close", func(t *testing.T) {
		m := openedDialog(t, strings.Replace(heardClose, "LISTED", "false", 1))
		id := m.uiOpen

		updated, cmd := m.Update(keyEsc)
		m = updated.(model)
		_, openBefore := m.session.Plugins.Dialogs().Get(id)
		cmd()

		_, open := m.session.Plugins.Dialogs().Get(id)
		if m.uiOpen != 0 || !openBefore || open || strings.Contains(sidebarText(m), "Form") {
			t.Errorf("open = %v side panel:\n%s", open, sidebarText(m))
		}
		eventuallyShown(t, m, "heard close")
	})

	t.Run("PD-02 a listed dialog shows in the side panel; esc only hides it, to open again", func(t *testing.T) {
		m := openedDialog(t, strings.Replace(heardClose, "LISTED", "true", 1))
		id := m.uiOpen

		m = pressed(t, m, keyEsc)

		_, open := m.session.Plugins.Dialogs().Get(id)
		if m.uiOpen != 0 || !open || !strings.Contains(sidebarText(m), "▣ Form") {
			t.Errorf("open = %v side panel:\n%s", open, sidebarText(m))
		}
		reopened, _ := m.openPluginDialog(id)
		if reopened.(model).uiOpen != id {
			t.Error("the hidden dialog does not open again")
		}
	})

	t.Run("PD-03 an unlisted dialog's own close button closes it for good too", func(t *testing.T) {
		m := openedDialog(t, strings.Replace(heardClose, "LISTED", "false", 1))
		id := m.uiOpen

		updated, cmd := m.uiFire(id, "cancel", "click")
		deliver(updated.(model), cmd, 2)

		if _, open := updated.(model).session.Plugins.Dialogs().Get(id); open || updated.(model).uiOpen != 0 {
			t.Error("the dialog is still there, hidden for good")
		}
	})

	t.Run("PD-04 a full dialog fills the screen, its body as wide as the screen", func(t *testing.T) {
		m := openedDialog(t, `
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Editor", size = "full", tree = { type = "textarea", id = "body" } }
end }`)

		frame, ok := m.uiFrame()

		if !ok || len(frame.lines) != m.height || frame.x != 0 || frame.y != 0 {
			t.Fatalf("lines = %d of %d at %d,%d", len(frame.lines), m.height, frame.x, frame.y)
		}
		for i, line := range frame.lines {
			if width := ansi.StringWidth(line); width != m.width {
				t.Fatalf("line %d is %d wide, not %d", i, width, m.width)
			}
		}
	})

	t.Run("PD-05 a textarea takes several lines, enter starting one, coloured as its language", func(t *testing.T) {
		m := openedDialog(t, `
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Code", tree = { type = "textarea", id = "body", language = "go", height = 4 } }
end }`)
		keys := []tea.KeyPressMsg{}
		for _, char := range "func a" {
			keys = append(keys, tea.KeyPressMsg{Code: char, Text: string(char)})
		}
		keys = append(keys, keyEnter, tea.KeyPressMsg{Code: 'x', Text: "x"})

		m = pressed(t, m, keys...)

		ui, _ := m.session.Plugins.Dialogs().Get(m.uiOpen)
		frame, _ := m.uiFrame()
		drawn := strings.Join(frame.lines, "\n")
		if ui.Values["body"] != "func a\nx" || m.uiOpen == 0 {
			t.Fatalf("value = %q", ui.Values["body"])
		}
		plain := ansi.Strip(drawn)
		if !strings.Contains(plain, "func a") || !strings.Contains(plain, "x"+uiCursor) {
			t.Errorf("drawn:\n%s", plain)
		}
		keyword := highlightLines("func", "go", 10, uiColor("surface"), true)[0]
		if !strings.Contains(drawn, strings.TrimSuffix(keyword, "\x1b[m")) {
			t.Errorf("func is not coloured as Go:\n%q", drawn)
		}
	})

	t.Run("PD-06 a textarea's keys line says enter makes a new line; esc closes or hides as the dialog does", func(t *testing.T) {
		tree := tools.UINode{Type: tools.UITextarea, ID: "body"}

		closes, _ := uiKeys(tree, true)
		hides, _ := uiKeys(tree, false)

		if !strings.Contains(closes, "enter new line") || !strings.Contains(closes, "esc closes") || !strings.Contains(hides, "esc hides") {
			t.Errorf("closes = %q hides = %q", closes, hides)
		}
	})

	t.Run("PD-07 mo.view opens the file in the viewer, over the dialog, which keys no longer reach", func(t *testing.T) {
		m := openedDialog(t, `
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Form", listed = true, tree = { type = "input", id = "name" } }
end }`)
		path := filepath.Join(t.TempDir(), "notes.txt")
		if err := os.WriteFile(path, []byte("today's notes"), 0o600); err != nil {
			t.Fatal(err)
		}

		updated, _ := m.Update(pluginViewMsg(path))
		m = pressed(t, updated.(model), tea.KeyPressMsg{Code: 'z', Text: "z"})

		ui, _ := m.session.Plugins.Dialogs().Get(m.uiOpen)
		if m.viewer == nil || m.viewer.file == "" || ui.Values["name"] != "" {
			t.Errorf("viewer = %v typed into the dialog = %q", m.viewer != nil, ui.Values["name"])
		}
	})
}

func eventuallyShown(t *testing.T, m model, text string) {
	t.Helper()
	for range 100 {
		if strings.Contains(sidebarText(m), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("never shown %q:\n%s", text, sidebarText(m))
}
