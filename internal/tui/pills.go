package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

const (
	pillRound = "round"
	pillArrow = "arrow"
	pillPlain = "plain"
)

// pillStyle is how pills end: as the settings say, or plain without a
// powerline status line, whose font may lack the glyphs.
func (m model) pillStyle() string {
	if m.statusStyle() != statusPowerline {
		return pillPlain
	}
	return orDefault(m.session.Config.Render.Pills, pillRound)
}

// pillEnds are the glyphs that close a pill on each side.
func pillEnds(style string) (left, right string) {
	switch style {
	case pillRound:
		return "\ue0b6", "\ue0b4"
	case pillArrow:
		return arrowLeft, arrowRight
	}
	return "", ""
}

// pill draws segments of text on their grounds as one pill, closed at both
// ends and joined in between by the ends of style; plain has none.
func pill(style string, texts []string, grounds []color.Color, foregrounds []color.Color) string {
	left, right := pillEnds(style)
	drawn := ""
	if left != "" {
		drawn = lipgloss.NewStyle().Foreground(grounds[0]).Render(left)
	}
	for i, text := range texts {
		drawn += lipgloss.NewStyle().Background(grounds[i]).Foreground(foregrounds[i]).Bold(i == 0).Render(text)
		if right == "" {
			continue
		}
		end := lipgloss.NewStyle().Foreground(grounds[i])
		if i+1 < len(texts) {
			end = end.Background(grounds[i+1])
		}
		drawn += end.Render(right)
	}
	return drawn
}

// pillSample is how a pill of that style looks, for the settings.
func pillSample(style string) string {
	return pill(style, []string{"btw", " 3 lines "}, []color.Color{accent(), col(uiNow().segment)},
		[]color.Color{col(uiNow().onAccent), col(uiNow().subtext)})
}
