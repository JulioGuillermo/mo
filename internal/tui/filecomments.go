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

var (
	viewerCursor  = lipgloss.NewStyle().Background(lipgloss.Color("#292e42"))
	commentMark   = lipgloss.NewStyle().Foreground(warning).Bold(true)
	commentFooter = lipgloss.NewStyle().Foreground(warning)
	skippedDirs   = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "__pycache__": true}
)

// fileComment is what the user said of lines of a file, from and to
// counted from 0, to send the model with it.
type fileComment struct {
	from, to int
	text     string
}

// comments are the user's comments on the file the viewer shows, in order.
func (m model) comments() []fileComment {
	if m.viewer == nil || m.fileComments == nil {
		return nil
	}
	return m.fileComments[m.viewer.file]
}

// commentAt is the comment covering a line, its index in the list.
func (m model) commentAt(line int) (fileComment, int, bool) {
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
	v.commenting, v.commentFrom, v.commentTo, v.commentText = true, from, to, ""
	if comment, _, ok := m.commentAt(from); ok {
		v.commentFrom, v.commentTo, v.commentText = comment.from, comment.to, comment.text
	}
}

// typeComment edits the comment being written; enter keeps it, esc drops
// what was typed, an empty one deletes it.
func (m model) typeComment(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.viewer
	switch msg.String() {
	case "enter":
		v.commenting, v.selected = false, false
		m.setComment(v.commentFrom, v.commentTo, strings.TrimSpace(v.commentText))
	case "esc":
		v.commenting = false
	case "backspace":
		if runes := []rune(v.commentText); len(runes) > 0 {
			v.commentText = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		v.commentText = ""
	default:
		if msg.Text != "" {
			v.commentText += msg.Text
		}
	}
	return m, nil
}

// setComment keeps a comment on lines, in place of one that covers them;
// empty, it deletes that one.
func (m model) setComment(from, to int, text string) {
	file := m.viewer.file
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
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.session.WorkDir, full)
	}
	m.viewer = nil
	if m.attachments == nil {
		m.attachments = map[string]string{}
	}
	comments := m.fileComments[v.file]
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
		m.commentNotes = map[string]string{}
	}
	m.commentNotes[label] = commentNote(v.file, v.lines, comments)
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
			quoted = strings.TrimSpace(lines[comment.from])
			if len([]rune(quoted)) > commentQuoteMost {
				quoted = string([]rune(quoted)[:commentQuoteMost]) + "…"
			}
		}
		fmt.Fprintf(&b, "\n- %s (`%s`): %s", where, quoted, comment.text)
	}
	return b.String()
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
	query := strings.ToLower(typed)
	type found struct {
		path  string
		score int
	}
	var matches []found
	for _, path := range m.files.list(m.session.WorkDir) {
		lower := strings.ToLower(path)
		base := strings.ToLower(filepath.Base(path))
		switch {
		case query == "":
			matches = append(matches, found{path, 3})
		case strings.HasPrefix(base, query):
			matches = append(matches, found{path, 0})
		case strings.Contains(base, query):
			matches = append(matches, found{path, 1})
		case strings.Contains(lower, query):
			matches = append(matches, found{path, 2})
		case inOrder(lower, query):
			matches = append(matches, found{path, 3})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return len(matches[i].path) < len(matches[j].path)
	})
	var commands []command
	for _, match := range matches[:min(len(matches), mentionShown)] {
		commands = append(commands, command{name: match.path, description: "file", mention: true})
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

// cursorRow paints the line the viewer's cursor is on.
func cursorRow(line string, width int) string {
	plain := ansi.Strip(line)
	return viewerCursor.Render(plain + strings.Repeat(" ", max(width-ansi.StringWidth(plain), 0)))
}
