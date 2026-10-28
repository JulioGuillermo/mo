package tui

import (
	"image/color"
	"math"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
)

// tableColors are the backgrounds of a table: its header and its rows, one
// shade apart so every other row stands out without borders.
type tableColors struct {
	header, even, odd, text, code, link color.Color
}

func tablePalette(dark bool) tableColors {
	c := ui(dark)
	code := c.inlineCode
	if !dark {
		code = c.orange
	}
	return tableColors{
		header: col(orDefault(c.tableHeader, c.button)), even: col(orDefault(c.tableEven, c.body)), odd: col(orDefault(c.tableOdd, c.block)),
		text: col(c.text), code: col(code), link: col(c.accent),
	}
}

var (
	tableDelimiter = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
	codeFence      = regexp.MustCompile("^\\s*(```|~~~)")
	inlineMarkdown = regexp.MustCompile("`[^`]+`|\\*\\*[^*]+\\*\\*|__[^_]+__|\\*[^*\\s][^*]*\\*|~~[^~]+~~|\\[[^\\]]+\\]\\([^)]+\\)")
)

// markdownPart is a run of markdown lines, or a table taken out of them.
type markdownPart struct {
	text    string
	isTable bool
}

// splitTables takes the tables out of markdown, leaving code blocks alone.
func splitTables(text string) []markdownPart {
	lines := strings.Split(text, "\n")
	var parts []markdownPart
	var prose []string
	flush := func() {
		if len(prose) > 0 {
			parts = append(parts, markdownPart{text: strings.Join(prose, "\n")})
			prose = nil
		}
	}
	inFence := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if codeFence.MatchString(line) {
			inFence = !inFence
		}
		if inFence || !strings.Contains(line, "|") || i+1 >= len(lines) || !tableDelimiter.MatchString(lines[i+1]) {
			prose = append(prose, line)
			continue
		}
		end := i + 2
		for end < len(lines) && strings.Contains(lines[end], "|") && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		flush()
		parts = append(parts, markdownPart{text: strings.Join(lines[i:end], "\n"), isTable: true})
		i = end - 1
	}
	flush()
	return parts
}

// tableCells splits a row at the bars that are not inside code or escaped.
func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cell strings.Builder
	inCode, escaped := false, false
	for _, character := range line {
		switch {
		case escaped:
			cell.WriteRune(character)
			escaped = false
		case character == '\\':
			escaped = true
		case character == '`':
			inCode = !inCode
			cell.WriteRune(character)
		case character == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteRune(character)
		}
	}
	return append(cells, strings.TrimSpace(cell.String()))
}

func tableAlignments(delimiter string) []lipgloss.Position {
	var positions []lipgloss.Position
	for _, cell := range tableCells(delimiter) {
		left, right := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			positions = append(positions, lipgloss.Center)
		case right:
			positions = append(positions, lipgloss.Right)
		default:
			positions = append(positions, lipgloss.Left)
		}
	}
	return positions
}

// Table draws a markdown table as stripes: a shaded header, then rows on
// alternating backgrounds, no borders.
func (p *printer) Table(text string) string {
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	lines := strings.Split(text, "\n")
	header := tableCells(lines[0])
	alignments := tableAlignments(lines[1])
	var rows [][]string
	for _, line := range lines[2:] {
		rows = append(rows, tableCells(line))
	}
	columns := len(header)
	colors := tablePalette(dark)
	background := func(row int) color.Color {
		switch {
		case row == table.HeaderRow:
			return colors.header
		case row%2 == 0:
			return colors.even
		}
		return colors.odd
	}
	cell := func(text string, row int) string {
		if row == table.HeaderRow {
			return inlineCell(text, colors.on(colors.header), colors.header, true)
		}
		return inlineCell(text, colors, background(row), false)
	}
	shownHeader := make([]string, columns)
	for i := range columns {
		shownHeader[i] = cell(header[i], table.HeaderRow)
	}
	drawn := table.New().Headers(shownHeader...).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderHeader(false).BorderRow(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Background(background(row)).Foreground(colors.text).Padding(0, 1)
			if col < len(alignments) {
				style = style.Align(alignments[col])
			}
			return style
		})
	for r, row := range rows {
		shown := make([]string, columns)
		for i := range min(len(row), columns) {
			shown[i] = cell(row[i], r)
		}
		drawn.Row(shown...)
	}
	limit := max(width-4, 20)
	if lipgloss.Width(drawn.String()) > limit {
		drawn = drawn.Width(limit).Wrap(true)
	}
	return indent(drawn.String(), "  ")
}

// minTextContrast is the least contrast a colour of code or a link needs
// with its background to be read, as WCAG asks of large text.
const minTextContrast = 3

// on is the colours of the text on a background: code and links in the
// text's colour where their own would hardly show, as red on a red header.
func (c tableColors) on(background color.Color) tableColors {
	if contrast(c.code, background) < minTextContrast {
		c.code = c.text
	}
	if contrast(c.link, background) < minTextContrast {
		c.link = c.text
	}
	return c
}

// contrast is how far apart two colours look, from 1 to 21, as WCAG
// measures it.
func contrast(a, b color.Color) float64 {
	lighter, darker := relativeLuminance(a), relativeLuminance(b)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(channel uint32) float64 {
		v := float64(channel) / 0xffff
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// inlineCell draws the code, bold, italic, struck and links of a cell, each
// piece on the row's background so the stripe stays whole.
func inlineCell(text string, colors tableColors, background color.Color, bold bool) string {
	base := lipgloss.NewStyle().Background(background).Foreground(colors.text).Bold(bold)
	var out strings.Builder
	last := 0
	for _, match := range inlineMarkdown.FindAllStringIndex(text, -1) {
		out.WriteString(base.Render(text[last:match[0]]))
		token := text[match[0]:match[1]]
		switch {
		case strings.HasPrefix(token, "`"):
			out.WriteString(base.Foreground(colors.code).Render(strings.Trim(token, "`")))
		case strings.HasPrefix(token, "**"), strings.HasPrefix(token, "__"):
			out.WriteString(base.Bold(true).Render(token[2 : len(token)-2]))
		case strings.HasPrefix(token, "~~"):
			out.WriteString(base.Strikethrough(true).Render(token[2 : len(token)-2]))
		case strings.HasPrefix(token, "["):
			label, _, _ := strings.Cut(token[1:], "](")
			out.WriteString(base.Foreground(colors.link).Underline(true).Render(label))
		default:
			out.WriteString(base.Italic(true).Render(token[1 : len(token)-1]))
		}
		last = match[1]
	}
	out.WriteString(base.Render(text[last:]))
	return out.String()
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > 0 {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
