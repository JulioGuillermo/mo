package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | UC-01 | Happy    | A canvas shows the lines its draw gave, cut and filled to its room, "full" taking the dialog | P0 |
// | UC-02 | Happy    | With the focus, every key goes to the canvas, tab and enter too, and it draws again | P0 |
// | UC-03 | Happy    | Esc closes the dialog unless the canvas keeps it; ctrl+c always closes     | P0       |
// | UC-04 | Happy    | A click on the canvas tells its cell; the wheel over it goes to it, not to the scroll | P0 |
// | UC-05 | Happy    | The keys line says keys go to the focus, and ctrl+c closes when esc is kept | P2       |
// | UC-06 | Happy    | Settings › Interface › Plugin UIs sets ui_fps, 1 to the most, 20 unless changed | P1   |

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/tools"
)

const keyCanvas = `
local heard = "none"
mo.command{ name = "open", run = function()
  mo.dialog{ title = "Pad", size = "full", frame = false, tree = { type = "canvas", id = "pad", width = "full", height = "full", keep_esc = KEEP,
    draw = function(w, h) return { "heard " .. heard, string.rep("x", w + 10) } end },
    on_event = function(e)
      heard = e.event .. " " .. (e.value or "") .. " " .. tostring(e.x) .. "," .. tostring(e.y)
    end }
end }`

// drawnSoon draws the dialog until its screen holds want.
func drawnSoon(t *testing.T, m model, want string) string {
	t.Helper()
	var screen string
	for range 100 {
		frame, _ := m.uiFrame()
		screen = ansi.Strip(strings.Join(frame.lines, "\n"))
		if strings.Contains(screen, want) {
			return screen
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never drawn %q:\n%s", want, screen)
	return screen
}

func TestUICanvas(t *testing.T) {
	t.Run("UC-01 a canvas shows the lines its draw gave, cut and filled to its room, \"full\" taking the dialog", func(t *testing.T) {
		m := openedDialog(t, strings.Replace(keyCanvas, "KEEP", "false", 1))

		screen := drawnSoon(t, m, "heard none")

		lines := strings.Split(screen, "\n")
		if len(lines) != m.height || ansi.StringWidth(lines[1]) != m.width || strings.Count(lines[1], "x") != m.width || strings.TrimSpace(lines[2]) != "" {
			t.Errorf("screen:\n%s", screen)
		}
	})

	t.Run("UC-02 with the focus, every key goes to the canvas, tab and enter too, and it draws again", func(t *testing.T) {
		m := openedDialog(t, strings.Replace(keyCanvas, "KEEP", "false", 1))
		drawnSoon(t, m, "heard none")

		m = pressed(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
		drawnSoon(t, m, "heard key tab nil,nil")
		m = pressed(t, m, keyEnter)
		drawnSoon(t, m, "heard key enter")
		m = pressed(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
		drawnSoon(t, m, "heard key j")

		if m.uiOpen == 0 {
			t.Error("a key closed the dialog")
		}
	})

	t.Run("UC-03 esc closes the dialog unless the canvas keeps it; ctrl+c always closes", func(t *testing.T) {
		plain := openedDialog(t, strings.Replace(keyCanvas, "KEEP", "false", 1))
		kept := openedDialog(t, strings.Replace(keyCanvas, "KEEP", "true", 1))

		plain = pressed(t, plain, keyEsc)
		kept = pressed(t, kept, keyEsc)
		drawnSoon(t, kept, "heard key esc")
		stillOpen := kept.uiOpen != 0
		kept = pressed(t, kept, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

		if plain.uiOpen != 0 || !stillOpen || kept.uiOpen != 0 {
			t.Errorf("plain open = %v kept open after esc = %v after ctrl+c = %v", plain.uiOpen != 0, stillOpen, kept.uiOpen != 0)
		}
	})

	t.Run("UC-04 a click on the canvas tells its cell; the wheel over it goes to it, not to the scroll", func(t *testing.T) {
		m := openedDialog(t, strings.Replace(keyCanvas, "KEEP", "false", 1))
		drawnSoon(t, m, "heard none")

		updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 7, Y: 3, Button: tea.MouseLeft}))
		m = updated.(model)
		drawnSoon(t, m, "heard click  7,3")
		updated, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseWheelDown}))
		m = updated.(model)
		drawnSoon(t, m, "heard wheel down 2,5")

		if m.live(m.uiOpen).scroll != 0 {
			t.Error("the wheel scrolled the dialog")
		}
	})

	t.Run("UC-05 the keys line says keys go to the focus, and ctrl+c closes when esc is kept", func(t *testing.T) {
		kept, _ := uiKeys(tools.UINode{Type: tools.UICanvas, ID: "c", KeepEsc: true}, true)
		plain, _ := uiKeys(tools.UINode{Type: tools.UICanvas, ID: "c"}, true)

		if !strings.Contains(kept, "keys go to what has the focus") || !strings.Contains(kept, "ctrl+c closes") || !strings.Contains(plain, "esc closes") {
			t.Errorf("kept = %q plain = %q", kept, plain)
		}
	})

	t.Run("UC-06 Settings › Interface › Plugin UIs sets ui_fps, 1 to the most, 20 unless changed", func(t *testing.T) {
		draft := newSettingsDraft(config.Config{Provider: "llama-server"})
		untouched, _ := draft.applyTo(config.Config{Provider: "llama-server"}, nil)
		draft.uiFPS = "60"
		changed, err := draft.applyTo(config.Config{Provider: "llama-server"}, nil)
		draft.uiFPS = "500"
		_, tooMany := draft.applyTo(config.Config{Provider: "llama-server"}, nil)

		if untouched.Render.UIFPS != 0 || err != nil || changed.Render.UIFPS != 60 || tooMany == nil || config.Defaults().Render.UIFPS != 20 {
			t.Errorf("untouched = %d changed = %d err = %v tooMany = %v", untouched.Render.UIFPS, changed.Render.UIFPS, err, tooMany)
		}
		if _, ok := sectionByKey("pluginuis"); !ok {
			t.Error("no Plugin UIs section")
		}
	})
}
