package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | BR-01 | Happy    | The context bar draws in the style picked, two steps a cell where it has them, shapes by default | P1 |
// | BR-02 | Happy    | Every style the settings offer is one the config takes, its sample ten cells | P2       |

import (
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
)

func TestBars(t *testing.T) {
	t.Run("BR-01 the context bar draws in the style picked, two steps a cell where it has them, shapes by default", func(t *testing.T) {
		for style, want := range map[string]string{
			"":             "▰▰▰▱▱",
			"shapes":       "▰▰▰▱▱",
			"braille":      "⣿⣿⣏⣉⣉",
			"half-circles": "●●◐○○",
			"circles":      "●●●○○",
			"half-squares": "■■◧□□",
			"line":         "━━━━━",
			"unknown":      "▰▰▰▱▱",
		} {
			if got := ansi.Strip(contextBar(0.5, 5, nil, nil, style)); got != want {
				t.Errorf("%q: %q, want %q", style, got, want)
			}
		}
		if got := ansi.Strip(contextBar(0.01, 5, nil, nil, "braille")); got != "⣏⣉⣉⣉⣉" {
			t.Errorf("a little used should show: %q", got)
		}
	})

	t.Run("BR-02 every style the settings offer is one the config takes, its sample ten cells", func(t *testing.T) {
		var names []string
		for _, style := range barStyles {
			names = append(names, style.name)
			if ansi.StringWidth(style.sample()) != 10 {
				t.Errorf("%s: sample %q", style.name, style.sample())
			}
		}
		if !slices.Equal(names, config.BarStyles) || config.Defaults().Render.Bar != barStyles[0].name {
			t.Errorf("styles %v, config %v", names, config.BarStyles)
		}
	})
}
