package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// flashTime is how long the status line says what was copied.
var flashTime = 2 * time.Second

const (
	copyTimeout = 2 * time.Second
	// edgeScrollEvery is how often a drag held at the conversation's top
	// or bottom scrolls it a line on.
	edgeScrollEvery = 50 * time.Millisecond
	everyColumn     = 1 << 30
	selectionMark   = "selected"
)

// point is a cell of the screen.
type point struct{ x, y int }

// selection is text being picked with the mouse, from where the button went
// down to where it is, within the column it started in: the conversation or
// the side panel. One started in the conversation counts its rows in the
// conversation's lines, not the screen's, so it holds while the
// conversation scrolls under it.
type selection struct {
	from, to       point
	left, right    int
	dragged        bool
	clickOnRelease bool
	inConversation bool
	// at is where the mouse last was on the screen; edge is the way a drag
	// held at the conversation's top or bottom scrolls it, while scrolling
	// tells a tick is on its way.
	at        point
	edge      int
	scrolling bool
}

// edgeScrollMsg scrolls the conversation on while a drag is held at its
// edge.
type edgeScrollMsg struct{}

type flashDoneMsg int

// press starts a selection; it is a click if the mouse lets go where it
// went down.
func (m model) press(x, y int) (tea.Model, tea.Cmd) {
	left, right := 0, everyColumn
	if m.sidebarShown() && y < m.height-2 {
		if x > m.mainWidth() {
			left = m.mainWidth() + 1
		} else {
			right = m.mainWidth()
		}
	}
	picked := &selection{from: point{x, y}, to: point{x, y}, left: left, right: right, at: point{x, y}}
	if y < m.conversationHeight() && left == 0 {
		offset := m.conversationOffset()
		picked.inConversation = true
		picked.from.y, picked.to.y = y+offset, y+offset
	}
	m.selected = picked
	return m, nil
}

// conversationOffset is how many of the conversation's lines are
// scrolled past, above the screen.
func (m model) conversationOffset() int {
	conversation := m.conversation(m.conversationHeight())
	return conversation.YOffset()
}

func (m model) drag(x, y int) (tea.Model, tea.Cmd) {
	if m.selected == nil {
		return m, nil
	}
	moved := *m.selected
	moved.at = point{x, y}
	x = min(max(x, moved.left), moved.right-1)
	var cmd tea.Cmd
	if moved.inConversation {
		height := m.conversationHeight()
		moved.edge = 0
		switch {
		case y <= 0:
			moved.edge = -1
		case y >= height-1:
			moved.edge = 1
		}
		if moved.edge != 0 && !moved.scrolling {
			moved.scrolling = true
			cmd = edgeScrollTick()
		}
		moved.to = point{x, min(max(y, 0), height-1) + m.conversationOffset()}
	} else {
		moved.to = point{x, max(y, 0)}
	}
	moved.dragged = moved.dragged || moved.to != moved.from
	m.selected = &moved
	return m, cmd
}

func edgeScrollTick() tea.Cmd {
	return tea.Tick(edgeScrollEvery, func(time.Time) tea.Msg { return edgeScrollMsg{} })
}

// edgeScroll scrolls the conversation a line the way a drag held at its
// edge points, and the selection with it, while the button is down.
func (m model) edgeScroll() (tea.Model, tea.Cmd) {
	if m.selected == nil || !m.selected.inConversation || m.selected.edge == 0 {
		if m.selected != nil {
			stopped := *m.selected
			stopped.scrolling = false
			m.selected = &stopped
		}
		return m, nil
	}
	moved := *m.selected
	m.scroll(moved.edge < 0, 1)
	height := m.conversationHeight()
	moved.to.y = min(max(moved.at.y, 0), height-1) + m.conversationOffset()
	moved.dragged = moved.dragged || moved.to != moved.from
	m.selected = &moved
	return m, edgeScrollTick()
}

// release copies what was dragged over, or clicks where nothing was.
func (m model) release(x, y int) (tea.Model, tea.Cmd) {
	picked := m.selected
	m.selected = nil
	if picked == nil {
		// The press went elsewhere, as to a menu that took it.
		return m, nil
	}
	if !picked.dragged {
		return m.click(x, y)
	}
	text := m.selectedText(*picked)
	if strings.TrimSpace(text) == "" {
		return m, nil
	}
	m.flashes++
	m.flash = fmt.Sprintf("copied %d characters", len([]rune(text)))
	number := m.flashes
	return m, tea.Batch(tea.SetClipboard(text), copyToSystem(text),
		tea.Tick(flashTime, func(time.Time) tea.Msg { return flashDoneMsg(number) }))
}

// span is the columns of a row a selection covers, as it reads: from its
// start to the end of the first row, whole rows between, to its end.
func (s selection) span(y int) (int, int, bool) {
	from, to := s.from, s.to
	if to.y < from.y || (to.y == from.y && to.x < from.x) {
		from, to = to, from
	}
	if y < from.y || y > to.y {
		return 0, 0, false
	}
	left, right := s.left, s.right
	if y == from.y {
		left = from.x
	}
	if y == to.y {
		right = to.x + 1
	}
	return left, right, left < right
}

// highlight shows the selection on the drawn screen.
func (m model) highlight(screen string) string {
	if m.selected == nil || !m.selected.dragged {
		return screen
	}
	shade := lipgloss.NewStyle().Reverse(true)
	lines := strings.Split(screen, "\n")
	offset, height := 0, len(lines)
	if m.selected.inConversation {
		offset, height = m.conversationOffset(), m.conversationHeight()
	}
	for y, line := range lines {
		if y >= height {
			break
		}
		left, right, covered := m.selected.span(y + offset)
		if !covered {
			continue
		}
		right = min(right, ansi.StringWidth(line))
		if left >= right {
			continue
		}
		lines[y] = ansi.Cut(line, 0, left) + shade.Render(ansi.Strip(ansi.Cut(line, left, right))) + ansi.Cut(line, right, everyColumn)
	}
	return strings.Join(lines, "\n")
}

// selectedText is the text under a selection, each row without its
// trailing blanks and the block without the indent all its rows share;
// inside a code block of an answer, as the model wrote it.
func (m model) selectedText(picked selection) string {
	screen, _ := m.screenLayout()
	if picked.inConversation {
		// The whole conversation: the selection may reach past the screen.
		screen = m.conversationContent()
	}
	var rows []string
	firstStart := -1
	for y, line := range strings.Split(ansi.Strip(screen), "\n") {
		left, right, covered := picked.span(y)
		if !covered {
			continue
		}
		if firstStart < 0 {
			firstStart = left - picked.left
		}
		rows = append(rows, strings.TrimRight(ansi.Cut(line, left, min(right, ansi.StringWidth(line))), " "))
	}
	shown := strings.Join(dedent(rows, max(firstStart, 0)), "\n")
	if picked.inConversation && m.printer != nil {
		return m.printer.asWritten(shown)
	}
	return shown
}

// dedent takes off the indent every row shares. The first row may start
// inside its line, offset columns in: its indent counts from the line's
// start, so a block picked from its first word keeps its shape.
func dedent(rows []string, offset int) []string {
	indentOf := func(i int) int {
		indent := len(rows[i]) - len(strings.TrimLeft(rows[i], " "))
		if i == 0 {
			indent += offset
		}
		return indent
	}
	shared := -1
	for i, row := range rows {
		if strings.TrimSpace(row) != "" && (shared < 0 || indentOf(i) < shared) {
			shared = indentOf(i)
		}
	}
	for i, row := range rows {
		cut := shared
		if i == 0 {
			cut = shared - offset
		}
		if cut > 0 && len(row) >= cut {
			rows[i] = row[cut:]
		}
	}
	return rows
}
