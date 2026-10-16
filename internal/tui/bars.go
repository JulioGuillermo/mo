package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// barStyle is how the context bar is drawn: a full cell, an empty one and,
// for those with two steps a cell, a half one.
type barStyle struct {
	name              string
	full, half, empty string
	about             string
}

// barStyles are the context bar's looks, the first the default. Box
// drawing, block elements and braille are drawn by most terminals
// themselves a cell wide; the shapes come from the font, as it has them.
var barStyles = []barStyle{
	{name: "shapes", full: "▰", empty: "▱", about: "parallelograms"},
	{name: "braille", full: "⣿", half: "⣏", empty: "⣉", about: "dots, an outline filled two steps a cell"},
	{name: "circles", full: "●", empty: "○", about: "circles"},
	{name: "half-circles", full: "●", half: "◐", empty: "○", about: "circles, a half one between"},
	{name: "squares", full: "■", empty: "□", about: "squares"},
	{name: "half-squares", full: "■", half: "◧", empty: "□", about: "squares, a half one between"},
	{name: "dots", full: "•", empty: "·", about: "dots"},
	{name: "rings", full: "◉", empty: "◎", about: "rings"},
	{name: "dashes", full: "╍", empty: "╍", about: "heavy dashes"},
	{name: "shade", full: "▓", empty: "░", about: "shades"},
	{name: "blocks", full: "█", empty: "░", about: "blocks"},
	{name: "halves", full: "▌", empty: "▌", about: "half blocks, apart"},
	{name: "line", full: "━", empty: "━", about: "a heavy line"},
}

// barStyleNamed is a style by its name, the default for any other.
func barStyleNamed(name string) barStyle {
	for _, style := range barStyles {
		if style.name == name {
			return style
		}
	}
	return barStyles[0]
}

// cells are the bar's cells for a share: full, then a half one when the
// style has it and the share asks, then empty.
func (s barStyle) cells(share float64, cells int) (full, half, empty int) {
	share = min(max(share, 0), 1)
	if s.half == "" {
		full = min(int(share*float64(cells)+0.5), cells)
		if share > 0 && full == 0 {
			full = 1
		}
		return full, 0, cells - full
	}
	halves := min(int(share*float64(cells*2)+0.5), cells*2)
	if share > 0 && halves == 0 {
		halves = 1
	}
	full, half = halves/2, halves%2
	return full, half, cells - full - half
}

// draw is the bar for a share in a style, what is used in fill and the
// rest in empty.
func (s barStyle) draw(share float64, cells int, fill, empty color.Color) string {
	full, half, rest := s.cells(share, cells)
	used := strings.Repeat(s.full, full) + strings.Repeat(s.half, half)
	return lipgloss.NewStyle().Foreground(fill).Render(used) + lipgloss.NewStyle().Foreground(empty).Render(strings.Repeat(s.empty, rest))
}

// sample is the bar as it looks, uncoloured, to choose it by.
func (s barStyle) sample() string {
	full, half, rest := s.cells(0.65, 10)
	return strings.Repeat(s.full, full) + strings.Repeat(s.half, half) + strings.Repeat(s.empty, rest)
}

// barSample is a style at 65%, in the status line's colours, to choose it
// by: those that fill with the same mark they leave empty differ by colour.
func barSample(style barStyle) string {
	colors := statusPalette(true)
	return style.draw(0.65, 10, colors.low, colors.empty)
}
