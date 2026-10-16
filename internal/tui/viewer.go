package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

const (
	viewerRefresh = 300 * time.Millisecond
	viewerShift   = 8
	viewerHelp    = "↑↓ pgup pgdn home end scroll · ←→ long lines · / search · n N next · drag or y copy · b background · x kill · esc close"
	fileHelp      = "↑↓ move · c comment the line or what you dragged over · d delete it · a attach to your message · / search · y copy · esc close"
	drawingHelp   = "drag, ↑↓ ←→ hjkl or the wheel pan · s code or drawing · / search · y copy · esc close"
	viewMaxBytes  = 8 << 20
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
	// file, when set, is the file shown instead of a job's output, and
	// colored its lines highlighted.
	file     string
	language string
	colored  []string
	// drawing, when set, names a diagram shown whole, as it was drawn;
	// dragging pans it, from where the drag started and the view then.
	drawing  string
	source   string
	asCode   bool
	drawn    [2][]string
	panning  bool
	panFrom  point
	panStart point
	// cursor is the line comments go on, selected set when lines were
	// dragged over; commenting while one is typed, on those lines.
	cursor      int
	selected    bool
	commenting  bool
	commentFrom int
	commentTo   int
	commentText string
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

// openDrawing shows a diagram whole over the screen, to scroll across what
// the conversation cuts.
func (m model) openDrawing(drawn drawing) (tea.Model, tea.Cmd) {
	lines, plain := drawn.lines, drawn.plain
	// The answer's indent goes: here the diagram starts at the left.
	indent := len(plain[0])
	for _, line := range plain {
		if strings.TrimSpace(line) != "" {
			indent = min(indent, len(line)-len(strings.TrimLeft(line, " ")))
		}
	}
	v := &outputViewer{drawing: "Diagram", source: strings.TrimSpace(drawn.source), pickFrom: -1}
	for i := range lines {
		v.lines = append(v.lines, ansi.Cut(plain[i], indent, everyColumn))
		v.colored = append(v.colored, ansi.Cut(lines[i], indent, everyColumn))
	}
	v.drawn = [2][]string{v.lines, v.colored}
	if drawn.code {
		v.toggleCode()
	}
	m.viewer = v
	return m, nil
}

// toggleCode shows a diagram's code in place of its drawing, or back.
func (v *outputViewer) toggleCode() {
	v.asCode = !v.asCode
	v.top, v.left, v.query, v.matches = 0, 0, "", nil
	if v.asCode {
		v.lines, v.colored = strings.Split(v.source, "\n"), nil
		v.drawing = "Diagram code"
		return
	}
	v.lines, v.colored = v.drawn[0], v.drawn[1]
	v.drawing = "Diagram"
}

// openFile shows a text file over the whole screen, highlighted as its
// name says, for the user only: the model never sees it.
func (m model) openFile(path string) (tea.Model, tea.Cmd) {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.session.WorkDir, path)
	}
	info, err := os.Stat(full)
	switch {
	case err != nil:
		return m.emitError(err)
	case info.IsDir():
		return m.emit(m.printer.Note(path + " is a folder: tab after /view lists what is in it"))
	case info.Size() > viewMaxBytes:
		return m.emit(m.printer.Note(fmt.Sprintf("%s is %s, over the %s /view shows", path, humanBytes(info.Size()), humanBytes(viewMaxBytes))))
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return m.emitError(err)
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return m.emit(m.printer.Note(path + " is not text"))
	}
	text := strings.TrimSuffix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	v := &outputViewer{file: path, pickFrom: -1}
	if text != "" {
		v.lines = strings.Split(text, "\n")
	}
	lexer := lexers.Match(filepath.Base(full))
	if lexer == nil {
		lexer = shebangLexer(text)
	}
	if lexer == nil {
		// Nothing in the name or the #! line: the content says, if anything.
		lexer = lexers.Analyse(text)
	}
	if lexer != nil {
		v.language = lexer.Config().Name
		colored := highlightLines(text, v.language, 1<<30, lipgloss.NoColor{}, m.printer == nil || m.printer.isDark())
		if len(colored) == len(v.lines) {
			v.colored = colored
		}
	}
	if m.fileComments == nil {
		m.fileComments = map[string][]fileComment{}
	}
	m.viewer = v
	return m, nil
}

func humanBytes(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", size)
}

// reloadViewer reads the job's output again, keeping the place unless the
// view follows the end; a file stays as it was opened.
func (m model) reloadViewer() error {
	if m.viewer.file != "" || m.viewer.drawing != "" {
		return nil
	}
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

// panViewer moves the view across by columns; a diagram stops at its
// right edge, the longest line in view.
func (m model) panViewer(by int) {
	v := m.viewer
	v.left = max(v.left+by, 0)
	if v.drawing == "" {
		return
	}
	widest := 0
	for _, line := range v.lines {
		widest = max(widest, ansi.StringWidth(line))
	}
	v.left = min(v.left, max(widest-max(m.width, 20), 0))
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
		sideways := msg.Button == tea.MouseWheelLeft || msg.Button == tea.MouseWheelRight || msg.Mod.Contains(tea.ModShift)
		switch {
		case sideways && (msg.Button == tea.MouseWheelLeft || msg.Button == tea.MouseWheelUp):
			m.panViewer(-viewerShift)
		case sideways:
			m.panViewer(viewerShift)
		case msg.Button == tea.MouseWheelUp:
			m.scrollViewer(-3)
		default:
			m.scrollViewer(3)
		}
		return m, nil
	case tea.MouseClickMsg:
		if v.drawing != "" && msg.Button == tea.MouseLeft {
			v.panning, v.panFrom, v.panStart = true, point{msg.X, msg.Y}, point{v.left, v.top}
			return m, nil
		}
		if msg.Button == tea.MouseLeft && msg.Y >= 1 && msg.Y <= m.viewerRows() {
			line := min(v.top+msg.Y-1, max(len(v.lines)-1, 0))
			v.picking, v.pickFrom, v.pickTo = true, line, line
		}
		return m, nil
	case tea.MouseMotionMsg:
		if v.panning {
			v.left = v.panStart.x
			m.panViewer(v.panFrom.x - msg.X)
			v.top = v.panStart.y
			m.scrollViewer(v.panFrom.y - msg.Y)
			return m, nil
		}
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
		if v.panning {
			v.panning = false
			return m, nil
		}
		if !v.picking {
			return m, nil
		}
		v.picking = false
		from, to := min(v.pickFrom, v.pickTo), max(v.pickFrom, v.pickTo)
		if len(v.lines) == 0 {
			return m, nil
		}
		if v.file != "" {
			v.cursor = v.pickTo
			v.selected = from != to
			if !v.selected {
				return m, nil
			}
		}
		return m, m.copyViewer(strings.Join(v.lines[from:to+1], "\n"), fmt.Sprintf("copied lines %d-%d", from+1, to+1))
	case tea.KeyPressMsg:
		switch {
		case v.commenting:
			return m.typeComment(msg)
		case v.typing:
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
		if v.file != "" {
			m.moveCursor(-1)
			return m, nil
		}
		m.scrollViewer(-1)
	case "down", "j":
		if v.file != "" {
			m.moveCursor(1)
			return m, nil
		}
		m.scrollViewer(1)
	case "c":
		if v.file != "" {
			m.startComment()
		}
	case "d":
		if comment, _, ok := m.commentAt(v.cursor); ok && v.file != "" {
			m.setComment(comment.from, comment.to, "")
		}
	case "a":
		if v.file != "" {
			return m.attachFile()
		}
	case "pgup", "shift+up":
		m.scrollViewer(-rows)
	case "pgdown", "shift+down", "space":
		m.scrollViewer(rows)
	case "home", "g":
		m.scrollViewer(-len(v.lines))
	case "end", "G":
		m.scrollViewer(len(v.lines))
	case "left", "h":
		m.panViewer(-viewerShift)
	case "right", "l":
		m.panViewer(viewerShift)
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
	case "s":
		if v.drawing != "" {
			v.toggleCode()
		}
	case "y":
		return m, m.copyViewer(strings.Join(v.lines, "\n"), fmt.Sprintf("copied all %d lines", len(v.lines)))
	case "b":
		if v.file != "" || v.drawing != "" {
			return m, nil
		}
		if err := m.jobs().DetachJob(v.id); err != nil {
			v.notice = err.Error()
			return m, nil
		}
		v.notice = "goes on in the background; the model was let go on"
	case "x":
		if v.file != "" || v.drawing != "" {
			return m, nil
		}
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
	help := viewerHelp
	gutter := 0
	if v.file != "" {
		about := fmt.Sprintf("%d lines", len(v.lines))
		if v.language != "" {
			about += " · " + v.language
		}
		switch count := len(m.comments()); count {
		case 0:
		case 1:
			about += " · 1 comment, a attaches it"
		default:
			about += fmt.Sprintf(" · %d comments, a attaches them", count)
		}
		header = toolName.Render(oneLine(v.file, width/2)) + " " + dimStyle.Render(about+" · only you see it")
		help = fileHelp
		// The comment mark, the line's number, a space.
		gutter = 1 + len(fmt.Sprint(len(v.lines))) + 1
	}
	if v.drawing != "" {
		widest := 0
		for _, line := range v.lines {
			widest = max(widest, ansi.StringWidth(line))
		}
		header = toolName.Render(v.drawing) + " " + dimStyle.Render(fmt.Sprintf("%d lines · %d columns", len(v.lines), widest))
		help = drawingHelp
	}
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
	room := max(width-gutter, 1)
	for i := v.top; i < v.top+rows; i++ {
		if i >= len(v.lines) {
			if i == 0 {
				out = append(out, dimStyle.Render("(empty)"))
			} else {
				out = append(out, "")
			}
			continue
		}
		plain := ansi.Strip(strings.ReplaceAll(v.lines[i], "\t", "    "))
		line := ansi.Cut(plain, v.left, v.left+room)
		switch {
		case v.picking && i >= from && i <= to:
			line = viewerPicked.Render(line)
		case re != nil && re.MatchString(plain):
			style := viewerMatch
			if i == current {
				style = viewerCurrent
			}
			line = re.ReplaceAllStringFunc(line, func(found string) string { return style.Render(found) })
		case (v.file != "" && i == v.cursor && !v.commenting) || (v.commenting && i >= v.commentFrom && i <= v.commentTo):
			if v.colored != nil {
				line = ansi.Cut(v.colored[i], v.left, v.left+room)
			}
			line = cursorRow(line, room)
		case v.colored != nil:
			line = ansi.Cut(v.colored[i], v.left, v.left+room)
		}
		if gutter > 0 {
			mark := " "
			if _, _, commented := m.commentAt(i); commented {
				mark = commentMark.Render("◆")
			}
			line = mark + dimStyle.Render(fmt.Sprintf("%*d ", gutter-2, i+1)) + line
		}
		out = append(out, line)
	}
	footer := help
	comment, _, onComment := m.commentAt(v.cursor)
	switch {
	case v.commenting:
		where := fmt.Sprintf("line %d", v.commentFrom+1)
		if v.commentTo > v.commentFrom {
			where = fmt.Sprintf("lines %d-%d", v.commentFrom+1, v.commentTo+1)
		}
		footer = commentMark.Render("◆ "+where+": ") + v.commentText + "▏" + dimStyle.Render("  enter keep · esc cancel · empty deletes it")
	case v.file != "" && onComment && !v.typing:
		footer = commentFooter.Render("◆ "+comment.text) + dimStyle.Render(" · c edit · d delete")
	case v.typing:
		footer = keyStyle.Render("/") + v.typed + "▏" + dimStyle.Render("  enter search · esc cancel")
	case v.notice != "":
		footer = keyStyle.Render(v.notice) + dimStyle.Render(" · "+help)
	}
	position := fmt.Sprintf(" %d-%d/%d", min(v.top+1, len(v.lines)), min(v.top+rows, len(v.lines)), len(v.lines))
	if v.follow && v.status.State == tools.JobRunning {
		position += " following"
	}
	out = append(out, ansi.Truncate(dimStyle.Render(footer), width-len(position)-1, "…")+dimStyle.Render(position))
	return strings.Join(out, "\n")
}

// drawnInline tells a file /view draws in the conversation: a picture, an
// SVG, a Mermaid diagram.
func drawnInline(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return render.IsImageFile(path) || extension == ".mmd" || extension == ".mermaid"
}

// shebangLexer is the lexer of a script's #! line: its interpreter, or the
// one env runs, without its version, as python for python3.
func shebangLexer(text string) chroma.Lexer {
	first, _, _ := strings.Cut(text, "\n")
	line, ok := strings.CutPrefix(strings.TrimSpace(first), "#!")
	if !ok {
		return nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	interpreter := filepath.Base(fields[0])
	if interpreter == "env" {
		for _, field := range fields[1:] {
			if !strings.HasPrefix(field, "-") {
				interpreter = filepath.Base(field)
				break
			}
		}
	}
	for _, name := range []string{interpreter, strings.TrimRight(interpreter, "0123456789.")} {
		if lexer := lexers.Get(name); lexer != nil {
			return lexer
		}
	}
	return nil
}
