package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const leftTreeHelp = "enter open · c C fold · ? keys"

// What the panel at the left lists.
const (
	leftFiles   = "files"
	leftChanged = "changes"
	leftBoth    = "both"
)

// leftHalf is a tree of the panel at the left and the rows it takes, from
// the panel's top row.
type leftHalf struct {
	tree      *fileTree
	top, rows int
	changes   bool
}

// leftTreeShown tells whether the panel at the left shows: /tree opened
// it, and the conversation keeps mainMinWidth beside it.
func (m model) leftTreeShown() bool {
	return m.leftMode != "" && m.width >= m.treeWidth()+1+mainMinWidth
}

// leftTreeFocused tells whether the keys go to the panel at the left.
func (m model) leftTreeFocused() bool {
	return m.leftTreeFocus && m.leftTreeShown()
}

// leftPanelWidth is the columns the panel at the left takes, and the line
// after it.
func (m model) leftPanelWidth() int {
	if !m.leftTreeShown() {
		return 0
	}
	return m.treeWidth() + 1
}

// panelRows is the rows over the input and the status line, where the side
// panels are.
func (m model) panelRows() int {
	rows := m.conversationHeight() + 1
	if upper, _ := m.upperLayout(); upper != "" {
		rows += lipgloss.Height(upper)
	}
	return rows
}

// leftHalves are the trees the panel at the left shows, in height rows:
// one, or the files over the changes.
func (m model) leftHalves(height int) []leftHalf {
	switch m.leftMode {
	case leftChanged:
		return []leftHalf{{tree: m.leftChanges.tree, rows: height, changes: true}}
	case leftBoth:
		upper := height / 2
		return []leftHalf{
			{tree: m.leftTree, rows: upper},
			{tree: m.leftChanges.tree, top: upper, rows: height - upper, changes: true},
		}
	}
	return []leftHalf{{tree: m.leftTree, rows: height}}
}

// activeLeftHalf is the tree of the panel at the left the keys go to.
func (m model) activeLeftHalf() leftHalf {
	halves := m.leftHalves(m.panelRows())
	if len(halves) == 2 && m.leftOnChanges {
		return halves[1]
	}
	return halves[0]
}

// leftHalfAt is the tree of the panel at the left at a row of the screen.
func (m model) leftHalfAt(y int) leftHalf {
	halves := m.leftHalves(m.panelRows())
	if len(halves) == 2 && y >= halves[1].top {
		return halves[1]
	}
	return halves[0]
}

// treeCommand is /tree: alone it shows the files of the project at the
// left, the keys in them, or hides them; files, changes and both pick what
// it lists, on and off whether it shows; status the marks of git; width
// its columns.
func (m model) treeCommand(argument string) (tea.Model, tea.Cmd) {
	words := strings.Fields(argument)
	first := ""
	if len(words) > 0 {
		first = words[0]
	}
	switch first {
	case "width":
		return m.widthCommand(treeSize, words[1:])
	case "status":
		return m.treeStatusCommand(words[1:])
	case leftChanged, leftBoth:
		if len(words) > 2 {
			return m.emit(m.printer.Note("/tree " + first + " takes a branch or commit to compare with, or nothing for the changes not committed"))
		}
		return m, loadLeftChanges(m.session.WorkDir, strings.Join(words[1:], ""), first, false)
	case "":
		if m.leftMode != "" {
			return m.hideLeftTree()
		}
	case sidebarOn:
		if m.leftMode != "" {
			m.leftTreeFocus = true
			return m, nil
		}
	case sidebarOff:
		return m.hideLeftTree()
	case leftFiles:
	default:
		return m.emit(m.printer.Note("/tree takes on, off, files, changes [branch], both [branch], status [on|off] or width [columns] [session|global|project]"))
	}
	m.leftMode, m.leftChanges, m.leftOnChanges, m.leftTreeFocus = leftFiles, nil, false, true
	if m.leftTree == nil {
		m.leftTree = newFileTree(m.session.WorkDir)
	}
	note := "files at the left · ctrl+t goes to them and back · /tree changes or both for what changed · /tree off hides them"
	if !m.leftTreeShown() {
		note = fmt.Sprintf("files at the left from %d columns", m.treeWidth()+1+mainMinWidth)
	}
	return m, tea.Batch(m.fitWidth(), printText(m.printer.Note(note)))
}

func (m model) hideLeftTree() (tea.Model, tea.Cmd) {
	m.leftMode, m.leftTree, m.leftChanges, m.leftTreeFocus = "", nil, nil, false
	return m, tea.Batch(m.fitWidth(), printText(m.printer.Note("files hidden")))
}

func (m model) treeSuggestions(partial string) []command {
	if rest, ok := strings.CutPrefix(partial, "width "); ok {
		return m.widthSuggestions(treeSize, rest)
	}
	status := "off"
	if m.treeStatusOn() {
		status = "on"
	}
	options := []command{
		{name: "tree " + sidebarOn, description: "the files of the project at the left, until /tree off"},
		{name: "tree " + sidebarOff, description: "hide them"},
		{name: "tree files", description: "the files of the project, the tree /tree opens"},
		{name: "tree changes", description: "the files changed and not committed, each opening its diff"},
		{name: "tree changes ", description: "what this branch changed over another branch or a commit", argument: "<branch>", more: true},
		{name: "tree both", description: "the files above, the changes not committed below"},
		{name: "tree both ", description: "the files above, what this branch changed over another below", argument: "<branch>", more: true},
		{name: "tree status on", description: "now " + status + " · a dot in the colour of what git says of each file and folder"},
		{name: "tree status off", description: "now " + status + " · the files without the dots of git"},
		{name: "tree width ", description: fmt.Sprintf("now %d columns · say or set it, for the session, global or project", m.treeWidth()), more: true},
	}
	var matches []command
	for _, option := range options {
		if strings.HasPrefix(option.name, "tree "+partial) {
			matches = append(matches, option)
		}
	}
	return matches
}

// leftTreeKey is a key pressed while the panel at the left has the focus.
func (m model) leftTreeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	half := m.activeLeftHalf()
	tree := half.tree
	if tree.typing {
		tree.typeSearch(msg)
		return m, nil
	}
	pressed := msg.String()
	if tree.navigate(pressed, half.rows-2) {
		return m, nil
	}
	row, onFile := tree.selected()
	onFile = onFile && !row.dir
	switch pressed {
	case "esc":
		if tree.filter != "" {
			tree.search("")
			return m, nil
		}
		m.leftTreeFocus = false
	case "tab":
		if m.leftMode == leftBoth {
			m.leftOnChanges = !m.leftOnChanges
			return m, nil
		}
		m.leftTreeFocus = false
	case "ctrl+t":
		m.leftTreeFocus = false
	case "right", "l", "enter", "space":
		if onFile {
			return m.openFromLeftTree(half, row.path)
		}
	case "/":
		tree.typing, tree.typed = true, tree.filter
	}
	return m, nil
}

// openFromLeftTree shows a file over the conversation, or its diff when it
// is one of the changes, with the same tree beside it, while the agent goes
// on; closing it comes back here.
func (m model) openFromLeftTree(half leftHalf, path string) (tea.Model, tea.Cmd) {
	if half.changes {
		return m.openLeftChange(path)
	}
	next, cmd := m.viewPath(path)
	opened := next.(model)
	if opened.viewer == nil {
		return opened, cmd
	}
	opened.leftTreeFocus = false
	opened.viewer.tree = half.tree
	half.tree.current = path
	half.tree.show(opened.treeHeight())
	return opened, cmd
}

// inLeftTree tells whether a cell of the screen is on the panel at the left.
func (m model) inLeftTree(x, y int) bool {
	return m.leftTreeShown() && x < m.treeWidth() && y < m.panelRows()
}

// paneX is a column of the screen as the conversation and the side panel
// count it, past the panel at the left over the input.
func (m model) paneX(x, y int) int {
	if y >= m.panelRows() {
		return x
	}
	return max(x-m.leftPanelWidth(), 0)
}

// leftTreeClick is a click on the panel at the left, row y of the screen.
func (m model) leftTreeClick(y int) (tea.Model, tea.Cmd) {
	half := m.leftHalfAt(y)
	m.leftTreeFocus, m.leftOnChanges = true, half.changes
	if file, picked := half.tree.pick(y - half.top); picked {
		return m.openFromLeftTree(half, file)
	}
	return m, nil
}

// withLeftTree puts the panel on the left of the rows over the input.
func (m model) withLeftTree(screen string) string {
	if !m.leftTreeShown() {
		return screen
	}
	width := m.treeWidth()
	rows := strings.Split(screen, "\n")
	height := min(m.panelRows(), len(rows))
	focused := m.leftTreeFocused()
	halves := m.leftHalves(height)
	var left []string
	for i, half := range halves {
		active := focused && (len(halves) == 1 || half.changes == m.leftOnChanges)
		lines := half.tree.lines(width, half.rows, active, m.treeIconSet(), m.gitMarksFor(half.tree))
		last := len(lines) - 1
		switch {
		case half.tree.typing:
		case i < len(halves)-1:
			lines[last] = dimStyle.Render(strings.Repeat("─", width))
		default:
			lines[last] = padTo(dimStyle.Render(ansi.Truncate(leftTreeHelp, width, "…")), width)
		}
		left = append(left, lines...)
	}
	divider := dimStyle.Render("│")
	if focused {
		divider = treeFolder().Render("│")
	}
	room := m.width - width - 1
	for i := range height {
		rows[i] = left[i] + divider + ansi.Truncate(rows[i], room, "")
	}
	return strings.Join(rows, "\n")
}

// scrollLeftTree moves the cursor of the tree under the pointer as the
// wheel turns.
func (m *model) scrollLeftTree(y int, up bool) {
	by := sidebarWheelStep
	if up {
		by = -by
	}
	half := m.leftHalfAt(y)
	half.tree.move(by, half.rows-2)
}
