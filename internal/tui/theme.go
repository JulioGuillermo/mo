package tui

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// keepTerminalBackground leaves the terminal's own background alone.
const keepTerminalBackground = "terminal"

// screenColors are the background mo paints, so it looks the same in any
// terminal, and the text on it; nil keeps the terminal's.
type screenColors struct {
	background, foreground color.Color
	dark                   bool
	set                    bool
}

func screenFor(setting string) screenColors {
	setting = strings.TrimSpace(setting)
	if setting == "" || setting == keepTerminalBackground {
		return screenColors{}
	}
	background := lipgloss.Color(setting)
	dark := luminance(background) < 0.5
	foreground := lipgloss.Color("#c0caf5")
	if !dark {
		foreground = lipgloss.Color("#3760bf")
	}
	return screenColors{background: background, foreground: foreground, dark: dark, set: true}
}

// luminance is how light a colour looks, from 0 to 1.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 0xffff
}

// applyScreen sets the darkness of everything drawn to the background mo
// paints, or to the terminal's when it paints none.
func (m *model) applyScreen(terminalDark bool) tea.Cmd {
	dark := terminalDark
	if screen := screenFor(m.session.Config.Render.Background); screen.set {
		dark = screen.dark
	}
	m.printer.setDark(dark)
	m.input.SetStyles(inputStyles(m.input.Styles(), dark))
	return m.scheduleRedraw()
}

// markdownStyle is glamour's own, with code on blue instead of grey.
func markdownStyle(dark bool) ansi.StyleConfig {
	config := styles.DarkStyleConfig
	codeText, codeBackground, blockBackground := "#7dcfff", "#16264a", "#0b1530"
	if !dark {
		config = styles.LightStyleConfig
		codeText, codeBackground, blockBackground = "#2e7de9", "#d5e1f7", "#e6ecfa"
	}
	config.Code.Color, config.Code.BackgroundColor = &codeText, &codeBackground
	if config.CodeBlock.Chroma != nil {
		chroma := *config.CodeBlock.Chroma
		chroma.Background.BackgroundColor = &blockBackground
		config.CodeBlock.Chroma = &chroma
	}
	return config
}
