package tui

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const stylePreviewWidth = 64

// codeSamples are what a theme of code colours is shown on, the shell first.
var codeSamples = []struct{ language, code string }{
	{shellLanguage, "go build -o mo --tags=\"a b\" ./cmd/mo 2>/dev/null\ngrep -c 'ok' out.log && echo \"$HOME\" 42 >> x.log # done"},
	{"go", "// Sum adds the values.\nfunc Sum(values []int) (total int) {\n    for _, v := range values { total += v * 2 }\n    return total, nil\n}"},
	{"python", "@cache\ndef greet(name: str = \"mo\") -> str | None:\n    return f\"hi {name}\" if name else None  # 3.12"},
	{"typescript", "export const load = async (url: string): Promise<Item[]> =>\n  (await fetch(url)).json() as Item[] // 42"},
	{"json", `{"name": "mo", "tools": 24, "on": true, "tags": ["a", null]}`},
}

func styleItems(m model, _ *settingsMenu) []settingItem {
	dark := m.printer == nil || m.printer.isDark()
	return []settingItem{
		{"Code colours", func(d *settingsDraft) string { return d.syntax }, func(d *settingsDraft) huh.Field {
			var options []huh.Option[string]
			for _, theme := range syntaxThemes {
				options = append(options, huh.NewOption(theme.name+" · "+theme.about, theme.name))
			}
			return huh.NewSelect[string]().Title("Code colours").
				DescriptionFunc(func() string { return codePreview(syntaxThemeNamed(d.syntax), dark) }, &d.syntax).
				Options(options...).Value(&d.syntax)
		}},
	}
}

// codePreview is each sample coloured as a theme colours code, on the
// background of a block, in the screen's darkness.
func codePreview(theme *syntaxTheme, dark bool) string {
	colors := theme.colors(dark)
	background := changePalette(dark).block
	shade := lipgloss.NewStyle().Background(background)
	var out []string
	for _, sample := range codeSamples {
		out = append(out, dimStyle.Render(sample.language))
		for _, line := range highlightWith(colors, sample.code, sample.language, stylePreviewWidth-2, background) {
			fill := shade.Render(strings.Repeat(" ", max(stylePreviewWidth-1-ansi.StringWidth(line), 0)))
			out = append(out, shade.Render(" ")+line+fill)
		}
	}
	return strings.Join(out, "\n")
}
