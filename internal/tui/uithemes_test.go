package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | UT-01 | Happy    | The looks are the ones the config accepts; mo's own, dark, is the default and as before | P0 |
// | UT-02 | Happy    | Every look has every colour, its text light on dark grounds and dark on light ones | P0 |
// | UT-03 | Happy    | A look chosen colours the dialogs, the status line, the diffs and the accent | P0     |
// | UT-04 | Happy    | The theme's background follows the mode: dark, light, or auto as the terminal is; a colour of its own sets the darkness | P0 |
// | UT-05 | Happy    | Settings saves the look and the mode when not the default, and previews the look hovered | P1 |
// | UT-06 | Happy    | Redline's own colours reach the separators, the context, tables, headings, line numbers and the cursor line | P1 |
// | UT-07 | Happy    | A look without colours of its own keeps faint separators and numbers, green context and its grounds for tables | P0 |
// | UT-08 | Happy    | Every machine theme sets all its own colours, dark and light, and colours code of its own | P1 |

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/glamour/v2/styles"

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
					if fields.Type().Field(i).Anonymous {
						continue
					}
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

	t.Run("UT-06 redline's own colours reach the separators, the context, tables, headings, line numbers and the cursor line", func(t *testing.T) {
		usingUI(t, "redline", true)
		red := uiNow()

		status, table, markdown := statusPalette(true), tablePalette(true), markdownStyle(true)

		if status.separator != col(red.separator) || status.low != col(red.gauge) || table.header != col(red.tableHeader) || table.even != col(red.tableEven) {
			t.Errorf("status %+v table %+v", status, table)
		}
		if markdown.Heading.Color == nil || *markdown.Heading.Color != red.heading || markdown.LinkText.Color == nil || *markdown.LinkText.Color != red.heading {
			t.Errorf("headings and links are not red")
		}
		if lineNumberStyle(false).GetForeground() != col(red.lineNumber) || !strings.Contains(cursorBackground(), "42;10;20") {
			t.Errorf("line number %v cursor %q", lineNumberStyle(false).GetForeground(), cursorBackground())
		}
	})

	t.Run("UT-07 a look without colours of its own keeps faint separators and numbers, green context and its grounds for tables", func(t *testing.T) {
		usingUI(t, "mo", true)
		mo := uiNow()

		status, table := statusPalette(true), tablePalette(true)

		if status.separator != col(mo.faint) || status.low != col(mo.green) || table.header != col(mo.button) || table.odd != col(mo.block) {
			t.Errorf("status %+v table %+v", status, table)
		}
		if !lineNumberStyle(false).GetFaint() || markdownStyle(true).LinkText.Color != styles.DarkStyleConfig.LinkText.Color {
			t.Error("line numbers or links changed")
		}
	})

	t.Run("UT-08 every machine theme sets all its own colours, dark and light, and colours code of its own", func(t *testing.T) {
		for _, name := range []string{"redline", "coldwire", "neon requiem", "fallout"} {
			theme, syntax := uiThemeNamed(name), syntaxThemeNamed(name)
			if theme == nil || syntax == nil || syntax.name != name {
				t.Fatalf("%s is missing", name)
			}
			for _, dark := range []bool{true, false} {
				extras := reflect.ValueOf(theme.colors(dark).uiExtras)
				for i := range extras.NumField() {
					if value := extras.Field(i).String(); !strings.HasPrefix(value, "#") {
						t.Errorf("%s dark=%v %s = %q", name, dark, extras.Type().Field(i).Name, value)
					}
				}
				if syntax.colors(dark).keyword.color != theme.colors(dark).inlineCode {
					t.Errorf("%s dark=%v: code keywords and code in text differ", name, dark)
				}
			}
		}
	})
}
