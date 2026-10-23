package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
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
	draftDark := func(d *settingsDraft) bool {
		switch d.uiMode {
		case config.ModeLight:
			return false
		case config.ModeAuto:
			return m.terminalDark
		}
		return true
	}
	return []settingItem{
		{"Interface colours", func(d *settingsDraft) string { return d.uiTheme }, func(d *settingsDraft) huh.Field {
			var options []huh.Option[string]
			for _, theme := range uiThemes {
				options = append(options, huh.NewOption(theme.name+" · "+theme.about, theme.name))
			}
			return huh.NewSelect[string]().Title("Interface colours").
				DescriptionFunc(func() string { return uiPreview(uiThemeNamed(d.uiTheme), draftDark(d)) }, &d.uiTheme).
				Options(options...).Value(&d.uiTheme)
		}},
		{"Light or dark", func(d *settingsDraft) string { return d.uiMode }, func(d *settingsDraft) huh.Field {
			return huh.NewSelect[string]().Title("Light or dark").Description("which look of the interface colours, and of the code's, is used").Options(
				huh.NewOption("dark", config.ModeDark),
				huh.NewOption("light", config.ModeLight),
				huh.NewOption("auto: as the terminal's background is", config.ModeAuto),
			).Value(&d.uiMode)
		}},
		{"Background", func(d *settingsDraft) string { return d.background }, func(d *settingsDraft) huh.Field {
			return huh.NewInput().Title("Background").
				Description("painted behind everything so mo looks the same in any terminal: theme, the colours' own; #rrggbb; or terminal to keep its own").
				Validate(func(text string) error {
					if !config.ValidBackground(text) {
						return fmt.Errorf("use theme, #rrggbb or terminal")
					}
					return nil
				}).Value(&d.background)
		}},
		{"Code colours", func(d *settingsDraft) string { return d.syntax }, func(d *settingsDraft) huh.Field {
			var options []huh.Option[string]
			for _, theme := range syntaxThemes {
				options = append(options, huh.NewOption(theme.name+" · "+theme.about, theme.name))
			}
			return huh.NewSelect[string]().Title("Code colours").
				DescriptionFunc(func() string {
					dark := draftDark(d)
					return codePreview(syntaxThemeNamed(d.syntax), dark, col(uiThemeNamed(d.uiTheme).colors(dark).block))
				}, &d.syntax).
				Options(options...).Value(&d.syntax)
		}},
	}
}

// codePreview is each sample coloured as a theme colours code, on the
// background of a block, in the screen's darkness.
func codePreview(theme *syntaxTheme, dark bool, background color.Color) string {
	colors := theme.colors(dark)
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

// uiPreview is a sketch of the screen as a look draws it: a message of
// yours, a call and its diff, the buttons of a question and the status
// line, on the look's background.
func uiPreview(theme *uiTheme, dark bool) string {
	c := theme.colors(dark)
	screen := lipgloss.NewStyle().Background(col(c.background)).Foreground(col(c.text))
	on := func(ground string) lipgloss.Style {
		return lipgloss.NewStyle().Background(col(ground)).Foreground(col(c.text))
	}
	row := func(pieces ...string) string {
		line := strings.Join(pieces, "")
		return line + screen.Render(strings.Repeat(" ", max(stylePreviewWidth-ansi.StringWidth(line), 0)))
	}
	band := func(ground, text string) string {
		return text + on(ground).Render(strings.Repeat(" ", max(stylePreviewWidth-ansi.StringWidth(text), 0)))
	}
	accentText := on(c.background).Foreground(col(c.accent))
	chosen := lipgloss.NewStyle().Background(col(c.accent)).Foreground(col(c.onAccent)).Bold(true)
	button := on(c.button)
	segment := func(ground, fg, text string) string {
		return lipgloss.NewStyle().Background(col(ground)).Foreground(col(fg)).Render(text)
	}
	lines := []string{
		row(screen.Render(" ")),
		band(c.panel, on(c.panel).Render(" ")+accentText.Background(col(c.panel)).Bold(true).Render("›")+on(c.panel).Render(" fix the parser, please")),
		row(screen.Render(" ")),
		row(screen.Foreground(col(c.accent)).Render(" ● "), screen.Foreground(col(c.accent)).Bold(true).Render("edit"), screen.Render(" internal/parser.go"), screen.Foreground(col(c.faint)).Render("  +1 -1")),
		band(c.removed, on(c.removed).Render("   ")+lipgloss.NewStyle().Foreground(col(c.red)).Background(col(c.removed)).Bold(true).Render("-")+on(c.removed).Render(" return nil, err")),
		band(c.added, on(c.added).Render("   ")+lipgloss.NewStyle().Foreground(col(c.green)).Background(col(c.added)).Bold(true).Render("+")+on(c.added).Render(" return nil, fmt.Errorf(\"parse: %w\", err)")),
		row(screen.Render(" ")),
		band(c.body, on(c.body).Render(" ")+on(c.body).Foreground(col(c.warning)).Bold(true).Render("?")+on(c.body).Render(" run go test ./... ")+chosen.Render(" allow ")+on(c.body).Render(" ")+button.Render(" always ")+on(c.body).Render(" ")+button.Render(" deny ")),
		row(screen.Render(" "), screen.Foreground(col(c.warning)).Render("⠋ writing"), screen.Foreground(col(c.faint)).Render(" · 12s · esc to stop")),
		band(c.statusBase, segment(c.segment, c.red, " ● all ")+segment(c.segmentDim, c.accent, " provider ")+segment(c.segmentDim, c.purple, "› model ")+
			segment(c.segmentDeep, c.green, " ▰▰▰")+segment(c.segmentDeep, c.empty, "▱▱ ")+segment(c.segmentDeep, c.subtext, "42% ")+segment(c.statusBase, c.warning, "  master")),
	}
	return strings.Join(lines, "\n")
}
