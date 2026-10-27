package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	fileIndexAge     = 15 * time.Second
	fileIndexMost    = 20000
	mentionShown     = 30
	commentQuoteMost = 80
)

func commentMark() lipgloss.Style   { return lipgloss.NewStyle().Foreground(warning()).Bold(true) }
func commentFooter() lipgloss.Style { return lipgloss.NewStyle().Foreground(warning()) }

var (
	skippedDirs = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "__pycache__": true}
)

// fileComment is what the user said of lines of a file, from and to
// counted from 0, to send the model with it; on a diff they count the
// diff's lines, shown or not.
type fileComment struct {
	from, to int
	text     string
}

// comments are the user's comments on the file the viewer shows, in order.
func (m model) comments() []fileComment {
	if m.viewer == nil || m.fileComments == nil {
		return nil
	}
	return m.fileComments[m.viewer.commentKey()]
}

// commentCount says how many comments there are and that a attaches them.
func (m model) commentCount() string {
	switch count := len(m.comments()); count {
	case 0:
		return ""
	case 1:
		return " · 1 comment, a attaches it"
	default:
		return fmt.Sprintf(" · %d comments, a attaches them", count)
	}
}

// commentKey is what the comments on what the viewer shows are kept by:
// the file, or the file in its comparison.
func (v *outputViewer) commentKey() string {
	if v.changes != nil {
		return "diff " + v.changes.title + " " + v.diffFile
	}
	return v.file
}

// annotatable tells whether the viewer shows lines to move a cursor on and
// comment: a file, or a diff.
func (v *outputViewer) annotatable() bool {
	return v.markdown == nil && (v.file != "" || v.diff != nil)
}

// anchor is what a comment on a row of the viewer keeps: the row of a
// file, the line of a diff; a fold of a diff has none.
func (v *outputViewer) anchor(row int) (int, bool) {
	if v.diff != nil {
		return v.diff.edit(row)
	}
	return row, true
}

// rowOf is the row an anchor is shown on.
func (v *outputViewer) rowOf(anchor int) int {
	if v.diff != nil {
		return v.diff.rowOf(anchor)
	}
	return anchor
}

// commentAt is the comment covering a row, its index in the list.
func (m model) commentAt(row int) (fileComment, int, bool) {
	line, ok := m.viewer.anchor(row)
	if !ok {
		return fileComment{}, -1, false
	}
	for i, comment := range m.comments() {
		if line >= comment.from && line <= comment.to {
			return comment, i, true
		}
	}
	return fileComment{}, -1, false
}

// startComment asks for a comment on the lines dragged over, or on the
// cursor's, its text the one there is already.
func (m model) startComment() {
	v := m.viewer
	from, to := v.cursor, v.cursor
	if v.selected {
		from, to = min(v.pickFrom, v.pickTo), max(v.pickFrom, v.pickTo)
	}
	if _, ok := v.anchor(from); !ok {
		v.notice = "a fold has nothing to comment: e shows its lines"
		return
	}
	v.commenting, v.commentFrom, v.commentTo = true, from, to
	text := ""
	if comment, _, ok := m.commentAt(from); ok {
		v.commentFrom, v.commentTo, text = v.rowOf(comment.from), v.rowOf(comment.to), comment.text
	}
	v.commentInput = newCommentInput(text, m.viewerWidth(), m.printer == nil || m.printer.isDark())
	m.keepCommentedInView()
}

// typeComment edits the comment being written; enter keeps it, esc drops
// what was typed, an empty one deletes it.
func (m model) typeComment(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.viewer
	switch msg.String() {
	case "enter":
		v.commenting, v.selected = false, false
		from, fromOK := v.anchor(v.commentFrom)
		to, toOK := v.anchor(v.commentTo)
		if !toOK {
			to, toOK = v.anchor(v.commentTo - 1)
		}
		if fromOK && toOK {
			m.setComment(from, to, strings.TrimSpace(v.commentInput.Value()))
		}
	case "esc":
		v.commenting = false
	default:
		return m, m.editComment(msg)
	}
	return m, nil
}

// setComment keeps a comment on lines, by their anchors, in place of one
// that covers them; empty, it deletes that one.
func (m model) setComment(from, to int, text string) {
	file := m.viewer.commentKey()
	kept := slices.DeleteFunc(slices.Clone(m.fileComments[file]), func(each fileComment) bool {
		return each.from <= to && each.to >= from
	})
	if text != "" {
		kept = append(kept, fileComment{from: from, to: to, text: text})
		sort.Slice(kept, func(i, j int) bool { return kept[i].from < kept[j].from })
	}
	if len(kept) == 0 {
		delete(m.fileComments, file)
		return
	}
	m.fileComments[file] = kept
}

// moveCursor moves the viewer's cursor by lines and scrolls to keep it in
// view.
func (m model) moveCursor(by int) {
	v := m.viewer
	v.cursor = min(max(v.cursor+by, 0), max(len(v.lines)-1, 0))
	rows := m.viewerRows()
	switch {
	case v.cursor < v.top:
		v.top = v.cursor
	case v.cursor >= v.top+rows:
		v.top = v.cursor - rows + 1
	}
}

// attachFile closes the viewer and puts the file in the input, with the
// comments on it when there are any, for the model to get with the
// message.
func (m model) attachFile() (tea.Model, tea.Cmd) {
	v := m.viewer
	full := v.file
	if v.changes != nil {
		full = filepath.Join(v.changes.root, v.diffFile)
	}
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.session.WorkDir, full)
	}
	m.viewer = nil
	if m.attachments == nil {
		m.attachments = map[string]string{}
	}
	key := v.commentKey()
	comments := m.fileComments[key]
	if len(comments) == 0 {
		m.attach([]string{full}, false)
		return m, nil
	}
	label := fmt.Sprintf("[comments %s]", filepath.Base(full))
	for n := 2; m.attachments[label] != "" && m.attachments[label] != full; n++ {
		label = fmt.Sprintf("[comments %s %d]", filepath.Base(full), n)
	}
	m.attachments[label] = full
	if m.commentNotes == nil {
		m.commentNotes, m.commentSources = map[string]string{}, map[string]string{}
	}
	m.commentSources[label] = key
	m.commentNotes[label] = commentNote(v.file, v.lines, comments)
	if v.diff != nil {
		m.commentNotes[label] = v.diff.commentNote(v.diffFile, v.changes.title, comments)
	}
	m.input.InsertString(label + " ")
	return m, nil
}

// commentNote is how the model gets the comments: each with the lines it
// is on, the first of them quoted.
func commentNote(file string, lines []string, comments []fileComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The user's comments on %s:", file)
	for _, comment := range comments {
		where := fmt.Sprintf("line %d", comment.from+1)
		if comment.to > comment.from {
			where = fmt.Sprintf("lines %d-%d", comment.from+1, comment.to+1)
		}
		quoted := ""
		if comment.from < len(lines) {
			quoted = quote(lines[comment.from])
		}
		fmt.Fprintf(&b, "\n- %s (`%s`): %s", where, quoted, strings.ReplaceAll(comment.text, "\n", "\n  "))
	}
	return b.String()
}

// quote is a line as a comment quotes it: trimmed, and cut when long.
func quote(line string) string {
	quoted := strings.TrimSpace(line)
	if len([]rune(quoted)) > commentQuoteMost {
		quoted = string([]rune(quoted)[:commentQuoteMost]) + "…"
	}
	return quoted
}

// fileIndex is the files of the working directory, read again when old,
// for @ to find.
type fileIndex struct {
	mu    sync.Mutex
	files []string
	read  time.Time
	dir   string
}

func (index *fileIndex) list(dir string) []string {
	if index == nil {
		return readFiles(dir)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.dir != dir || time.Since(index.read) > fileIndexAge {
		index.files, index.read, index.dir = readFiles(dir), time.Now(), dir
	}
	return index.files
}

// readFiles lists the files under dir by their paths from it, leaving out
// hidden folders and those of dependencies and builds.
func readFiles(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || skippedDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if relative, err := filepath.Rel(dir, path); err == nil {
			files = append(files, relative)
		}
		if len(files) >= fileIndexMost {
			return filepath.SkipAll
		}
		return nil
	})
	return files
}

// mention is the word being typed at the end of the input when it starts
// with @, without it.
func (m model) mention() (string, bool) {
	value := m.input.Value()
	start := strings.LastIndexAny(value, " \n\t") + 1
	word := value[start:]
	if !strings.HasPrefix(word, "@") || strings.HasPrefix(value, "/") {
		return "", false
	}
	return word[1:], true
}

// mentionSuggestions are the files whose path holds what was typed after
// @, those whose name does first; or, failing that, those holding its
// letters in order.
func (m model) mentionSuggestions(typed string) []command {
	var commands []command
	matches := fuzzyFiles(m.files.list(m.session.WorkDir), typed)
	for _, path := range matches[:min(mentionShown, len(matches))] {
		commands = append(commands, command{name: path, description: "file", mention: true})
	}
	return commands
}

// inOrder tells whether text holds every letter of letters in their order.
func inOrder(text, letters string) bool {
	for _, letter := range letters {
		at := strings.IndexRune(text, letter)
		if at < 0 {
			return false
		}
		text = text[at+len(string(letter)):]
	}
	return true
}

// completeMention puts the file picked after @ in the input as an
// attachment, in place of what was typed.
func (m *model) completeMention(chosen command) {
	value := m.input.Value()
	start := strings.LastIndexAny(value, " \n\t") + 1
	m.input.SetValue(value[:start])
	m.input.CursorEnd()
	m.attach([]string{filepath.Join(m.session.WorkDir, chosen.name)}, false)
	m.suggestion = 0
}

// cursorBackground is the cursor line's background as an escape
// sequence, put back after each reset of the highlighting's colours.
func cursorBackground() string {
	red, green, blue, _ := col(uiNow().cursorLine).RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", red>>8, green>>8, blue>>8)
}

// cursorRow paints the background of the line the viewer's cursor is on,
// keeping the colours of its highlighting, to the width of the screen.
func cursorRow(line string, width int) string {
	padded := line + strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))
	background := cursorBackground()
	for _, reset := range []string{"\x1b[0m", "\x1b[m"} {
		padded = strings.ReplaceAll(padded, reset, reset+background)
	}
	return background + padded + "\x1b[m"
}
