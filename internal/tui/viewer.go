package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/tools"
)

const (
	viewerRefresh = 300 * time.Millisecond
	viewerShift   = 8
	viewerHelp    = "↑↓ pgup pgdn home end scroll · ←→ long lines · / search · n N next · drag or y copy · b background · x kill · esc close"
)

var (
	viewerMatch   = lipgloss.NewStyle().Reverse(true)
	viewerCurrent = lipgloss.NewStyle().Background(warning).Foreground(lipgloss.Color("#1a1b26"))
	viewerPicked  = lipgloss.NewStyle().Reverse(true)
)

// outputViewer shows a job's output over the whole screen, as a terminal
// would: it follows the end while the job runs and the view is at the
// bottom, scrolls, searches and copies.
type outputViewer struct {
	id      int
	status  tools.JobStatus
	lines   []string
	top     int
	left    int
	follow  bool
	query   string
	typing  bool
	typed   string
	matches []int
	current int
	// picked are the lines dragged over, to copy, from and to.
	picking      bool
	pickFrom     int
	pickTo       int
	notice       string
	refreshAfter int
}

type viewerTickMsg int

// openViewer shows a job's output live.
func (m model) openViewer(id int) (tea.Model, tea.Cmd) {
	viewer := &outputViewer{id: id, follow: true, pickFrom: -1}
	m.viewer = viewer
	if err := m.reloadViewer(); err != nil {
		m.viewer = nil
		return m.emitError(err)
	}
	return m, m.tickViewer()
}

func (m model) tickViewer() tea.Cmd {
	if m.viewer == nil {
		return nil
	}
	m.viewer.refreshAfter++
	number := m.viewer.refreshAfter
	return tea.Tick(viewerRefresh, func(time.Time) tea.Msg { return viewerTickMsg(number) })
}

// reloadViewer reads the job's output again, keeping the place unless the
// view follows the end.
func (m model) reloadViewer() error {
	jobs := m.jobs()
	if jobs == nil {
		return fmt.Errorf("no jobs here")
	}
	status, text, err := jobs.Text(m.viewer.id)
	if err != nil {
		return err
	}
	v := m.viewer
	v.status = status
	v.lines = strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	if text == "" {
		v.lines = nil
	}
	v.findMatches()
	if v.follow {
		v.top = max(len(v.lines)-m.viewerRows(), 0)
	}
	v.top = min(v.top, max(len(v.lines)-1, 0))
	return nil
}

// viewerRows is how many lines of output fit between the header and the
// help.
func (m model) viewerRows() int {
	return max(m.height-2, 1)
}

func (v *outputViewer) findMatches() {
	v.matches = nil
	if v.query == "" {
		return
	}
	re, err := regexp.Compile("(?i)" + v.query)
	if err != nil {
		re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(v.query))
	}
	for i, line := range v.lines {
		if re.MatchString(line) {
			v.matches = append(v.matches, i)
		}
	}
	v.current = min(v.current, max(len(v.matches)-1, 0))
}

func (v *outputViewer) queryRegexp() *regexp.Regexp {
	re, err := regexp.Compile("(?i)" + v.query)
	if err != nil {
		return regexp.MustCompile("(?i)" + regexp.QuoteMeta(v.query))
	}
	return re
}

// scrollViewer moves the view by lines; reaching the end follows it again.
func (m model) scrollViewer(by int) {
	v := m.viewer
	bottom := max(len(v.lines)-m.viewerRows(), 0)
	v.top = min(max(v.top+by, 0), bottom)
	v.follow = v.top == bottom
}

// showMatch puts the current match in view.
func (m model) showMatch() {
	v := m.viewer
	if len(v.matches) == 0 {
		v.notice = "no match for " + v.query
		return
	}
	line := v.matches[v.current]
	v.notice = fmt.Sprintf("match %d of %d", v.current+1, len(v.matches))
	if line < v.top || line >= v.top+m.viewerRows() {
		v.top = max(line-m.viewerRows()/2, 0)
	}
	v.follow = false
}

func (m model) updateViewer(msg tea.Msg) (tea.Model, tea.Cmd) {
	v := m.viewer
	switch msg := msg.(type) {
	case viewerTickMsg:
		if int(msg) != v.refreshAfter {
			return m, nil
		}
		running := v.status.State == tools.JobRunning
		if err := m.reloadViewer(); err != nil {
			v.notice = err.Error()
		}
		if running {
			return m, m.tickViewer()
		}
		return m, nil
	case tea.WindowSizeMsg:
		redraw := m.resize(msg)
		if v.follow {
			v.top = max(len(v.lines)-m.viewerRows(), 0)
		}
		return m, redraw
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.scrollViewer(-3)
		} else {
			m.scrollViewer(3)
		}
		return m, nil
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && msg.Y >= 1 && msg.Y <= m.viewerRows() {
			line := min(v.top+msg.Y-1, max(len(v.lines)-1, 0))
			v.picking, v.pickFrom, v.pickTo = true, line, line
		}
		return m, nil
	case tea.MouseMotionMsg:
		if v.picking {
			v.pickTo = min(max(v.top+msg.Y-1, 0), max(len(v.lines)-1, 0))
			switch {
			case msg.Y < 1:
				m.scrollViewer(-1)
			case msg.Y > m.viewerRows():
				m.scrollViewer(1)
			}
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if !v.picking {
			return m, nil
		}
		v.picking = false
		from, to := min(v.pickFrom, v.pickTo), max(v.pickFrom, v.pickTo)
		if len(v.lines) == 0 {
			return m, nil
		}
		return m, m.copyViewer(strings.Join(v.lines[from:to+1], "\n"), fmt.Sprintf("copied lines %d-%d", from+1, to+1))
	case tea.KeyPressMsg:
		if v.typing {
			return m.typeQuery(msg)
		}
		return m.viewerKey(msg.String())
	}
	return m, nil
}

func (m model) typeQuery(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.viewer
	switch msg.String() {
	case "enter":
		v.typing, v.query, v.current = false, v.typed, 0
		v.findMatches()
		if v.query == "" {
			v.notice = ""
			return m, nil
		}
		v.current = 0
		for i, line := range v.matches {
			if line >= v.top {
				v.current = i
				break
			}
		}
		m.showMatch()
	case "esc":
		v.typing = false
	case "backspace":
		if runes := []rune(v.typed); len(runes) > 0 {
			v.typed = string(runes[:len(runes)-1])
		}
	default:
		if msg.Text != "" {
			v.typed += msg.Text
		}
	}
	return m, nil
}

func (m model) viewerKey(pressed string) (tea.Model, tea.Cmd) {
	v := m.viewer
	rows := m.viewerRows()
	switch pressed {
	case "esc", "q", "ctrl+c":
		if pressed == "esc" && v.query != "" {
			v.query, v.matches, v.notice = "", nil, ""
			return m, nil
		}
		m.viewer = nil
		return m, nil
	case "up", "k":
		m.scrollViewer(-1)
	case "down", "j":
		m.scrollViewer(1)
	case "pgup", "shift+up":
		m.scrollViewer(-rows)
	case "pgdown", "shift+down", "space":
		m.scrollViewer(rows)
	case "home", "g":
		m.scrollViewer(-len(v.lines))
	case "end", "G":
		m.scrollViewer(len(v.lines))
	case "left", "h":
		v.left = max(v.left-viewerShift, 0)
	case "right", "l":
		v.left += viewerShift
	case "/":
		v.typing, v.typed = true, v.query
	case "n", "N":
		if len(v.matches) > 0 {
			step := 1
			if pressed == "N" {
				step = len(v.matches) - 1
			}
			v.current = (v.current + step) % len(v.matches)
			m.showMatch()
		}
	case "y":
		return m, m.copyViewer(strings.Join(v.lines, "\n"), fmt.Sprintf("copied all %d lines", len(v.lines)))
	case "b":
		if err := m.jobs().DetachJob(v.id); err != nil {
			v.notice = err.Error()
			return m, nil
		}
		v.notice = "goes on in the background; the model was let go on"
	case "x":
		jobs, id := m.jobs(), v.id
		v.notice = "killing…"
		return m, func() tea.Msg {
			if err := jobs.Kill(id); err != nil {
				return printMsg(m.printer.Error(err))
			}
			return printMsg(m.printer.Note(fmt.Sprintf("job %d killed", id)))
		}
	}
	return m, nil
}

func (m model) copyViewer(text, done string) tea.Cmd {
	m.viewer.notice = done
	return tea.Batch(tea.SetClipboard(text), copyToSystem(text))
}

// viewerView is the header, the lines in view and the help or the search
// being typed.
func (m model) viewerView() string {
	v := m.viewer
	width := max(m.width, 20)
	rows := m.viewerRows()
	header := toolName.Render(fmt.Sprintf("Job %d", v.id)) + " " + dimStyle.Render(oneLine(v.status.Describe(), width-8))
	var out []string
	out = append(out, header)
	var re *regexp.Regexp
	if v.query != "" {
		re = v.queryRegexp()
	}
	current := -1
	if len(v.matches) > 0 {
		current = v.matches[v.current]
	}
	from, to := min(v.pickFrom, v.pickTo), max(v.pickFrom, v.pickTo)
	for i := v.top; i < v.top+rows; i++ {
		if i >= len(v.lines) {
			if i == 0 {
				out = append(out, dimStyle.Render("(no output yet)"))
			} else {
				out = append(out, "")
			}
			continue
		}
		line := ansi.Strip(strings.ReplaceAll(v.lines[i], "\t", "    "))
		line = ansi.Cut(line, v.left, v.left+width)
		switch {
		case v.picking && i >= from && i <= to:
			line = viewerPicked.Render(line)
		case re != nil:
			style := viewerMatch
			if i == current {
				style = viewerCurrent
			}
			line = re.ReplaceAllStringFunc(line, func(found string) string { return style.Render(found) })
		}
		out = append(out, line)
	}
	footer := viewerHelp
	switch {
	case v.typing:
		footer = keyStyle.Render("/") + v.typed + "▏" + dimStyle.Render("  enter search · esc cancel")
	case v.notice != "":
		footer = keyStyle.Render(v.notice) + dimStyle.Render(" · "+viewerHelp)
	}
	position := fmt.Sprintf(" %d-%d/%d", min(v.top+1, len(v.lines)), min(v.top+rows, len(v.lines)), len(v.lines))
	if v.follow && v.status.State == tools.JobRunning {
		position += " following"
	}
	out = append(out, ansi.Truncate(dimStyle.Render(footer), width-len(position)-1, "…")+dimStyle.Render(position))
	return strings.Join(out, "\n")
}
