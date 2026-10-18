package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// conversationPane is the conversation as the screen shows it, height
// lines from offset. Only the lines in view are put together and measured,
// so a long conversation costs no more to draw than a short one: the
// others are only counted.
type conversationPane struct {
	width, height int
	offset, total int
	// window is the lines from one line to before another, as they read.
	window func(from, to int) []string
}

func (p conversationPane) YOffset() int        { return p.offset }
func (p conversationPane) Height() int         { return p.height }
func (p conversationPane) TotalLineCount() int { return p.total }

func (p conversationPane) maxOffset() int {
	return max(p.total-p.height, 0)
}

func (p *conversationPane) SetYOffset(offset int) {
	p.offset = min(max(offset, 0), p.maxOffset())
}

func (p *conversationPane) ScrollUp(lines int)   { p.SetYOffset(p.offset - lines) }
func (p *conversationPane) ScrollDown(lines int) { p.SetYOffset(p.offset + lines) }
func (p *conversationPane) GotoBottom()          { p.offset = p.maxOffset() }
func (p conversationPane) AtBottom() bool        { return p.offset >= p.maxOffset() }

// View is the lines in view, each cut or filled to the width, and blank
// lines under them down to the height.
func (p conversationPane) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}
	lines := p.window(p.offset, min(p.offset+p.height, p.total))
	blank := strings.Repeat(" ", p.width)
	out := make([]string, p.height)
	for i := range out {
		if i >= len(lines) {
			out[i] = blank
			continue
		}
		line := lines[i]
		width := ansi.StringWidth(line)
		if width > p.width {
			line, width = ansi.Truncate(line, p.width, ""), p.width
		}
		out[i] = line + strings.Repeat(" ", p.width-width)
	}
	return strings.Join(out, "\n")
}

// conversationLines are the parts the conversation is made of: each block
// followed by a new line, then the reply being written, with the new lines
// at the very end left out. It gives the number of lines and those of a
// window without joining the parts.
type conversationLines struct {
	parts []string
	// lazy are the rows of code the screen paints when it shows them, of the
	// parts that have them: nil for the others.
	lazy []*lazyRows
	// starts are the line each part starts on: every part but the last
	// ends with a new line.
	starts []int
	total  int
}

func (m model) conversationLines() conversationLines {
	parts := make([]string, 0, len(m.blocks)+1)
	lazy := make([]*lazyRows, 0, len(m.blocks)+1)
	for _, b := range m.blocks {
		parts, lazy = append(parts, b.text), append(lazy, b.lazy)
	}
	parts, lazy = append(parts, m.liveText()), append(lazy, nil)
	lines := conversationLines{parts: parts, lazy: lazy, starts: make([]int, len(parts))}
	line := 0
	for i, part := range parts {
		lines.starts[i] = line
		line += strings.Count(part, "\n") + 1
	}
	// The trailing new lines are cut, each taking a line with it.
	trailing := 0
	for i := len(parts) - 1; i >= 0; i-- {
		if i < len(parts)-1 {
			trailing++ // the new line after a block
		}
		text := parts[i]
		kept := strings.TrimRight(text, "\n")
		trailing += len(text) - len(kept)
		if kept != "" {
			break
		}
	}
	lines.total = max(line-trailing, 1)
	return lines
}

// blockAt is the block line row belongs to; the reply being written is
// none.
func (c conversationLines) blockAt(row int) (int, bool) {
	if row < 0 || row >= c.total {
		return 0, false
	}
	i, found := slices.BinarySearch(c.starts, row)
	if !found {
		i--
	}
	return i, i < len(c.parts)-1
}

// window is the lines from one line to before another, the calls that run
// with their arc as it turns now.
func (c conversationLines) window(from, to int) []string {
	lines := c.lines(from, to)
	for i, line := range lines {
		lines[i] = turning(line)
	}
	return lines
}

func (c conversationLines) lines(from, to int) []string {
	var out []string
	for i, part := range c.parts {
		start := c.starts[i]
		end := c.total
		if i+1 < len(c.parts) {
			end = min(c.starts[i+1], c.total)
		}
		if end <= from || start >= to || start >= end {
			continue
		}
		low, high := max(from, start), min(to, end)
		shown := nthLines(part, low-start, high-start)
		for len(shown) < high-low {
			shown = append(shown, "")
		}
		// Where the part has rows of code that are plain in its text, those in
		// view are painted now.
		if lazy := c.lazy[i]; lazy != nil {
			first, last := max(low-start, lazy.first), min(high-start, lazy.first+lazy.count)
			if first < last {
				copy(shown[first-(low-start):], lazy.render(first-lazy.first, last-lazy.first))
			}
		}
		out = append(out, shown...)
	}
	return out
}

// nthLines is the lines from one to before another of a text, found without
// cutting the whole of it: a block of many thousands of lines costs what the
// lines it shows do.
func nthLines(text string, from, to int) []string {
	offset := 0
	for range from {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return nil
		}
		offset += next + 1
	}
	var out []string
	for line := from; line < to; line++ {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return append(out, text[offset:])
		}
		out = append(out, text[offset:offset+next])
		offset += next + 1
	}
	return out
}
