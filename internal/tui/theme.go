package tui

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/config"
)

// keepTerminalBackground leaves the terminal's own background alone.
const keepTerminalBackground = "terminal"

// screenColors are the background mo paints, so it looks the same in any
// terminal, and the text on it; set is false to keep the terminal's. dark
// is how the screen is drawn.
type screenColors struct {
	background, foreground color.Color
	dark                   bool
	set                    bool
}

// screenFor is the screen the settings give, on a terminal dark or not:
// the theme's background, dark or light as the mode says, a colour of its
// own, or the terminal's.
func screenFor(settings config.Render, terminalDark bool) screenColors {
	setting := strings.TrimSpace(settings.Background)
	theme := uiThemeNamed(settings.Theme)
	switch setting {
	case "", keepTerminalBackground:
		return screenColors{dark: terminalDark}
	case config.BackgroundTheme:
		dark := settings.Mode != config.ModeLight
		if settings.Mode == config.ModeAuto {
			dark = terminalDark
		}
		colors := theme.colors(dark)
		return screenColors{background: col(colors.background), foreground: col(colors.text), dark: dark, set: true}
	}
	background := lipgloss.Color(setting)
	dark := luminance(background) < 0.5
	return screenColors{background: background, foreground: col(theme.colors(dark).text), dark: dark, set: true}
}

// luminance is how light a colour looks, from 0 to 1.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 0xffff
}

// applyScreen sets the darkness of everything drawn to the background mo
// paints, or to the terminal's when it paints none.
func (m *model) applyScreen(terminalDark bool) tea.Cmd {
	m.terminalDark = terminalDark
	dark := screenFor(m.session.Config.Render, terminalDark).dark
	if useUITheme(m.session.Config.Render.Theme, dark) {
		m.printer.forgetMarkdown()
	}
	m.printer.setDark(dark)
	m.spinner.Style = workingStyle()
	m.input.SetStyles(inputStyles(m.input.Styles(), dark))
	return m.scheduleRedraw()
}

// markdownStyle is glamour's own, with code on blue instead of grey.
func markdownStyle(dark bool) ansi.StyleConfig {
	config := styles.DarkStyleConfig
	if !dark {
		config = styles.LightStyleConfig
	}
	colors := ui(dark)
	codeText, codeBackground, blockBackground := colors.inlineCode, colors.inlineCodeBack, colors.codeBlockBack
	config.Code.Color, config.Code.BackgroundColor = &codeText, &codeBackground
	if config.CodeBlock.Chroma != nil {
		chroma := *config.CodeBlock.Chroma
		chroma.Background.BackgroundColor = &blockBackground
		config.CodeBlock.Chroma = &chroma
	}
	if theme := currentSyntax(); theme.styled {
		config.CodeBlock.Chroma = theme.colors(dark).glamourChroma(config.CodeBlock.Chroma)
	}
	return config
}
