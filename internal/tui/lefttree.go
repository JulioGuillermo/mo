package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const leftTreeHelp = "enter open · c C fold · ? keys"

// leftTreeShown tells whether the files of the project show at the left of
// the conversation: /tree opened them, and the conversation keeps
// mainMinWidth beside them.
func (m model) leftTreeShown() bool {
	return m.leftTree != nil && m.width >= m.treeWidth()+1+mainMinWidth
}

// leftTreeFocused tells whether the keys go to the tree at the left.
func (m model) leftTreeFocused() bool {
	return m.leftTreeFocus && m.leftTreeShown()
}

// leftPanelWidth is the columns the tree at the left takes, and the line
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

// treeCommand is /tree: alone it shows the files of the project at the
// left, the keys in them, or hides them; on and off say which; /tree width
// says the width, or sets it.
func (m model) treeCommand(argument string) (tea.Model, tea.Cmd) {
	words := strings.Fields(argument)
	if len(words) > 0 && words[0] == "width" {
		return m.widthCommand(treeSize, words[1:])
	}
	show := m.leftTree == nil
	switch argument = strings.TrimSpace(argument); argument {
	case "":
	case sidebarOn:
		show = true
	case sidebarOff:
		show = false
	default:
		return m.emit(m.printer.Note("/tree takes on, off or width [columns] [session|global|project]"))
	}
	if !show {
		m.leftTree, m.leftTreeFocus = nil, false
		return m, tea.Batch(m.fitWidth(), printText(m.printer.Note("files hidden")))
	}
	note := "files at the left · ctrl+t goes to them and back · /tree hides them"
	if m.leftTree == nil {
		m.leftTree = newFileTree(m.session.WorkDir)
	}
	m.leftTreeFocus = true
	if !m.leftTreeShown() {
		note = fmt.Sprintf("files at the left from %d columns", m.treeWidth()+1+mainMinWidth)
	}
	return m, tea.Batch(m.fitWidth(), printText(m.printer.Note(note)))
}

func (m model) treeSuggestions(partial string) []command {
	if rest, ok := strings.CutPrefix(partial, "width "); ok {
		return m.widthSuggestions(treeSize, rest)
	}
	options := []command{
		{name: "tree " + sidebarOn, description: "the files of the project at the left, until /tree off"},
		{name: "tree " + sidebarOff, description: "hide them"},
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

// leftTreeKey is a key pressed while the tree at the left has the focus.
func (m model) leftTreeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	tree := m.leftTree
	if tree.typing {
		tree.typeSearch(msg)
		return m, nil
	}
	pressed := msg.String()
	if tree.navigate(pressed, m.panelRows()-2) {
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
	case "tab", "ctrl+t":
		m.leftTreeFocus = false
	case "right", "l", "enter", "space":
		if onFile {
			return m.openFromLeftTree(row.path)
		}
	case "/":
		tree.typing, tree.typed = true, tree.filter
	}
	return m, nil
}

// openFromLeftTree shows a file over the conversation, with the same tree
// beside it, while the agent goes on; closing it comes back here.
func (m model) openFromLeftTree(path string) (tea.Model, tea.Cmd) {
	next, cmd := m.viewPath(path)
	opened := next.(model)
	if opened.viewer == nil {
		return opened, cmd
	}
	opened.leftTreeFocus = false
	opened.viewer.tree = opened.leftTree
	opened.leftTree.current = path
	opened.leftTree.show(opened.treeHeight())
	return opened, cmd
}

// inLeftTree tells whether a cell of the screen is on the tree at the left.
func (m model) inLeftTree(x, y int) bool {
	return m.leftTreeShown() && x < m.treeWidth() && y < m.panelRows()
}

// paneX is a column of the screen as the conversation and the side panel
// count it, past the tree at the left over the input.
func (m model) paneX(x, y int) int {
	if y >= m.panelRows() {
		return x
	}
	return max(x-m.leftPanelWidth(), 0)
}

// leftTreeClick is a click on the tree at the left, row y of the screen.
func (m model) leftTreeClick(y int) (tea.Model, tea.Cmd) {
	m.leftTreeFocus = true
	if file, picked := m.leftTree.pick(y); picked {
		return m.openFromLeftTree(file)
	}
	return m, nil
}

// withLeftTree puts the tree on the left of the rows over the input.
func (m model) withLeftTree(screen string) string {
	if !m.leftTreeShown() {
		return screen
	}
	width := m.treeWidth()
	rows := strings.Split(screen, "\n")
	height := min(m.panelRows(), len(rows))
	focused := m.leftTreeFocused()
	tree := m.leftTree.lines(width, height, focused, m.treeIconSet())
	if !m.leftTree.typing {
		tree[len(tree)-1] = padTo(dimStyle.Render(ansi.Truncate(leftTreeHelp, width, "…")), width)
	}
	divider := dimStyle.Render("│")
	if focused {
		divider = treeFolder().Render("│")
	}
	room := m.width - width - 1
	for i := range height {
		rows[i] = tree[i] + divider + ansi.Truncate(rows[i], room, "")
	}
	return strings.Join(rows, "\n")
}

// scrollLeftTree moves the cursor of the tree at the left as the wheel turns.
func (m *model) scrollLeftTree(up bool) {
	by := sidebarWheelStep
	if up {
		by = -by
	}
	m.leftTree.move(by, m.panelRows()-2)
}
