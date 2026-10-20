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
	// svgs draws the interface's svg nodes, ui being its number.
	svgs *svgFrames
	ui   int
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

// uiRoom is the room a node is drawn in: height 0 when its parent has no
// height to give. fillWidth and fillHeight ask the node to take all of it,
// as stretch, grow and full do.
type uiRoom struct {
	width, height         int
	fillWidth, fillHeight bool
}

// render draws a node within its room, on the background it sits on. Its
// width and height, fixed or full, and its least and most sizes count
// with its padding and border, not its margin. A node is never cut to its
// room's height, only to its own: what is taller than the dialog scrolls.
func (r uiRenderer) render(n tools.UINode, room uiRoom, parentBG string) uiBox {
	if !n.Shown() || room.width <= 0 {
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
	across, along := pl+pr, pt+pb
	if bordered {
		across, along = across+2, along+2
	}

	width := room.width - ml - mr
	fillWidth := room.fillWidth || n.Width.Full()
	if n.Width.Cells() > 0 {
		width, fillWidth = min(width, n.Width.Cells()), true
	}
	if n.MaxWidth > 0 {
		width = min(width, n.MaxWidth)
	}
	height := max(room.height-mt-mb, 0)
	fillHeight := (room.fillHeight || n.Height.Full()) && height > 0
	if n.Height.Cells() > 0 {
		height, fillHeight = n.Height.Cells(), true
	}
	if n.MaxHeight > 0 && (height == 0 || height > n.MaxHeight) {
		height = n.MaxHeight
	}

	inner := uiRoom{width: max(width-across, 1), fillWidth: fillWidth, fillHeight: fillHeight}
	if height > 0 {
		inner.height = max(height-along, 1)
	}
	content := r.content(n, inner, bg)
	if len(content.lines) == 0 && (n.Width.Cells() > 0 || fillHeight) {
		content.lines = []string{""}
	}
	if fillWidth {
		content = widen(content, inner.width, bg)
	}
	if n.MinWidth > 0 {
		content = widen(content, min(n.MinWidth-across, inner.width), bg)
	}
	rows := len(content.lines)
	if fillHeight {
		rows = inner.height
	}
	if n.MinHeight > 0 {
		rows = max(rows, n.MinHeight-along)
	}
	if most := n.MaxHeight - along; n.MaxHeight > 0 {
		rows = min(rows, max(most, 1))
	}
	content = withRows(content, rows, bg)
	box := surround(content, pt, pr, pb, pl, bg)
	if bordered {
		// A card is as wide as its title needs, so the title shows.
		if need := ansi.StringWidth(n.Title) + 4; n.Title != "" && box.width < need {
			box = widen(box, min(need, max(width-2, box.width)), bg)
		}
		box = framed(box, border, n.Title, n.FG, bg)
	}
	return surround(box, mt, mr, mb, ml, parentBG)
}

// withRows pads a box with blank rows, or cuts it, to rows; its hits below
// the cut go.
func withRows(box uiBox, rows int, bg string) uiBox {
	for len(box.lines) < rows {
		box.lines = append(box.lines, blank(box.width, bg))
	}
	if len(box.lines) > rows {
		box.lines = box.lines[:rows]
		kept := box.hits[:0]
		for _, hit := range box.hits {
			if hit.y0 < rows {
				hit.y1 = min(hit.y1, rows)
				kept = append(kept, hit)
			}
		}
		box.hits = kept
	}
	return box
}

// content draws what a node holds, without its padding, border or margin.
func (r uiRenderer) content(n tools.UINode, room uiRoom, bg string) uiBox {
	width, fill := room.width, room.fillWidth
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
	case tools.UITextarea:
		return r.textarea(n, room)
	case tools.UISelect:
		return r.selectBox(n, width, bg)
	case tools.UISlider:
		return r.slider(n, width, bg, fill)
	case tools.UICheckbox, tools.UISwitch:
		return r.checkbox(n, width, bg)
	case tools.UIRadio:
		return r.radio(n, width, bg)
	case tools.UISVG:
		return r.svg(n, room, bg)
	}
	if n.Type == tools.UIGrid {
		return r.grid(n, width, bg, fill)
	}
	if n.Direction == "row" {
		return r.row(n, room, bg)
	}
	return r.column(n, room, bg)
}

// growth is how much of the room left along its parent a child takes:
// its grow, or 1 when its size along is full.
func growth(child tools.UINode, along tools.UISize) int {
	if child.Grow > 0 {
		return child.Grow
	}
	if along.Full() {
		return 1
	}
	return 0
}

// natural is a child as it is when it takes no more room than it needs:
// before the room left is shared out, a full size along counts as none.
func natural(child tools.UINode, row bool) tools.UINode {
	if row && child.Width.Full() {
		child.Width = 0
	}
	if !row && child.Height.Full() {
		child.Height = 0
	}
	return child
}

// column stacks the children, each placed across as align says. When the
// column has a height to give, the rows left go to the children that grow,
// as their grow says.
func (r uiRenderer) column(n tools.UINode, room uiRoom, bg string) uiBox {
	stretch := n.Align == "stretch"
	var children []tools.UINode
	for _, child := range n.Children {
		if child.Shown() {
			children = append(children, child)
		}
	}
	across := func(child tools.UINode) bool { return stretch || child.Grow > 0 }
	boxes := make([]uiBox, len(children))
	used, shares := n.Gap*max(len(children)-1, 0), 0
	for i, child := range children {
		boxes[i] = r.render(natural(child, false), uiRoom{width: room.width, fillWidth: across(child)}, bg)
		used += boxes[i].height()
		shares += growth(child, child.Height)
	}
	if left := room.height - used; shares > 0 && left > 0 {
		for i, child := range children {
			if share := growth(child, child.Height); share > 0 {
				rows := boxes[i].height() + left*share/shares
				boxes[i] = r.render(child, uiRoom{width: room.width, height: rows, fillWidth: across(child), fillHeight: true}, bg)
			}
		}
	}
	widest := 0
	for _, box := range boxes {
		widest = max(widest, box.width)
	}
	if room.fillWidth || stretch {
		widest = room.width
	}
	var out uiBox
	out.width = widest
	shown := 0
	for _, box := range boxes {
		if box.height() == 0 {
			continue
		}
		if shown > 0 {
			for range n.Gap {
				out.lines = append(out.lines, blank(widest, bg))
			}
		}
		shown++
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
// room left given to those that grow, then spread as justify says. Those
// full in height, or all with align stretch, take the row's height; in a
// row of a set height, the others have it to give their own that grow.
func (r uiRenderer) row(n tools.UINode, room uiRoom, bg string) uiBox {
	width, fill := room.width, room.fillWidth
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
		boxes[i] = r.render(natural(child, true), uiRoom{width: width}, bg)
		used += boxes[i].width
	}
	if used+gaps > width && used > 0 {
		room := max(width-gaps, len(children))
		widths := sumWidths(boxes)
		used = 0
		for i, child := range children {
			share := max(boxes[i].width*room/widths, 1)
			boxes[i] = r.render(natural(child, true), uiRoom{width: share}, bg)
			used += boxes[i].width
		}
	}
	left := max(width-used-gaps, 0)
	shares := 0
	for _, child := range children {
		shares += growth(child, child.Width)
	}
	if shares > 0 && left > 0 {
		given := 0
		for i, child := range children {
			share := growth(child, child.Width)
			if share == 0 {
				continue
			}
			extra := left * share / shares
			given += extra
			boxes[i] = r.render(child, uiRoom{width: boxes[i].width + extra, fillWidth: true}, bg)
		}
		used += given
		left = max(width-used-gaps, 0)
	}
	total := width
	if !fill && shares == 0 && (n.Justify == "" || n.Justify == "start") {
		total = used + gaps
		left = 0
	}
	height := 0
	for _, box := range boxes {
		height = max(height, box.height())
	}
	if room.fillHeight {
		height = max(height, room.height)
	}
	for i, child := range children {
		fills := child.Height.Full() || n.Align == "stretch"
		if room.fillHeight || fills {
			boxes[i] = r.render(child, uiRoom{width: boxes[i].width, height: height, fillWidth: true, fillHeight: fills}, bg)
		}
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
		widths[column] = max(widths[column], r.render(cell, uiRoom{width: width}, bg).width)
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
			boxes[column] = r.render(cell, uiRoom{width: widths[column], fillWidth: stretch || grows[column] > 0 || fixed(column) > 0}, bg)
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
