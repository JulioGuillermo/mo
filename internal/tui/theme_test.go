package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | TH-01 | Happy    | A set background is painted, with light text on a dark one and dark on a light one | P0 |
// | TH-02 | Happy    | "terminal" keeps the terminal's background and its darkness          | P1       |
// | TH-03 | Happy    | Inline code is on blue, not grey                                     | P2       |
// | TH-04 | Error    | A background that is not #rrggbb, theme or terminal is refused       | P2       |

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/config"
)

func TestTheme(t *testing.T) {
	t.Run("TH-01 a set background is painted, with light text on a dark one and dark on a light one", func(t *testing.T) {
		m := conversationModel()
		m.session.Config.Render.Background = "#00081a"
		dark := m.View()
		m.session.Config.Render.Background = "#f5f5f5"
		light := screenFor(config.Render{Background: "#f5f5f5"}, true)

		if dark.BackgroundColor == nil || !screenFor(config.Render{Background: "#00081a"}, false).dark || light.dark || luminance(light.foreground) > 0.5 {
			t.Errorf("dark view bg = %v light = %+v", dark.BackgroundColor, light)
		}
	})

	t.Run("TH-02 terminal keeps the terminal's background and its darkness", func(t *testing.T) {
		m := conversationModel()
		m.session.Config.Render.Background = "terminal"

		_ = m.applyScreen(false)

		if m.View().BackgroundColor != nil || m.printer.isDark() {
			t.Error("the terminal's background or darkness was not kept")
		}
	})

	t.Run("TH-03 inline code is on blue, not grey", func(t *testing.T) {
		style := markdownStyle(true)
		background := lipgloss.Color(*style.Code.BackgroundColor)
		r, g, b, _ := background.RGBA()

		if b <= r || b <= g {
			t.Errorf("code background = %s", *style.Code.BackgroundColor)
		}
	})

	t.Run("TH-04 a background that is not #rrggbb, theme or terminal is refused", func(t *testing.T) {
		if config.ValidBackground("blue") || config.ValidBackground("#12345") || !config.ValidBackground(" #00081A ") || !config.ValidBackground("terminal") || !config.ValidBackground("theme") {
			t.Error("background validation is wrong")
		}
	})
}
