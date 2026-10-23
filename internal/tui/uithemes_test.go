package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | UT-01 | Happy    | The looks are the ones the config accepts; mo's own, dark, is the default and as before | P0 |
// | UT-02 | Happy    | Every look has every colour, its text light on dark grounds and dark on light ones | P0 |
// | UT-03 | Happy    | A look chosen colours the dialogs, the status line, the diffs and the accent | P0     |
// | UT-04 | Happy    | The theme's background follows the mode: dark, light, or auto as the terminal is; a colour of its own sets the darkness | P0 |
// | UT-05 | Happy    | Settings saves the look and the mode when not the default, and previews the look hovered | P1 |

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
)

func usingUI(t *testing.T, name string, dark bool) {
	t.Helper()
	before := currentUIState()
	useUITheme(name, dark)
	t.Cleanup(func() { useUITheme(before.theme.name, before.dark) })
}

func TestUIThemes(t *testing.T) {
	t.Run("UT-01 the looks are the ones the config accepts; mo's own, dark, is the default and as before", func(t *testing.T) {
		var names []string
		for _, theme := range uiThemes {
			names = append(names, theme.name)
		}
		defaults := config.Defaults().Render
		mo := uiThemeNamed("mo").dark

		if !slices.Equal(names, config.UIThemes) || defaults.Theme != "mo" || defaults.Mode != config.ModeDark || defaults.Background != config.BackgroundTheme {
			t.Errorf("themes %q default %q %q %q", names, defaults.Theme, defaults.Mode, defaults.Background)
		}
		if mo.background != "#00081a" || mo.panel != "#24283b" || mo.block != "#0e1630" || mo.accent != "#7aa2f7" || mo.statusBase != "#0b1230" {
			t.Errorf("mo's own changed: %+v", mo)
		}
	})

	t.Run("UT-02 every look has every colour, its text light on dark grounds and dark on light ones", func(t *testing.T) {
		for _, theme := range uiThemes {
			for _, dark := range []bool{true, false} {
				colors := theme.colors(dark)
				fields := reflect.ValueOf(colors)
				for i := range fields.NumField() {
					if value := fields.Field(i).String(); !strings.HasPrefix(value, "#") || len(value) != 7 {
						t.Errorf("%s dark=%v %s = %q", theme.name, dark, fields.Type().Field(i).Name, value)
					}
				}
				text, ground := luminance(col(colors.text)), luminance(col(colors.background))
				if dark != (text > ground) {
					t.Errorf("%s dark=%v text %.2f on %.2f", theme.name, dark, text, ground)
				}
			}
		}
	})

	t.Run("UT-03 a look chosen colours the dialogs, the status line, the diffs and the accent", func(t *testing.T) {
		usingUI(t, "gruvbox", true)
		gruvbox := uiThemeNamed("gruvbox").dark

		if dialogPalette(true).body != col(gruvbox.body) || statusPalette(true).base != col(gruvbox.statusBase) ||
			changePalette(true).added != col(gruvbox.added) || accent() != col(gruvbox.accent) {
			t.Error("gruvbox's colours are not the ones in use")
		}
	})

	t.Run("UT-04 the theme's background follows the mode: dark, light, or auto as the terminal is; a colour of its own sets the darkness", func(t *testing.T) {
		nord := uiThemeNamed("nord")
		dark := screenFor(config.Render{Background: "theme", Theme: "nord", Mode: "dark"}, false)
		light := screenFor(config.Render{Background: "theme", Theme: "nord", Mode: "light"}, true)
		auto := screenFor(config.Render{Background: "theme", Theme: "nord", Mode: "auto"}, false)
		terminal := screenFor(config.Render{Background: "terminal", Theme: "nord", Mode: "dark"}, false)
		own := screenFor(config.Render{Background: "#ffffff", Theme: "nord", Mode: "dark"}, true)

		if !dark.dark || dark.background != col(nord.dark.background) || light.dark || light.background != col(nord.light.background) {
			t.Errorf("dark %+v light %+v", dark, light)
		}
		if auto.dark || auto.background != col(nord.light.background) || terminal.set || terminal.dark || own.dark || own.foreground != col(nord.light.text) {
			t.Errorf("auto %+v terminal %+v own %+v", auto, terminal, own)
		}
	})

	t.Run("UT-05 settings saves the look and the mode when not the default, and previews the look hovered", func(t *testing.T) {
		base := config.Config{Provider: "llama-server"}
		untouched, _ := newSettingsDraft(base).applyTo(base, nil)
		draft := newSettingsDraft(base)
		draft.uiTheme, draft.uiMode = "catppuccin", config.ModeLight
		layer, err := draft.applyTo(base, nil)

		preview := uiPreview(uiThemeNamed("catppuccin"), false)

		if untouched.Render.Theme != "" || untouched.Render.Mode != "" || err != nil || layer.Render.Theme != "catppuccin" || layer.Render.Mode != "light" {
			t.Errorf("untouched %q %q saved %q %q err %v", untouched.Render.Theme, untouched.Render.Mode, layer.Render.Theme, layer.Render.Mode, err)
		}
		if background := uiThemeNamed("catppuccin").light.background; !strings.Contains(preview, strings.TrimPrefix(sgrOf(background), "38")) {
			t.Errorf("the preview is not on %s", background)
		}
	})
}
