package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
)

const liveReasoningLines = 3

// block is a piece of the transcript: printed text, or a reasoning of the
// model, shown as one line until it is opened.
type block struct {
	text      string
	draw      func() string
	reasoning string
	open      bool
}

type reasoningMsg string

// drawMsg prints what draw returns, and draws it again at a new width.
type drawMsg func() string

// redrawMsg asks for the redraw it numbers, done only if no resize came
// after it.
type redrawMsg int

const redrawDelay = 120 * time.Millisecond

func drawText(draw func() string) tea.Cmd {
	return func() tea.Msg { return drawMsg(draw) }
}

// emitDrawn adds a block that can be drawn again. One that uploads kitty
// images stays as first drawn, so the images are sent only once.
func (m model) emitDrawn(draw drawMsg) (tea.Model, tea.Cmd) {
	text := draw()
	if kittyUploads.MatchString(text) {
		return m.emit(text)
	}
	m.blocks = append(m.blocks, block{text: text, draw: draw})
	return m, nil
}

// scheduleRedraw redraws the transcript once the resizing has stopped for
// a moment, not at every step of it.
func (m *model) scheduleRedraw() tea.Cmd {
	m.redraws++
	number := m.redraws
	return tea.Tick(redrawDelay, func(time.Time) tea.Msg { return redrawMsg(number) })
}

func (m *model) redraw() {
	m.blocks = slices.Clone(m.blocks)
	for i, b := range m.blocks {
		if b.draw != nil {
			m.blocks[i].text = b.draw()
		}
	}
}

// reasoningStyle is the grey of reasoning, darker than the faint of notes.
func (p *printer) reasoningStyle() lipgloss.Style {
	p.mu.Lock()
	dark := p.dark
	p.mu.Unlock()
	if dark {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#9699a3"))
}

// addReasoning adds a reasoning, open or shut as the last ctrl+o left them.
func (m *model) addReasoning(text string) {
	if text = strings.TrimSpace(text); text != "" {
		m.blocks = append(m.blocks, block{reasoning: text, open: m.showReasoning})
	}
}

// transcript is the text of every block, and for each reasoning the line
// its header is on, to open it with a click.
func (m model) transcript() (string, map[int]int) {
	var out strings.Builder
	headers := map[int]int{}
	line := 0
	for i, b := range m.blocks {
		text := b.text
		if b.reasoning != "" {
			text = m.reasoningText(b.reasoning, b.open, false)
			headers[line+1] = i
		}
		out.WriteString(text)
		out.WriteByte('\n')
		line += strings.Count(text, "\n") + 1
	}
	return out.String(), headers
}

// reasoningText is a blank line and the header of a reasoning, then all of
// it when open; while it is being written, its last lines.
func (m model) reasoningText(text string, open, live bool) string {
	style := m.printer.reasoningStyle()
	lines := strings.Count(text, "\n") + 1
	wrap := lipgloss.NewStyle().Width(max(m.width-6, 20)).PaddingLeft(4)
	var header string
	switch {
	case live:
		header = "  ∴ thinking…"
	case open:
		header = "  ▾ reasoning · ctrl+o or click to fold"
	default:
		header = fmt.Sprintf("  ▸ reasoning · %d lines · ctrl+o or click to open", lines)
	}
	shown := ""
	switch {
	case open:
		shown = wrap.Render(text)
	case live:
		all := strings.Split(wrap.Render(text), "\n")
		shown = strings.Join(all[max(len(all)-liveReasoningLines, 0):], "\n")
	}
	if shown == "" {
		return "\n" + style.Render(header)
	}
	return "\n" + style.Render(header+"\n"+shown)
}

// toggleReasoning opens or folds every reasoning, and the ones to come.
func (m *model) toggleReasoning() {
	m.showReasoning = !m.showReasoning
	m.blocks = slices.Clone(m.blocks)
	for i := range m.blocks {
		m.blocks[i].open = m.showReasoning
	}
}

// clickReasoning opens or folds the reasoning whose header is at row y of
// the screen, and tells whether there was one.
func (m *model) clickReasoning(y int) bool {
	conversation := m.conversation(m.height - lipgloss.Height(m.bottom()) - 1)
	_, headers := m.transcript()
	i, found := headers[y+conversation.YOffset()]
	if !found {
		return false
	}
	m.blocks = slices.Clone(m.blocks)
	m.blocks[i].open = !m.blocks[i].open
	return true
}
