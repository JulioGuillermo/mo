package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// flashTime is how long the status line says what was copied.
var flashTime = 2 * time.Second

const (
	copyTimeout   = 2 * time.Second
	everyColumn   = 1 << 30
	selectionMark = "selected"
)

// point is a cell of the screen.
type point struct{ x, y int }

// selection is text being picked with the mouse, from where the button went
// down to where it is, within the column it started in: the conversation or
// the side panel.
type selection struct {
	from, to       point
	left, right    int
	dragged        bool
	clickOnRelease bool
}

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
	m.selected = &selection{from: point{x, y}, to: point{x, y}, left: left, right: right}
	return m, nil
}

func (m model) drag(x, y int) (tea.Model, tea.Cmd) {
	if m.selected == nil {
		return m, nil
	}
	moved := *m.selected
	moved.to = point{min(max(x, moved.left), moved.right-1), max(y, 0)}
	moved.dragged = moved.dragged || moved.to != moved.from
	m.selected = &moved
	return m, nil
}

// release copies what was dragged over, or clicks where nothing was.
func (m model) release(x, y int) (tea.Model, tea.Cmd) {
	picked := m.selected
	m.selected = nil
	if picked == nil || !picked.dragged {
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
	for y, line := range lines {
		left, right, covered := m.selected.span(y)
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
// trailing blanks and the block without the indent all its rows share.
func (m model) selectedText(picked selection) string {
	screen, _ := m.screenLayout()
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
	return strings.Join(dedent(rows, max(firstStart, 0)), "\n")
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

// copyToSystem also puts the text on the desktop's clipboard, for terminals
// that do not take OSC 52.
func copyToSystem(text string) tea.Cmd {
	var name string
	var args []string
	switch {
	case runtime.GOOS == "darwin":
		name = "pbcopy"
	case os.Getenv("WAYLAND_DISPLAY") != "" && found("wl-copy"):
		name = "wl-copy"
	case os.Getenv("DISPLAY") != "" && found("xclip"):
		name, args = "xclip", []string{"-selection", "clipboard"}
	default:
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, name, args...)
		command.Stdin = strings.NewReader(text)
		_ = command.Run()
		return nil
	}
}
