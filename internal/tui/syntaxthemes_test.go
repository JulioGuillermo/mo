package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | ST-01 | Happy    | The themes are the ones the config accepts, mo's own first and the default | P0     |
// | ST-02 | Happy    | Every theme has a colour for every piece, dark and light                  | P0       |
// | ST-03 | Happy    | A theme chosen colours the viewer's code, the commands and the input      | P0       |
// | ST-04 | Happy    | mo's own leaves the answers' code blocks to glamour; the others colour them | P1    |
// | ST-05 | Happy    | Settings › Interface › Code colours previews the theme hovered, the shell first, and saves it | P0 |

import (
	"fmt"
	"image/color"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
)

func usingSyntax(t *testing.T, name string) {
	t.Helper()
	before := currentSyntax().name
	useSyntaxTheme(name)
	t.Cleanup(func() { useSyntaxTheme(before) })
}

// sgrOf is how a #rrggbb colour is written as the colour of text.
func sgrOf(hex string) string {
	value, _ := strconv.ParseUint(hex[1:], 16, 32)
	return fmt.Sprintf("38;2;%d;%d;%d", value>>16&0xff, value>>8&0xff, value&0xff)
}

func TestSyntaxThemes(t *testing.T) {
	t.Run("ST-01 the themes are the ones the config accepts, mo's own first and the default", func(t *testing.T) {
		var names []string
		for _, theme := range syntaxThemes {
			names = append(names, theme.name)
		}

		if !slices.Equal(names, config.SyntaxThemes) || config.Defaults().Render.Syntax != "mo" || syntaxThemeNamed("nope") != syntaxThemes[0] {
			t.Errorf("themes %q config %q default %q", names, config.SyntaxThemes, config.Defaults().Render.Syntax)
		}
	})

	t.Run("ST-02 every theme has a colour for every piece, dark and light", func(t *testing.T) {
		for _, theme := range syntaxThemes {
			for _, colors := range []syntaxColors{theme.dark, theme.light} {
				fields := reflect.ValueOf(colors)
				for i := range fields.NumField() {
					if color := fields.Field(i).FieldByName("color").String(); !strings.HasPrefix(color, "#") || len(color) != 7 {
						t.Errorf("%s %s = %q", theme.name, fields.Type().Field(i).Name, color)
					}
				}
			}
		}
	})

	t.Run("ST-03 a theme chosen colours the viewer's code, the commands and the input", func(t *testing.T) {
		usingSyntax(t, "dracula")
		pink := syntaxThemeNamed("dracula").dark.keyword.color
		m := conversationModel()
		m.input.SetValue("!go build")

		code := newCodeText("func main() {}", "go", 80).render(0, 1, func(int) color.Color { return changePalette(true).block }, true)[0]
		command := highlightLines("go build -o mo", shellLanguage, 80, changePalette(true).block, true)[0]
		input := m.shellInputView(m.input.View())

		if !strings.Contains(code, sgrOf(pink)) {
			t.Errorf("func is not %s: %q", pink, code)
		}
		program := sgrOf(syntaxThemeNamed("dracula").dark.program.color)
		if !strings.Contains(command, program) || !strings.Contains(input, program) {
			t.Errorf("go is not %s:\n%q\n%q", program, command, input)
		}
	})

	t.Run("ST-04 mo's own leaves the answers' code blocks to glamour; the others colour them", func(t *testing.T) {
		usingSyntax(t, "mo")
		own := markdownStyle(true).CodeBlock.Chroma
		useSyntaxTheme("gruvbox")
		styled := markdownStyle(true).CodeBlock.Chroma

		if own.Keyword.Color != nil && *own.Keyword.Color == syntaxThemeNamed("gruvbox").dark.keyword.color {
			t.Errorf("mo took gruvbox's keyword")
		}
		if styled.Keyword.Color == nil || *styled.Keyword.Color != syntaxThemeNamed("gruvbox").dark.keyword.color || styled.Background.BackgroundColor == nil {
			t.Errorf("gruvbox keyword %v background %v", styled.Keyword.Color, styled.Background.BackgroundColor)
		}
	})

	t.Run("ST-05 settings › interface › code colours previews the theme hovered, the shell first, and saves it", func(t *testing.T) {
		usingSyntax(t, "mo")
		base := config.Config{Provider: "llama-server"}
		draft := newSettingsDraft(base)
		draft.syntax = "nord"

		preview := codePreview(syntaxThemeNamed(draft.syntax), true, changePalette(true).block)
		layer, err := draft.applyTo(base, nil)
		untouched, _ := newSettingsDraft(base).applyTo(base, nil)

		if !strings.HasPrefix(ansi.Strip(preview), shellLanguage) || !strings.Contains(preview, sgrOf(syntaxThemeNamed("nord").dark.function.color)) {
			t.Errorf("preview:\n%s", ansi.Strip(preview))
		}
		if err != nil || layer.Render.Syntax != "nord" || untouched.Render.Syntax != "" {
			t.Errorf("saved %q untouched %q err %v", layer.Render.Syntax, untouched.Render.Syntax, err)
		}
	})
}
