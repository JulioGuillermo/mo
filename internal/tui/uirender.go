package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

// uiPalette is what the colour names of an interface stand for.
var uiPalette = map[string]color.Color{
	"text":       lipgloss.Color("#c0caf5"),
	"dim":        lipgloss.Color("#565f89"),
	"accent":     lipgloss.Color("#7aa2f7"),
	"error":      lipgloss.Color("#f7768e"),
	"warning":    lipgloss.Color("#e0af68"),
	"success":    lipgloss.Color("#9ece6a"),
	"surface":    lipgloss.Color("#292e42"),
	"background": lipgloss.Color("#16161e"),
}

const (
	uiInputWidth = 24
	uiCursor     = "▏"
)

// uiHit is where a control is drawn, to click it; part tells which of
// its pieces: a radio's option counted from 1, or a select's arrow.
type uiHit struct {
	x0, y0, x1, y1 int
	id             string
	part           int
}

// moved is a hit drawn dx right and dy down.
func (h uiHit) moved(dx, dy int) uiHit {
	h.x0, h.x1, h.y0, h.y1 = h.x0+dx, h.x1+dx, h.y0+dy, h.y1+dy
	return h
}

// uiBox is a node drawn: its lines, all as wide as width, and its hits.
type uiBox struct {
	lines []string
	width int
	hits  []uiHit
}

func (b uiBox) height() int { return len(b.lines) }

// uiRenderer draws an interface with what the user typed and where the
// focus is.
type uiRenderer struct {
	values map[string]string
	focus  string
}

func uiColor(name string) color.Color {
	if name == "" {
		return nil
	}
	if found, ok := uiPalette[name]; ok {
		return found
	}
	return lipgloss.Color(name)
}

// style paints text in fg on bg, either unset.
func uiStyle(fg, bg string, bold bool) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(bold)
	if c := uiColor(fg); c != nil {
		style = style.Foreground(c)
	}
	if c := uiColor(bg); c != nil {
		style = style.Background(c)
	}
	return style
}

func blank(width int, bg string) string {
	if width <= 0 {
		return ""
	}
	return uiStyle("", bg, false).Render(strings.Repeat(" ", width))
}

// sides reads padding or margin as CSS does: all, vertical and
// horizontal, or top, right, bottom and left.
func sides(values []int) (top, right, bottom, left int) {
	switch len(values) {
	case 1:
		return values[0], values[0], values[0], values[0]
	case 2:
		return values[0], values[1], values[0], values[1]
	case 4:
		return values[0], values[1], values[2], values[3]
	}
	return 0, 0, 0, 0
}

// widen pads every line of a box to width on bg.
func widen(box uiBox, width int, bg string) uiBox {
	for i, line := range box.lines {
		box.lines[i] = line + blank(width-ansi.StringWidth(line), bg)
	}
	box.width = max(box.width, width)
	return box
}

// surround adds rows and columns of bg around a box, moving its hits.
func surround(box uiBox, top, right, bottom, left int, bg string) uiBox {
	width := box.width + left + right
	var lines []string
	for range top {
		lines = append(lines, blank(width, bg))
	}
	for _, line := range box.lines {
		lines = append(lines, blank(left, bg)+line+blank(right, bg))
	}
	for range bottom {
		lines = append(lines, blank(width, bg))
	}
	hits := make([]uiHit, len(box.hits))
	for i, hit := range box.hits {
		hits[i] = hit.moved(left, top)
	}
	return uiBox{lines: lines, width: width, hits: hits}
}

func uiBorder(name string) (lipgloss.Border, bool) {
	switch name {
	case "rounded":
		return lipgloss.RoundedBorder(), true
	case "normal":
		return lipgloss.NormalBorder(), true
	case "thick":
		return lipgloss.ThickBorder(), true
	case "double":
		return lipgloss.DoubleBorder(), true
	}
	return lipgloss.Border{}, false
}

// framed draws a border around a box, the title in its top edge.
func framed(box uiBox, border lipgloss.Border, title, fg, bg string) uiBox {
	edge := uiStyle(orDefault(fg, "dim"), bg, false)
	top := border.Top
	inner := box.width
	head := ""
	if title != "" && inner > 4 {
		name := ansi.Truncate(title, inner-4, "…")
		head = edge.Render(top+" ") + uiStyle(orDefault(fg, "text"), bg, true).Render(name) + edge.Render(" ")
	}
	rest := inner - ansi.StringWidth(head)
	lines := []string{edge.Render(border.TopLeft) + head + edge.Render(strings.Repeat(top, max(rest, 0))+border.TopRight)}
	for _, line := range box.lines {
		lines = append(lines, edge.Render(border.Left)+line+edge.Render(border.Right))
	}
	lines = append(lines, edge.Render(border.BottomLeft+strings.Repeat(border.Bottom, inner)+border.BottomRight))
	hits := make([]uiHit, len(box.hits))
	for i, hit := range box.hits {
		hits[i] = hit.moved(1, 1)
	}
	return uiBox{lines: lines, width: inner + 2, hits: hits}
}

// render draws a node within width, on the background it sits on; fill
// makes it take the whole width, as stretch and grow ask.
func (r uiRenderer) render(n tools.UINode, width int, parentBG string, fill bool) uiBox {
	if !n.Shown() || width <= 0 {
		return uiBox{}
	}
	bg := orDefault(n.BG, parentBG)
	mt, mr, mb, ml := sides(n.Margin)
	pt, pr, pb, pl := sides(n.Padding)
	borderName := n.Border
	if n.Type == tools.UICard && borderName == "" {
		borderName = "rounded"
	}
	border, bordered := uiBorder(borderName)
	frame := pl + pr
	if bordered {
		frame += 2
	}
	room := width - ml - mr
	if n.Width > 0 {
		room = min(room, n.Width)
		fill = true
	}
	inner := max(room-frame, 1)
	content := r.content(n, inner, bg, fill)
	if len(content.lines) == 0 && n.Width > 0 {
		content.lines = []string{""}
	}
	if fill {
		content = widen(content, inner, bg)
	}
	if n.Height > 0 {
		rows := max(n.Height-pt-pb-boolInt(bordered)*2, 1)
		for len(content.lines) < rows {
			content.lines = append(content.lines, blank(content.width, bg))
		}
		content.lines = content.lines[:rows]
	}
	box := surround(content, pt, pr, pb, pl, bg)
	if bordered {
		box = framed(box, border, n.Title, n.FG, bg)
	}
	return surround(box, mt, mr, mb, ml, parentBG)
}

// content draws what a node holds, without its padding, border or margin.
func (r uiRenderer) content(n tools.UINode, width int, bg string, fill bool) uiBox {
	switch n.Type {
	case tools.UILabel:
		style := uiStyle(n.FG, bg, n.Bold)
		var lines []string
		widest := 0
		for _, line := range strings.Split(ansi.Wrap(n.Text, width, " "), "\n") {
			line = ansi.Truncate(line, width, "…")
			widest = max(widest, ansi.StringWidth(line))
			lines = append(lines, style.Render(line))
		}
		return widen(uiBox{lines: lines, width: widest}, widest, bg)
	case tools.UIButton:
		fg, face := orDefault(n.FG, "text"), orDefault(n.BG, "surface")
		switch {
		case n.Disabled:
			fg = "dim"
		case r.focus == n.ID:
			fg, face = "background", "accent"
		}
		text := " " + ansi.Truncate(n.Label, max(width-2, 1), "…") + " "
		if fill {
			text = lipgloss.PlaceHorizontal(width, lipgloss.Center, text)
		}
		line := uiStyle(fg, face, true).Render(text)
		w := ansi.StringWidth(text)
		return uiBox{lines: []string{line}, width: w, hits: []uiHit{{x1: w, y1: 1, id: n.ID}}}
	case tools.UIInput:
		w := min(uiInputWidth, width)
		if fill {
			w = width
		}
		value, typed := r.values[n.ID]
		if !typed {
			value = n.Value
		}
		face := orDefault(n.BG, "surface")
		var text string
		switch {
		case r.focus == n.ID:
			shown := value
			if ansi.StringWidth(shown)+1 > w {
				shown = ansi.TruncateLeft(shown, ansi.StringWidth(shown)+2-w, "…")
			}
			text = uiStyle(orDefault(n.FG, "text"), face, false).Render(shown) + uiStyle("accent", face, false).Render(uiCursor)
		case value == "":
			text = uiStyle("dim", face, false).Render(ansi.Truncate(n.Placeholder, w, "…"))
		default:
			text = uiStyle(orDefault(n.FG, "text"), face, false).Render(ansi.Truncate(value, w, "…"))
		}
		line := text + blank(w-ansi.StringWidth(text), face)
		return uiBox{lines: []string{line}, width: w, hits: []uiHit{{x1: w, y1: 1, id: n.ID}}}
	case tools.UISelect:
		return r.selectBox(n, width, bg)
	case tools.UISlider:
		return r.slider(n, width, bg, fill)
	case tools.UICheckbox, tools.UISwitch:
		return r.checkbox(n, width, bg)
	case tools.UIRadio:
		return r.radio(n, width, bg)
	}
	if n.Type == tools.UIGrid {
		return r.grid(n, width, bg, fill)
	}
	if n.Direction == "row" {
		return r.row(n, width, bg, fill)
	}
	return r.column(n, width, bg, fill)
}

// column stacks the children, each placed across as align says.
func (r uiRenderer) column(n tools.UINode, width int, bg string, fill bool) uiBox {
	stretch := n.Align == "stretch"
	var boxes []uiBox
	widest := 0
	for _, child := range n.Children {
		box := r.render(child, width, bg, stretch || child.Grow > 0)
		if box.height() == 0 {
			continue
		}
		boxes = append(boxes, box)
		widest = max(widest, box.width)
	}
	if fill || stretch {
		widest = width
	}
	var out uiBox
	out.width = widest
	for i, box := range boxes {
		if i > 0 {
			for range n.Gap {
				out.lines = append(out.lines, blank(widest, bg))
			}
		}
		left := 0
		switch n.Align {
		case "center":
			left = (widest - box.width) / 2
		case "end":
			left = widest - box.width
		}
		placed := surround(box, 0, widest-box.width-left, 0, left, bg)
		top := len(out.lines)
		for _, hit := range placed.hits {
			out.hits = append(out.hits, hit.moved(0, top))
		}
		out.lines = append(out.lines, placed.lines...)
	}
	return out
}

// row lays the children side by side: shrunk when they do not fit, the
// room left given to those that grow, then spread as justify says.
func (r uiRenderer) row(n tools.UINode, width int, bg string, fill bool) uiBox {
	var children []tools.UINode
	for _, child := range n.Children {
		if child.Shown() {
			children = append(children, child)
		}
	}
	if len(children) == 0 {
		return uiBox{width: 0}
	}
	gaps := n.Gap * (len(children) - 1)
	boxes := make([]uiBox, len(children))
	used := 0
	for i, child := range children {
		boxes[i] = r.render(child, width, bg, false)
		used += boxes[i].width
	}
	if used+gaps > width && used > 0 {
		room := max(width-gaps, len(children))
		natural := sumWidths(boxes)
		used = 0
		for i, child := range children {
			share := max(boxes[i].width*room/natural, 1)
			boxes[i] = r.render(child, share, bg, false)
			used += boxes[i].width
		}
	}
	left := max(width-used-gaps, 0)
	growth := 0
	for _, child := range children {
		growth += child.Grow
	}
	if growth > 0 && left > 0 {
		given := 0
		for i, child := range children {
			if child.Grow == 0 {
				continue
			}
			extra := left * child.Grow / growth
			given += extra
			boxes[i] = r.render(child, boxes[i].width+extra, bg, true)
		}
		used += given
		left = max(width-used-gaps, 0)
	}
	total := width
	if !fill && growth == 0 && (n.Justify == "" || n.Justify == "start") {
		total = used + gaps
		left = 0
	}
	height := 0
	for _, box := range boxes {
		height = max(height, box.height())
	}
	if n.Height > 0 {
		height = max(height, n.Height)
	}
	offsets := make([]int, len(boxes))
	spacing := float64(n.Gap)
	start := 0.0
	switch n.Justify {
	case "end":
		start = float64(left)
	case "center":
		start = float64(left) / 2
	case "space-between":
		if len(boxes) > 1 {
			spacing += float64(left) / float64(len(boxes)-1)
		}
	case "space-around":
		around := float64(left) / float64(len(boxes))
		start, spacing = around/2, spacing+around
	}
	x := start
	for i, box := range boxes {
		offsets[i] = int(x + 0.5)
		x += float64(box.width) + spacing
	}
	out := uiBox{width: total}
	for y := range height {
		var line strings.Builder
		at := 0
		for i, box := range boxes {
			top := 0
			switch n.Align {
			case "center":
				top = (height - box.height()) / 2
			case "end":
				top = height - box.height()
			}
			line.WriteString(blank(offsets[i]-at, bg))
			if row := y - top; row >= 0 && row < box.height() {
				line.WriteString(box.lines[row])
			} else {
				line.WriteString(blank(box.width, bg))
			}
			at = offsets[i] + box.width
			if y == 0 {
				for _, hit := range box.hits {
					out.hits = append(out.hits, hit.moved(offsets[i], top))
				}
			}
		}
		line.WriteString(blank(total-at, bg))
		out.lines = append(out.lines, line.String())
	}
	return out
}

// grid lays the children in columns, row by row: each column as wide as
// its widest cell or as widths fixes it, shrunk when they do not fit,
// and the room left given to columns with a cell that grows. gap parts
// the columns; rows sit close, as a table's do.
func (r uiRenderer) grid(n tools.UINode, width int, bg string, fill bool) uiBox {
	columns := n.GridColumns()
	cells := n.Children
	if len(cells) == 0 {
		return uiBox{}
	}
	gaps := n.Gap * (columns - 1)
	fixed := func(column int) int {
		if column < len(n.Widths) {
			return n.Widths[column]
		}
		return 0
	}
	widths := make([]int, columns)
	grows := make([]int, columns)
	for i, cell := range cells {
		column := i % columns
		grows[column] = max(grows[column], cell.Grow)
		if fixed(column) > 0 {
			widths[column] = fixed(column)
			continue
		}
		widths[column] = max(widths[column], r.render(cell, width, bg, false).width)
	}
	used := 0
	for _, w := range widths {
		used += w
	}
	if room := max(width-gaps, columns); used > room {
		natural := max(used, 1)
		used = 0
		for column, w := range widths {
			widths[column] = max(w*room/natural, 1)
			used += widths[column]
		}
	}
	growth := 0
	for _, grow := range grows {
		growth += grow
	}
	if left := width - gaps - used; growth > 0 && left > 0 {
		for column, grow := range grows {
			widths[column] += left * grow / growth
		}
	}
	stretch := n.Align == "stretch"
	var out uiBox
	for start := 0; start < len(cells); start += columns {
		boxes := make([]uiBox, columns)
		height := 0
		for column := range columns {
			if start+column >= len(cells) {
				continue
			}
			cell := cells[start+column]
			boxes[column] = r.render(cell, widths[column], bg, stretch || grows[column] > 0 || fixed(column) > 0)
			height = max(height, boxes[column].height())
		}
		top := len(out.lines)
		for y := range height {
			var line strings.Builder
			x := 0
			for column, box := range boxes {
				if column > 0 {
					line.WriteString(blank(n.Gap, bg))
					x += n.Gap
				}
				left := 0
				switch n.Align {
				case "center":
					left = (widths[column] - box.width) / 2
				case "end":
					left = widths[column] - box.width
				}
				left = max(left, 0)
				line.WriteString(blank(left, bg))
				if y < box.height() {
					line.WriteString(box.lines[y])
				} else {
					line.WriteString(blank(box.width, bg))
				}
				line.WriteString(blank(widths[column]-left-box.width, bg))
				if y == 0 {
					for _, hit := range box.hits {
						out.hits = append(out.hits, hit.moved(x+left, top))
					}
				}
				x += widths[column]
			}
			out.lines = append(out.lines, line.String())
		}
	}
	out.width = gaps
	for _, w := range widths {
		out.width += w
	}
	if fill {
		out = widen(out, width, bg)
	}
	return out
}

func sumWidths(boxes []uiBox) int {
	total := 0
	for _, box := range boxes {
		total += box.width
	}
	return max(total, 1)
}

// focusable are the ids that take the focus, in the order tab goes.
func focusable(n tools.UINode) []string {
	if !n.Shown() {
		return nil
	}
	var ids []string
	if n.Focusable() && !n.Disabled {
		ids = append(ids, n.ID)
	}
	for _, child := range n.Children {
		ids = append(ids, focusable(child)...)
	}
	return ids
}

// findNode is the node of an id in a tree.
func findNode(n tools.UINode, id string) (tools.UINode, bool) {
	if n.ID == id {
		return n, true
	}
	for _, child := range n.Children {
		if found, ok := findNode(child, id); ok {
			return found, true
		}
	}
	return tools.UINode{}, false
}
