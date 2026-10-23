package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	treeMinWidth = 22
	treeMaxWidth = 38
	// treeNarrow is the columns a terminal needs for the tree beside a file:
	// under it the tree has no room and folders are listed to pick from.
	treeNarrow = 60
	treeHelp   = "↑↓ move · →/enter open · ← close · / find · tab file · t hide"
)

func treeFolder() lipgloss.Style { return lipgloss.NewStyle().Foreground(accent()) }
func treeOpened() lipgloss.Style { return lipgloss.NewStyle().Foreground(warning()).Bold(true) }

var (
	treeFocused = lipgloss.NewStyle().Reverse(true)
	treeBlurred = lipgloss.NewStyle().Underline(true)
)

// treeRow is a line of the tree: a folder, open or not, or a file, as deep
// as the folders around it.
type treeRow struct {
	path, name string
	depth      int
	dir, open  bool
}

// fileTree is the folders and files under a root, as an editor lists them:
// folders open and close, a cursor moves, and a search narrows it to the
// files that hold what was typed.
type fileTree struct {
	root     string
	expanded map[string]bool
	rows     []treeRow
	cursor   int
	top      int
	// current is the file the viewer shows.
	current string
	// filter is the search applied, typing the one being typed, found the
	// files it names, by their paths from the root.
	filter  string
	typing  bool
	typed   string
	found   []string
	indexed []string
	// listing gives the folders and files of a folder, from the disk when
	// unset; kinds is what happened to each file of a comparison, by path.
	listing func(dir string) (folders, files []string)
	kinds   map[string]rune
}

func newFileTree(root string) *fileTree {
	tree := &fileTree{root: filepath.Clean(root), expanded: map[string]bool{}}
	tree.rebuild()
	return tree
}

// entries are the folders and the files of a folder, each by name, in the
// order they are listed: folders first, capitals not mattering.
func entries(dir string) (folders, files []string) {
	listed, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, entry := range listed {
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			folders = append(folders, entry.Name())
		} else {
			files = append(files, entry.Name())
		}
	}
	less := func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }
	slices.SortFunc(folders, less)
	slices.SortFunc(files, less)
	return folders, files
}

// rebuild lists the rows again: the folders open, or, with a search applied,
// the files it found.
func (t *fileTree) rebuild() {
	t.rows = t.rows[:0]
	if t.filter != "" {
		for _, path := range t.found {
			t.rows = append(t.rows, treeRow{path: filepath.Join(t.root, path), name: path})
		}
	} else {
		var walk func(dir string, depth int)
		listing := t.listing
		if listing == nil {
			listing = entries
		}
		walk = func(dir string, depth int) {
			folders, files := listing(dir)
			for _, name := range folders {
				full := filepath.Join(dir, name)
				open := t.expanded[full]
				t.rows = append(t.rows, treeRow{path: full, name: name, depth: depth, dir: true, open: open})
				if open {
					walk(full, depth+1)
				}
			}
			for _, name := range files {
				t.rows = append(t.rows, treeRow{path: filepath.Join(dir, name), name: name, depth: depth})
			}
		}
		walk(t.root, 0)
	}
	t.cursor = min(max(t.cursor, 0), max(len(t.rows)-1, 0))
}

// selected is the row the cursor is on.
func (t *fileTree) selected() (treeRow, bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return treeRow{}, false
	}
	return t.rows[t.cursor], true
}

// move puts the cursor by rows further, within the list, and keeps it in view
// of height rows.
func (t *fileTree) move(by, height int) {
	t.cursor = min(max(t.cursor+by, 0), max(len(t.rows)-1, 0))
	t.show(height)
}

// show scrolls the list so the cursor is in view.
func (t *fileTree) show(height int) {
	height = max(height, 1)
	if t.cursor < t.top {
		t.top = t.cursor
	}
	if t.cursor >= t.top+height {
		t.top = t.cursor - height + 1
	}
	t.top = max(min(t.top, max(len(t.rows)-height, 0)), 0)
}

// reveal opens the folders over a file and puts the cursor on it, if it is
// under the root.
func (t *fileTree) reveal(path string, height int) {
	relative, err := filepath.Rel(t.root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return
	}
	t.current = path
	if t.filter != "" {
		t.filter, t.found = "", nil
	}
	dir := t.root
	for _, part := range strings.Split(filepath.Dir(relative), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		dir = filepath.Join(dir, part)
		t.expanded[dir] = true
	}
	t.rebuild()
	for i, row := range t.rows {
		if row.path == path {
			t.cursor = i
			break
		}
	}
	t.show(height)
}

// search applies what was typed: the files of the root that hold it, by the
// search /view uses, the pattern when it has * or ?; nothing typed clears it.
func (t *fileTree) search(typed string) {
	t.filter = strings.TrimSpace(typed)
	t.found, t.cursor, t.top = nil, 0, 0
	if t.filter != "" {
		if t.indexed == nil {
			t.indexed = readFiles(t.root)
		}
		if isGlob(t.filter) {
			t.found = globFiles(t.indexed, t.filter)
		} else {
			t.found = fuzzyFiles(t.indexed, t.filter)
		}
	}
	t.rebuild()
}

// lines are height lines of the tree, each width wide: the title, the rows from
// the top, the help.
func (t *fileTree) lines(width, height int, focused bool, icons string) []string {
	title := treeTitle(t, width)
	out := []string{padTo(title, width)}
	for i := t.top; i < t.top+height-2; i++ {
		if i >= len(t.rows) {
			out = append(out, strings.Repeat(" ", width))
			continue
		}
		row := t.rows[i]
		indent := strings.Repeat("  ", row.depth)
		icon, hasIcon := iconOf(icons, filepath.Base(row.name), row.dir, row.open)
		var mark, tint string
		switch {
		case hasIcon:
			mark = icon.glyph + " "
			tint = mark
			if icon.color != nil {
				tint = lipgloss.NewStyle().Foreground(icon.color).Render(icon.glyph) + " "
			}
		case row.dir && row.open:
			mark, tint = "▾ ", "▾ "
		case row.dir:
			mark, tint = "▸ ", "▸ "
		case t.filter == "":
			mark, tint = "  ", "  "
		}
		name := row.name
		if row.dir && !hasIcon {
			name += "/"
		}
		// What the row says, as plain text, for the cursor to colour whole, and
		// as it is coloured otherwise.
		plain := ansi.Truncate(indent+mark+name, width, "…")
		var text string
		kind, changed := t.kinds[row.path]
		switch {
		case i == t.cursor && changed:
			// A changed file keeps its colour under the cursor, the cursor
			// shown by the colour's ground or by underlining it.
			style := changeStyle(kind, row.path == t.current)
			if focused {
				style = style.Reverse(true)
				plain = padTo(plain, width)
			} else {
				style = style.Underline(true)
			}
			text = style.Render(plain)
		case i == t.cursor && focused:
			text = treeFocused.Render(padTo(plain, width))
		case i == t.cursor:
			text = treeBlurred.Render(plain)
		default:
			label := name
			switch {
			case row.dir:
				label = treeFolder().Render(name)
			case changed:
				label = changeStyle(kind, row.path == t.current).Render(name)
			case row.path == t.current:
				label = treeOpened().Render(name)
			}
			text = ansi.Truncate(indent+tint+label, width, "…")
		}
		out = append(out, padTo(text, width))
	}
	help := treeHelp
	if t.typing {
		help = "enter find · esc cancel"
	}
	out = append(out, padTo(dimStyle.Render(ansi.Truncate(help, width, "…")), width))
	return out
}

func treeTitle(t *fileTree, width int) string {
	switch {
	case t.typing:
		return keyStyle().Render("/") + ansi.Truncate(t.typed, width-3, "") + "▏"
	case t.filter != "":
		return keyStyle().Render("/") + ansi.Truncate(t.filter, width-12, "…") + dimStyle.Render(fmt.Sprintf(" · %d found", len(t.found)))
	}
	return toolName().Render(ansi.Truncate(filepath.Base(t.root)+"/", width, "…"))
}

// padTo fills a styled line with spaces to width, or cuts it there.
func padTo(line string, width int) string {
	if shown := ansi.StringWidth(line); shown > width {
		return ansi.Truncate(line, width, "")
	} else if shown < width {
		return line + strings.Repeat(" ", width-shown)
	}
	return line
}

// treeWidth is the columns the tree takes beside a file.
func (m model) treeWidth() int {
	return min(max(m.width/4, treeMinWidth), treeMaxWidth)
}

// treeShown tells whether the tree is drawn: there is one, and the terminal
// has room for it beside a file; made narrower it is hidden, not closed.
func (m model) treeShown() bool {
	return m.viewer != nil && m.viewer.tree != nil && m.width >= treeNarrow
}

// viewerWidth is the columns the text of the viewer has: all the screen, or
// what the tree leaves, and the line between.
func (m model) viewerWidth() int {
	width := max(m.width, 20)
	switch {
	case m.commitListShown():
		return max(width-m.commitListWidth()-1, 20)
	case m.treeShown():
		return max(width-m.treeWidth()-1, 20)
	}
	return width
}

// treeHeight is the rows the tree lists: the screen less its title and help.
func (m model) treeHeight() int {
	return max(m.viewerRows(), 1)
}

// openExplorer shows the viewer with the tree of a folder and nothing in the
// file's place yet, the tree in focus; where the terminal has no room for both,
// the folder is listed to pick from.
func (m model) openExplorer(dir string) (tea.Model, tea.Cmd) {
	if m.width < treeNarrow {
		return m.browse(dir)
	}
	v := &outputViewer{placeholder: true, pickFrom: -1, tree: newFileTree(dir), treeFocus: true}
	m.viewer = v
	return m, nil
}

// showTree opens the tree beside the file the viewer shows, on that file, or
// hides it.
func (m model) toggleTree() {
	v := m.viewer
	if v.tree != nil {
		v.tree, v.treeFocus = nil, false
		v.left = 0
		return
	}
	if v.changes != nil {
		v.tree, v.treeFocus = v.changes.tree, true
		return
	}
	if m.width < treeNarrow {
		v.notice = "the terminal is too narrow for the tree"
		return
	}
	root := m.session.WorkDir
	if v.file != "" {
		full := v.file
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, full)
		}
		if relative, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(relative, "..") {
			root = filepath.Dir(full)
		}
		v.tree = newFileTree(root)
		v.tree.reveal(full, m.treeHeight())
	} else {
		v.tree = newFileTree(root)
	}
	v.treeFocus = true
}

// openFromTree shows a file the tree picked in the viewer's place, the tree
// kept; focus goes to the file when asked.
func (m model) openFromTree(path string, focusFile bool) (tea.Model, tea.Cmd) {
	old := m.viewer
	if old.changes != nil {
		m.openChange(old.changes, path)
		m.viewer.tree, m.viewer.treeFocus = old.tree, !focusFile
		return m, nil
	}
	if drawnInline(path) {
		old.notice = "pictures and diagrams show in the conversation: /view " + filepath.Base(path)
		return m, nil
	}
	next, cmd := m.openFile(path)
	opened := next.(model)
	if opened.viewer == nil || opened.viewer == old {
		old.notice = "could not open " + filepath.Base(path)
		return opened, cmd
	}
	opened.viewer.tree, opened.viewer.treeFocus = old.tree, old.treeFocus && !focusFile
	opened.viewer.tree.current = path
	return opened, cmd
}

// treeKey is a key pressed while the tree has the focus.
func (m model) treeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.viewer
	tree := v.tree
	height := m.treeHeight()
	pressed := msg.String()
	if tree.typing {
		switch pressed {
		case "enter":
			tree.typing = false
			tree.search(tree.typed)
		case "esc":
			tree.typing = false
		case "backspace":
			if runes := []rune(tree.typed); len(runes) > 0 {
				tree.typed = string(runes[:len(runes)-1])
			}
		default:
			tree.typed += msg.Text
		}
		return m, nil
	}
	row, onRow := tree.selected()
	switch pressed {
	case "esc":
		if tree.filter != "" {
			tree.search("")
			return m, nil
		}
		m.viewer = nil
	case "q", "ctrl+c":
		m.viewer = nil
	case "up", "k":
		tree.move(-1, height)
	case "down", "j":
		tree.move(1, height)
	case "pgup":
		tree.move(-height, height)
	case "pgdown":
		tree.move(height, height)
	case "home", "g":
		tree.move(-len(tree.rows), height)
	case "end", "G":
		tree.move(len(tree.rows), height)
	case "right", "l":
		switch {
		case !onRow:
		case row.dir && !row.open:
			tree.expanded[row.path] = true
			tree.rebuild()
		case row.dir:
			tree.move(1, height)
		default:
			return m.openFromTree(row.path, false)
		}
	case "left", "h":
		switch {
		case !onRow:
		case row.dir && row.open:
			delete(tree.expanded, row.path)
			tree.rebuild()
		default:
			// The folder over it.
			for i := tree.cursor - 1; i >= 0; i-- {
				if tree.rows[i].dir && tree.rows[i].depth < row.depth {
					tree.cursor = i
					tree.show(height)
					break
				}
			}
		}
	case "enter", "space":
		switch {
		case !onRow:
		case row.dir:
			if row.open {
				delete(tree.expanded, row.path)
			} else {
				tree.expanded[row.path] = true
			}
			tree.rebuild()
		default:
			return m.openFromTree(row.path, true)
		}
	case "/":
		tree.typing, tree.typed = true, tree.filter
	case "tab":
		v.treeFocus = false
	case "t":
		m.toggleTree()
	}
	return m, nil
}

// treeClick is a click on the tree, row y of the screen: the row under it is
// chosen, a folder opens or closes, a file is shown.
func (m model) treeClick(y int) (tea.Model, tea.Cmd) {
	v := m.viewer
	tree := v.tree
	v.treeFocus = true
	row := tree.top + y - 1
	if y < 1 || row >= len(tree.rows) {
		return m, nil
	}
	tree.cursor = row
	picked := tree.rows[row]
	if picked.dir {
		if picked.open {
			delete(tree.expanded, picked.path)
		} else {
			tree.expanded[picked.path] = true
		}
		tree.rebuild()
		return m, nil
	}
	return m.openFromTree(picked.path, false)
}

// treeLayout puts the tree on the left of the viewer's lines, a line between.
func (m model) treeLayout(body string) string {
	v := m.viewer
	width := m.treeWidth()
	rows := strings.Split(body, "\n")
	left := v.tree.lines(width, len(rows), v.treeFocus, m.treeIconSet())
	divider := dimStyle.Render("│")
	if v.treeFocus {
		divider = treeFolder().Render("│")
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = padTo(left[min(i, len(left)-1)], width) + divider + row
	}
	return strings.Join(out, "\n")
}
