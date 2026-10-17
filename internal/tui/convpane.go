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
	// starts are the line each part starts on: every part but the last
	// ends with a new line.
	starts []int
	total  int
}

func (m model) conversationLines() conversationLines {
	parts := make([]string, 0, len(m.blocks)+1)
	for _, b := range m.blocks {
		parts = append(parts, b.text)
	}
	parts = append(parts, m.liveText())
	lines := conversationLines{parts: parts, starts: make([]int, len(parts))}
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
		split := strings.Split(part, "\n")
		for row := max(from, start); row < min(to, end); row++ {
			if index := row - start; index < len(split) {
				out = append(out, split[index])
			} else {
				out = append(out, "")
			}
		}
	}
	return out
}
