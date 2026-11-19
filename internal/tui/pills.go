package tui

import (
	"image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// pillShape is a way to end a pill: the glyph that closes it on its left
// and the one on its right, none for square.
type pillShape struct {
	name, left, right, about string
}

var pillShapes = []pillShape{
	{"round", "\ue0b6", "\ue0b4", "half circle"},
	{"arrow", "\ue0b2", "\ue0b0", "triangle"},
	{"slant", "\ue0ba", "\ue0bc", "diagonal cut"},
	{"flame", "\ue0c2", "\ue0c0", "flames"},
	{"pixel", "\ue0c5", "\ue0c4", "pixels"},
	{"square", "", "", "no glyph, for any font"},
}

const (
	pillDetailsSegment = "segment"
	pillDetailsText    = "text"
)

// pillLook is how pills are drawn: the shape of each end, and whether the
// details beside the name go on a ground of their own or as dim text.
type pillLook struct {
	left, right, details string
}

func (d *settingsDraft) pillLook() pillLook {
	return pillLook{left: d.pillLeft, right: d.pillRight, details: d.pillDetails}
}

// shapeNamed is the shape of that name; an unknown one is square, which
// needs no glyph.
func shapeNamed(name string) pillShape {
	for _, shape := range pillShapes {
		if shape.name == name {
			return shape
		}
	}
	return pillShapes[len(pillShapes)-1]
}

// pillLook is how pills are drawn as the settings say; without a powerline
// status line, whose font may lack the glyphs, their ends are square.
func (m model) pillLook() pillLook {
	render := m.session.Config.Render
	look := pillLook{left: render.PillLeft, right: render.PillRight, details: orDefault(render.PillDetails, pillDetailsSegment)}
	if m.statusStyle() != statusPowerline {
		look.left, look.right = "square", "square"
	}
	return look
}

// pill draws a name on its ground and details beside it, closed and joined
// as look says.
func pill(look pillLook, name, details string, nameGround, nameText, detailsGround, detailsText color.Color) string {
	left, right := shapeNamed(look.left).left, shapeNamed(look.right).right
	drawn := ""
	if left != "" {
		drawn = lipgloss.NewStyle().Foreground(nameGround).Render(left)
	}
	drawn += lipgloss.NewStyle().Background(nameGround).Foreground(nameText).Bold(true).Render(name)
	if look.details == pillDetailsText {
		if right != "" {
			drawn += lipgloss.NewStyle().Foreground(nameGround).Render(right)
		}
		return drawn + lipgloss.NewStyle().Foreground(detailsText).Faint(true).Render(details)
	}
	if right != "" {
		drawn += lipgloss.NewStyle().Foreground(nameGround).Background(detailsGround).Render(right)
	}
	drawn += lipgloss.NewStyle().Background(detailsGround).Foreground(detailsText).Render(details)
	if right != "" {
		drawn += lipgloss.NewStyle().Foreground(detailsGround).Render(right)
	}
	return drawn
}

// pillSample is how a pill looks so, for the settings.
func pillSample(look pillLook) string {
	return pill(look, " btw ", " 3 lines ", accent(), col(uiNow().onAccent), col(uiNow().segment), col(uiNow().subtext))
}

// shapeOptions are the shapes to pick for one end, each drawn there.
func shapeOptions(rightEnd bool) []huh.Option[string] {
	var options []huh.Option[string]
	for _, shape := range pillShapes {
		look := pillLook{left: "square", right: shape.name, details: pillDetailsSegment}
		if !rightEnd {
			look = pillLook{left: shape.name, right: "square", details: pillDetailsSegment}
		}
		options = append(options, huh.NewOption(pillSample(look)+"  "+shape.name+": "+shape.about, shape.name))
	}
	return options
}
