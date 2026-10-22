package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

const (
	historyMinWidth = 34
	historyMaxWidth = 56
	historyListHelp = "↑↓ move · enter open · / find · ctrl+n p next prev · tab diff · t hide"
	historyHelp     = "ctrl+n p next prev commit · tab list · e E on ⋯ show lines · f whole file · c comment · a attach · / search · y copy · t list · esc back"
)

// historyEntry is a commit that changed the file, or its changes not
// committed yet.
type historyEntry struct {
	commit      gitops.FileCommit
	uncommitted bool
}

func (e historyEntry) title() string {
	if e.uncommitted {
		return "not committed yet"
	}
	return fmt.Sprintf("%s %s %s · %s", e.commit.Short(), e.commit.Author, e.commit.When.Format("2006-01-02"), e.commit.Subject)
}

// fileHistory is a file's history in the viewer: its commits listed beside
// the diff of the one shown, newest first, its changes not committed
// before them. back is the viewer it was opened from, to go back to.
type fileHistory struct {
	repo    *gitops.Repo
	path    string
	full    string
	entries []historyEntry
	current int
	// listed are the entries the filter keeps, by index; cursor and top
	// are in it.
	listed      []int
	cursor, top int
	focus       bool
	hidden      bool
	filter      string
	typing      bool
	typed       string
	back        *outputViewer
}

// historyStart is where a history opens: the commit with that hash, or the
// changes not committed; and the line to put the cursor on, by its text
// or, in the file on disk, its number.
type historyStart struct {
	hash        string
	uncommitted bool
	text        string
	line        int
}

type historyReadMsg struct {
	history *fileHistory
	start   historyStart
	err     error
}

// openHistory shows the history of the file the viewer shows, from the
// commit that last changed the cursor's line.
func (m model) openHistory() (tea.Model, tea.Cmd) {
	v := m.viewer
	if v.history != nil {
		return m, nil
	}
	start := historyStart{line: -1}
	blame := v.blame
	switch {
	case v.file != "" && v.cursor < len(v.lines):
		start.text, start.line = v.lines[v.cursor], v.cursor
	case v.diff != nil:
		if edit, ok := v.diff.edit(v.cursor); ok {
			start.text = v.diff.edits[edit].Text
		}
		start.hash, start.uncommitted = v.changes.revision, v.changes.uncommitted
	}
	full := m.viewedPath()
	v.notice = "reading the history of " + filepath.Base(full) + "…"
	return m, readHistory(full, start, v, blame)
}

// gitHistory is /git history: the file's history from its last commit.
func (m model) gitHistory(argument string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(argument) == "" {
		return m.emit(m.printer.Note("usage: /git history <file>"))
	}
	return m, readHistory(m.absolutePath(strings.TrimSpace(argument)), historyStart{line: -1}, m.viewer, nil)
}

// readHistory reads a file's commits in the background, and where to
// start: what blame says of the line asked for, when one was.
func readHistory(full string, start historyStart, back *outputViewer, blame []gitops.LineBlame) tea.Cmd {
	return func() tea.Msg {
		repo, path, err := repoFile(full)
		if err != nil {
			return historyReadMsg{err: err}
		}
		commits, err := repo.FileHistory(path)
		if err != nil {
			return historyReadMsg{err: err}
		}
		h := &fileHistory{repo: repo, path: path, full: full, back: back}
		if _, changed := repo.UncommittedFile(path); changed {
			h.entries = append(h.entries, historyEntry{uncommitted: true})
		}
		for _, commit := range commits {
			h.entries = append(h.entries, historyEntry{commit: commit})
		}
		if start.line >= 0 && start.hash == "" && !start.uncommitted {
			if blame == nil {
				blame, _ = repo.BlameFile(path)
			}
			if start.line < len(blame) {
				start.hash, start.uncommitted = blame[start.line].Hash, !blame[start.line].Committed()
			}
		}
		return historyReadMsg{history: h, start: start}
	}
}

func (m model) historyRead(msg historyReadMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if m.viewer != nil {
			m.viewer.notice = "no history: " + msg.err.Error()
			return m, nil
		}
		return m.emitError(msg.err)
	}
	h := msg.history
	if len(h.entries) == 0 {
		note := filepath.Base(h.full) + " has no commits yet"
		if m.viewer != nil {
			m.viewer.notice = note
			return m, nil
		}
		return m.emit(m.printer.Note(note))
	}
	h.refilter()
	m.showHistoryEntry(h, h.startIndex(msg.start))
	m.viewer.focusLine(msg.start, m.viewerRows())
	return m, nil
}

// startIndex is the entry a history opens on: the one asked for, else the
// newest commit.
func (h *fileHistory) startIndex(start historyStart) int {
	newest := 0
	for i, entry := range h.entries {
		switch {
		case entry.uncommitted && start.uncommitted:
			return i
		case !entry.uncommitted && start.hash != "" && strings.HasPrefix(entry.commit.Hash, start.hash):
			return i
		case entry.uncommitted && newest == i:
			newest = i + 1
		}
	}
	return min(newest, len(h.entries)-1)
}

// showHistoryEntry shows the diff of one entry of the history.
func (m *model) showHistoryEntry(h *fileHistory, index int) {
	entry := h.entries[index]
	var comparison gitops.Comparison
	if entry.uncommitted {
		comparison, _ = h.repo.UncommittedFile(h.path)
	} else {
		var err error
		if comparison, err = h.repo.FileChange(entry.commit.Hash, h.path); err != nil && m.viewer != nil {
			m.viewer.notice = err.Error()
			return
		}
	}
	changes := &changeView{title: entry.title(), root: h.repo.Root(), comparison: comparison,
		revision: entry.commit.Hash, uncommitted: entry.uncommitted}
	m.openChange(changes, h.full)
	h.current = index
	if at := indexIn(h.listed, index); at >= 0 {
		h.cursor = at
	}
	m.viewer.history = h
	h.show(m.viewerRows())
}

func indexIn(list []int, value int) int {
	for i, each := range list {
		if each == value {
			return i
		}
	}
	return -1
}

// focusLine puts the cursor of a diff on the line a history opened for,
// the first change when it is not there.
func (v *outputViewer) focusLine(start historyStart, rows int) {
	if v.diff == nil {
		return
	}
	want := strings.TrimSpace(start.text)
	found := -1
	for i, edit := range v.diff.edits {
		matchesNumber := start.line >= 0 && v.changes.uncommitted && edit.Kind != '-' && edit.New == start.line
		matchesText := want != "" && edit.Kind == '+' && strings.TrimSpace(edit.Text) == want
		if matchesNumber || matchesText {
			found = i
			break
		}
	}
	if found < 0 {
		for i, edit := range v.diff.edits {
			if edit.Kind != ' ' {
				found = i
				break
			}
		}
	}
	if found < 0 {
		return
	}
	v.cursor = v.diff.rowOf(found)
	v.top = max(v.cursor-rows/2, 0)
}

// stepCommit shows the next commit, older, or the one before.
func (m model) stepCommit(by int) (tea.Model, tea.Cmd) {
	h := m.viewer.history
	at := indexIn(h.listed, h.current) + by
	if at < 0 || at >= len(h.listed) {
		m.viewer.notice = "no more commits that way"
		return m, nil
	}
	focus := h.focus
	m.showHistoryEntry(h, h.listed[at])
	h.focus = focus
	m.viewer.focusLine(historyStart{line: -1}, m.viewerRows())
	return m, nil
}

// historyViewerKey is a key of the viewer showing a history, before the
// usual ones; it tells whether it took it.
func (m model) historyViewerKey(pressed string) (bool, tea.Model, tea.Cmd) {
	v := m.viewer
	h := v.history
	switch pressed {
	case "ctrl+n":
		next, cmd := m.stepCommit(1)
		return true, next, cmd
	case "ctrl+p":
		next, cmd := m.stepCommit(-1)
		return true, next, cmd
	case "tab":
		if m.commitListShown() {
			h.focus = true
		}
		return true, m, nil
	case "t":
		h.hidden = !h.hidden
		h.focus = !h.hidden
		return true, m, nil
	case "esc", "q":
		if pressed == "esc" && v.query != "" {
			return false, m, nil
		}
		m.viewer = h.back
		return true, m, nil
	}
	return false, m, nil
}

// historyKey is a key pressed while the list of commits has the focus.
func (m model) commitListKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h := m.viewer.history
	rows := m.viewerRows()
	pressed := msg.String()
	if h.typing {
		switch pressed {
		case "enter":
			h.typing, h.filter = false, h.typed
			h.refilter()
		case "esc":
			h.typing = false
		case "backspace":
			if runes := []rune(h.typed); len(runes) > 0 {
				h.typed = string(runes[:len(runes)-1])
			}
		default:
			h.typed += msg.Text
		}
		return m, nil
	}
	switch pressed {
	case "up", "k":
		h.move(-1, rows)
	case "down", "j":
		h.move(1, rows)
	case "pgup":
		h.move(-rows, rows)
	case "pgdown":
		h.move(rows, rows)
	case "home", "g":
		h.move(-len(h.listed), rows)
	case "end", "G":
		h.move(len(h.listed), rows)
	case "enter", "right", "l", "space":
		if h.cursor < len(h.listed) {
			m.showHistoryEntry(h, h.listed[h.cursor])
			h.focus = pressed != "enter"
			m.viewer.focusLine(historyStart{line: -1}, rows)
		}
	case "/":
		h.typing, h.typed = true, h.filter
	case "esc":
		if h.filter != "" {
			h.filter = ""
			h.refilter()
			return m, nil
		}
		m.viewer = h.back
	case "q", "ctrl+c":
		m.viewer = h.back
	case "tab":
		h.focus = false
	case "t":
		h.hidden, h.focus = true, false
	case "ctrl+n":
		return m.stepCommit(1)
	case "ctrl+p":
		return m.stepCommit(-1)
	}
	return m, nil
}

// commitListClick opens the commit clicked in the list, row y of the screen.
func (m model) commitListClick(y int) (tea.Model, tea.Cmd) {
	h := m.viewer.history
	h.focus = true
	at := h.top + y - 1
	if y < 1 || at >= len(h.listed) {
		return m, nil
	}
	h.cursor = at
	m.showHistoryEntry(h, h.listed[at])
	h.focus = true
	m.viewer.focusLine(historyStart{line: -1}, m.viewerRows())
	return m, nil
}

// refilter lists the entries whose hash, author, date or subject hold the
// filter, any case, keeping the cursor on the one shown when it stays.
func (h *fileHistory) refilter() {
	query := strings.ToLower(h.filter)
	h.listed = nil
	for i, entry := range h.entries {
		if query == "" || strings.Contains(strings.ToLower(entry.commit.Hash+" "+entry.title()), query) {
			h.listed = append(h.listed, i)
		}
	}
	h.cursor = max(indexIn(h.listed, h.current), 0)
	h.top = 0
}

func (h *fileHistory) move(by, rows int) {
	h.cursor = min(max(h.cursor+by, 0), max(len(h.listed)-1, 0))
	h.show(rows)
}

// show scrolls the list so the cursor is in view.
func (h *fileHistory) show(rows int) {
	rows = max(rows, 1)
	if h.cursor < h.top {
		h.top = h.cursor
	}
	if h.cursor >= h.top+rows {
		h.top = h.cursor - rows + 1
	}
	h.top = max(min(h.top, max(len(h.listed)-rows, 0)), 0)
}

// commitListShown tells whether the list of commits is drawn beside the diff.
func (m model) commitListShown() bool {
	return m.viewer != nil && m.viewer.history != nil && !m.viewer.history.hidden && m.width >= treeNarrow
}

func (m model) commitListWidth() int {
	return min(max(m.width/3, historyMinWidth), historyMaxWidth)
}

// commitListLayout puts the list of commits on the left of the diff.
func (m model) commitListLayout(body string) string {
	h := m.viewer.history
	width := m.commitListWidth()
	rows := strings.Split(body, "\n")
	left := h.lines(width, len(rows))
	divider := dimStyle.Render("│")
	if h.focus {
		divider = treeFolder.Render("│")
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = padTo(left[min(i, len(left)-1)], width) + divider + row
	}
	return strings.Join(out, "\n")
}

// lines are height lines of the list, each width wide: the title, the
// entries from the top, the help.
func (h *fileHistory) lines(width, height int) []string {
	commits := len(h.entries)
	if h.entries[0].uncommitted {
		commits--
	}
	title := toolName.Render("history") + dimStyle.Render(fmt.Sprintf(" · %d commits", commits))
	switch {
	case h.typing:
		title = keyStyle.Render("/") + ansi.Truncate(h.typed, width-3, "") + "▏"
	case h.filter != "":
		title = keyStyle.Render("/") + ansi.Truncate(h.filter, width-12, "…") + dimStyle.Render(fmt.Sprintf(" · %d found", len(h.listed)))
	}
	out := []string{padTo(title, width)}
	for at := h.top; at < h.top+height-2; at++ {
		if at >= len(h.listed) {
			out = append(out, strings.Repeat(" ", width))
			continue
		}
		index := h.listed[at]
		out = append(out, h.row(index, at == h.cursor, width))
	}
	help := historyListHelp
	if h.typing {
		help = "enter find · esc cancel"
	}
	return append(out, padTo(dimStyle.Render(ansi.Truncate(help, width, "…")), width))
}

// row is an entry of the list: a mark on the one shown, under the cursor
// in reverse when the list has the focus.
func (h *fileHistory) row(index int, cursor bool, width int) string {
	entry := h.entries[index]
	mark := "  "
	if index == h.current {
		mark = "▶ "
	}
	text := mark + "● not committed yet"
	if !entry.uncommitted {
		commit := entry.commit
		text = fmt.Sprintf("%s%s %s %s  %s", mark, commit.Short(), commit.When.Format("2006-01-02"), firstName(commit.Author), commit.Subject)
	}
	plain := padTo(ansi.Truncate(text, width, "…"), width)
	switch {
	case cursor && h.focus:
		return treeFocused.Render(plain)
	case cursor:
		return treeBlurred.Render(plain)
	case entry.uncommitted:
		return changeStyle(gitops.ChangeModified, index == h.current).Render(plain)
	case index == h.current:
		return treeOpened.Render(plain)
	}
	return plain
}

// firstName is the first word of an author's name, to keep rows short.
func firstName(name string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(name), " ")
	return first
}

// historyHeader is what the viewer says over a diff of the history.
func (v *outputViewer) historyHeader(width int, comments string) string {
	h := v.history
	about := fmt.Sprintf("%d of %d · %s", h.current+1, len(h.entries), h.entries[h.current].title())
	return toolName.Render(oneLine(h.path, width/3)) + " " + dimStyle.Render(oneLine(about+comments, width))
}
