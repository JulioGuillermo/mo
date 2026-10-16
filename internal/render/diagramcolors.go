package render

import (
	"fmt"
	"sort"
	"strings"
)

// rgb is a colour of a diagram.
type rgb struct{ r, g, b int }

func hexColor(text string) rgb {
	var c rgb
	fmt.Sscanf(strings.TrimPrefix(text, "#"), "%02x%02x%02x", &c.r, &c.g, &c.b)
	return c
}

// mix is c a share of the way over base.
func (c rgb) mix(base rgb, share float64) rgb {
	blend := func(a, b int) int { return int(float64(b) + (float64(a)-float64(b))*share + 0.5) }
	return rgb{blend(c.r, base.r), blend(c.g, base.g), blend(c.b, base.b)}
}

func (c rgb) fg() string { return fmt.Sprintf("38;2;%d;%d;%d", c.r, c.g, c.b) }
func (c rgb) bg() string { return fmt.Sprintf("48;2;%d;%d;%d", c.r, c.g, c.b) }

// diagramPalette are the colours a diagram is drawn in: a box's edge in
// turn, a shade of it inside, and the lines, labels, text and groups.
type diagramPalette struct {
	edges                        []rgb
	base, line, label, text, box rgb
	fill                         float64
}

func paletteFor(dark bool) diagramPalette {
	if dark {
		return diagramPalette{
			edges: []rgb{hexColor("#4a9fe0"), hexColor("#e8833a"), hexColor("#d6409f"), hexColor("#a6c94a"),
				hexColor("#9d7cd8"), hexColor("#2ab5a0"), hexColor("#e0af68")},
			base: hexColor("#1a1b26"), line: hexColor("#4b5578"), label: hexColor("#a9b1d6"),
			text: hexColor("#d5daf0"), box: hexColor("#565f89"), fill: 0.12,
		}
	}
	return diagramPalette{
		edges: []rgb{hexColor("#2e7de9"), hexColor("#c2570a"), hexColor("#c0267f"), hexColor("#5d7a1c"),
			hexColor("#7847bd"), hexColor("#118c74"), hexColor("#8c6c00")},
		base: hexColor("#ffffff"), line: hexColor("#8990b3"), label: hexColor("#4c5272"),
		text: hexColor("#343b58"), box: hexColor("#a1a6c5"), fill: 0.08,
	}
}

// diagramBox is a rectangle of box characters drawn: its corners' rows
// and columns.
type diagramBox struct{ top, left, bottom, right int }

func (b diagramBox) holds(other diagramBox) bool {
	return b != other && other.top > b.top && other.bottom < b.bottom && other.left > b.left && other.right < b.right
}

func (b diagramBox) border(row, column int) bool {
	return row == b.top || row == b.bottom || column == b.left || column == b.right
}

func (b diagramBox) covers(row, column int) bool {
	return row >= b.top && row <= b.bottom && column >= b.left && column <= b.right
}

// findBoxes are every rectangle of box characters in a grid, edges met on
// their sides allowed.
func findBoxes(grid [][]rune) []diagramBox {
	at := func(row, column int) rune {
		if row < 0 || row >= len(grid) || column < 0 || column >= len(grid[row]) {
			return 0
		}
		return grid[row][column]
	}
	across := func(r rune) bool { return strings.ContainsRune("─┬┴┼", r) }
	down := func(r rune) bool { return strings.ContainsRune("│├┤┼", r) }
	var boxes []diagramBox
	for top, line := range grid {
		for left, character := range line {
			if character != '┌' {
				continue
			}
			right := left + 1
			for across(at(top, right)) {
				right++
			}
			if at(top, right) != '┐' {
				continue
			}
			bottom := top + 1
			for down(at(bottom, left)) {
				bottom++
			}
			if at(bottom, left) != '└' || at(bottom, right) != '┘' {
				continue
			}
			whole := true
			for row := top + 1; row < bottom && whole; row++ {
				whole = down(at(row, right))
			}
			for column := left + 1; column < right && whole; column++ {
				whole = across(at(bottom, column))
			}
			if whole {
				boxes = append(boxes, diagramBox{top, left, bottom, right})
			}
		}
	}
	return boxes
}

// colorDiagram paints a diagram drawn with box characters as a picture of
// one would be: each box's edge a colour of its own over a shade of it,
// the boxes that hold others quieter, the lines dim and their labels
// light.
func colorDiagram(drawn string, dark bool) string {
	palette := paletteFor(dark)
	lines := strings.Split(strings.TrimRight(drawn, "\n"), "\n")
	grid := make([][]rune, len(lines))
	for i, line := range lines {
		grid[i] = []rune(line)
	}
	boxes := findBoxes(grid)
	sort.Slice(boxes, func(i, j int) bool {
		if boxes[i].top != boxes[j].top {
			return boxes[i].top < boxes[j].top
		}
		return boxes[i].left < boxes[j].left
	})
	var leaves, groups []diagramBox
	for _, box := range boxes {
		group := false
		for _, other := range boxes {
			if box.holds(other) {
				group = true
				break
			}
		}
		if group {
			groups = append(groups, box)
		} else {
			leaves = append(leaves, box)
		}
	}
	lineMarks := markLines(grid)
	style := func(row, column int, character rune) string {
		for i, box := range leaves {
			if !box.covers(row, column) {
				continue
			}
			edge := palette.edges[i%len(palette.edges)]
			fill := edge.mix(palette.base, palette.fill).bg()
			if box.border(row, column) {
				return edge.fg()
			}
			return palette.text.fg() + ";" + fill
		}
		for _, box := range groups {
			if box.covers(row, column) && box.border(row, column) {
				return palette.box.fg()
			}
		}
		switch {
		case character == ' ':
			return ""
		case lineMarks[row][column]:
			return palette.line.fg()
		}
		return palette.label.fg()
	}
	var out strings.Builder
	for row, line := range grid {
		if row > 0 {
			out.WriteByte('\n')
		}
		current := ""
		for column, character := range line {
			next := style(row, column, character)
			if next != current {
				if current != "" {
					out.WriteString("\x1b[m")
				}
				if next != "" {
					out.WriteString("\x1b[" + next + "m")
				}
				current = next
			}
			if labelSpace(line, column, lineMarks[row]) {
				character = ' '
			}
			out.WriteRune(character)
		}
		if current != "" {
			out.WriteString("\x1b[m")
		}
	}
	return out.String()
}

// lineRunes are what lines and arrows are drawn with; marks are what can
// be a line's end, as an ER diagram's o{ or ||, or a letter of a label.
const (
	lineRunes = "─│┌┐└┘├┤┬┴┼┄┈┆┊╌╎═║►◄▲▼"
	lineEnds  = "<>^v|o{}*x"
)

// markLines tells which cells are of lines: every line rune, and the marks
// standing alone or together between lines, not within a label's words.
func markLines(grid [][]rune) [][]bool {
	marks := make([][]bool, len(grid))
	for row, line := range grid {
		marks[row] = make([]bool, len(line))
		for start := 0; start < len(line); {
			character := line[start]
			if strings.ContainsRune(lineRunes, character) {
				marks[row][start] = true
				start++
				continue
			}
			if character == ' ' {
				start++
				continue
			}
			end := start
			for end < len(line) && line[end] != ' ' && !strings.ContainsRune(lineRunes, line[end]) {
				end++
			}
			ends := true
			for _, each := range line[start:end] {
				ends = ends && strings.ContainsRune(lineEnds, each)
			}
			for column := start; column < end; column++ {
				marks[row][column] = ends
			}
			start = end
		}
	}
	return marks
}

// labelSpace tells a line drawn between two words of a label, where the
// label had a space: mermaid-ascii writes labels over their line.
func labelSpace(line []rune, column int, marks []bool) bool {
	if column == 0 || column >= len(line)-1 || !strings.ContainsRune("─┄┈╌", line[column]) {
		return false
	}
	word := func(at int) bool { return line[at] != ' ' && !marks[at] }
	return word(column-1) && word(column+1)
}
