package tui

import (
	"math"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

const (
	uiSliderWidth = 20
	// A select's arrows, as parts of its hit.
	uiSelectEarlier = 1
	uiSelectLater   = 2
)

// controlColors are a control's mark and text: dim when disabled, the
// accent when in focus.
func (r uiRenderer) controlColors(n tools.UINode) (mark, text string) {
	switch {
	case n.Disabled:
		return "dim", "dim"
	case r.focus == n.ID:
		return "accent", orDefault(n.FG, "text")
	}
	return orDefault(n.FG, "text"), orDefault(n.FG, "text")
}

// labelPart is a control's label before it, with a space, and its width.
func labelPart(n tools.UINode, width int, bg string) (string, int) {
	if n.Label == "" {
		return "", 0
	}
	text := ansi.Truncate(n.Label, max(width/2, 1), "…") + " "
	return uiStyle(orDefault(n.FG, "text"), bg, n.Bold).Render(text), ansi.StringWidth(text)
}

// selectBox draws ◂ value ▸ as wide as the widest option; a click on ◂
// goes back, anywhere else on.
func (r uiRenderer) selectBox(n tools.UINode, width int, bg string) uiBox {
	label, lw := labelPart(n, width, bg)
	widest := 1
	for _, option := range n.Options {
		widest = max(widest, ansi.StringWidth(option))
	}
	widest = max(min(widest, width-lw-4), 1)
	mark, text := r.controlColors(n)
	face := orDefault(n.BG, "surface")
	value := ansi.Truncate(n.Current(), widest, "…")
	value += strings.Repeat(" ", widest-ansi.StringWidth(value))
	line := label + uiStyle(mark, face, false).Render("◂ ") + uiStyle(text, face, r.focus == n.ID).Render(value) +
		uiStyle(mark, face, false).Render(" ▸")
	w := lw + widest + 4
	return uiBox{lines: []string{line}, width: w, hits: []uiHit{
		{x0: lw, x1: lw + 2, y1: 1, id: n.ID, part: uiSelectEarlier},
		{x0: lw + 2, x1: w, y1: 1, id: n.ID, part: uiSelectLater},
	}}
}

// slider draws a track with a knob where the value is, and the value; a
// click on the track puts it there.
func (r uiRenderer) slider(n tools.UINode, width int, bg string, fill bool) uiBox {
	label, lw := labelPart(n, width, bg)
	least, most, _ := n.Range()
	digits := max(len(tools.FormatNumber(least)), len(tools.FormatNumber(most)), len(n.Current()))
	track := uiSliderWidth
	if n.Width > 0 || fill {
		track = width - lw - digits - 1
	}
	track = max(min(track, width-lw-digits-1), 3)
	fraction := (n.Number() - least) / (most - least)
	knob := int(math.Round(fraction * float64(track-1)))
	mark, text := r.controlColors(n)
	filled := "accent"
	if n.Disabled {
		filled = "dim"
	}
	line := label + uiStyle(filled, bg, false).Render(strings.Repeat("━", knob)) +
		uiStyle(mark, bg, true).Render("●") + uiStyle("dim", bg, false).Render(strings.Repeat("─", track-knob-1)) +
		uiStyle(text, bg, false).Render(" "+padLeft(n.Current(), digits))
	return uiBox{lines: []string{line}, width: lw + track + 1 + digits, hits: []uiHit{{x0: lw, x1: lw + track, y1: 1, id: n.ID}}}
}

func padLeft(text string, width int) string {
	return strings.Repeat(" ", max(width-ansi.StringWidth(text), 0)) + text
}

// checkbox draws [x] or [ ] before its label, or a switch as ○── or ──●.
func (r uiRenderer) checkbox(n tools.UINode, width int, bg string) uiBox {
	mark, text := r.controlColors(n)
	on := n.Checked()
	var box string
	switch {
	case n.Type == tools.UISwitch && on:
		face := "success"
		if mark != orDefault(n.FG, "text") {
			face = mark
		}
		box = uiStyle("background", face, true).Render(" ● ")
	case n.Type == tools.UISwitch:
		box = uiStyle(mark, "surface", false).Render(" ○ ")
	case on:
		box = uiStyle(mark, bg, true).Render("[x]")
	default:
		box = uiStyle(mark, bg, false).Render("[ ]")
	}
	line := box
	w := 3
	if n.Label != "" {
		words := ansi.Truncate(n.Label, max(width-4, 1), "…")
		line += uiStyle(text, bg, n.Bold).Render(" " + words)
		w += 1 + ansi.StringWidth(words)
	}
	return uiBox{lines: []string{line}, width: w, hits: []uiHit{{x1: w, y1: 1, id: n.ID}}}
}

// radio draws its options in a column, or a row if its direction says,
// the chosen one marked, its label before them.
func (r uiRenderer) radio(n tools.UINode, width int, bg string) uiBox {
	mark, text := r.controlColors(n)
	chosen := slices.Index(n.Options, n.Current())
	var out uiBox
	if n.Label != "" {
		words := ansi.Truncate(n.Label, width, "…")
		out.lines = append(out.lines, uiStyle(orDefault(n.FG, "text"), bg, true).Render(words))
		out.width = ansi.StringWidth(words)
	}
	var row strings.Builder
	x := 0
	for i, option := range n.Options {
		dot, style := "( ) ", uiStyle(text, bg, false)
		if i == chosen {
			dot, style = "(•) ", uiStyle(mark, bg, true)
		}
		words := ansi.Truncate(option, max(width-4, 1), "…")
		item := style.Render(dot + words)
		w := 4 + ansi.StringWidth(words)
		if n.Direction == "row" {
			if i > 0 {
				row.WriteString(blank(2, bg))
				x += 2
			}
			if x+w > width && i > 0 {
				break
			}
			row.WriteString(item)
			out.hits = append(out.hits, uiHit{x0: x, x1: x + w, y0: len(out.lines), y1: len(out.lines) + 1, id: n.ID, part: i + 1})
			x += w
			continue
		}
		out.hits = append(out.hits, uiHit{x1: w, y0: len(out.lines), y1: len(out.lines) + 1, id: n.ID, part: i + 1})
		out.lines = append(out.lines, item)
		out.width = max(out.width, w)
	}
	if n.Direction == "row" {
		out.lines = append(out.lines, row.String())
		out.width = max(out.width, x)
	}
	return widen(out, out.width, bg)
}
