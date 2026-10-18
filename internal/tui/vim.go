package tui

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// vimMode is what keys do with vim keys on: write in the input, move and
// act, or select lines of the conversation.
type vimMode int

const (
	vimInsert vimMode = iota
	vimNormal
	vimVisual
)

// pane is the part of the screen the keys act on in normal mode.
type pane int

const (
	paneInput pane = iota
	paneConversation
	paneSidebar
)

// vimState is where vim keys are: the mode, the pane, a key waiting for its
// second (d of dd, g of gg), the line of the conversation the cursor is on
// and where a visual selection started, and the item of the side panel.
type vimState struct {
	enabled bool
	mode    vimMode
	pane    pane
	pending string
	line    int
	anchor  int
	item    int
	// register is what y, d and x took, for p; linewise when whole lines.
	register string
	linewise bool
	// undo and redo are the input as it was before each change.
	undo, redo []inputSnapshot
}

// inputSnapshot is the input's text and where its cursor was.
type inputSnapshot struct {
	value    string
	row, col int
}

const vimUndoDepth = 100

var (
	vimCursorLine = lipgloss.NewStyle().Background(lipgloss.Color("#2a3355"))
	vimPicked     = lipgloss.NewStyle().Reverse(true)
	vimColors     = map[vimMode]color.Color{
		vimInsert: lipgloss.Color("#9ece6a"),
		vimNormal: lipgloss.Color("#7aa2f7"),
		vimVisual: lipgloss.Color("#bb9af7"),
	}
)

// vimOn tells whether vim keys are in use.
func (m model) vimOn() bool {
	return m.vim.enabled
}

// vimSegment is the first part of the status line with vim keys on: the
// mode, and the pane when it is not the input, dark on the mode's colour.
func (m model) vimSegment() segment {
	if !m.vimOn() {
		return segment{}
	}
	name := map[vimMode]string{vimInsert: "INSERT", vimNormal: "NORMAL", vimVisual: "VISUAL"}[m.vim.mode]
	switch m.vim.pane {
	case paneConversation:
		name += " · chat"
	case paneSidebar:
		name += " · panel"
	}
	if m.vim.pending != "" {
		name += " " + m.vim.pending
	}
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1b26")).Background(vimColors[m.vim.mode]).Bold(true).Render(name)
	return segment{text: text, bg: vimColors[m.vim.mode]}
}

// vimKey handles a key with vim keys on, and tells whether it did; the
// keys it leaves go on as without vim.
func (m model) vimKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	pressed := msg.String()
	if m.vim.mode == vimInsert {
		if pressed == "esc" && len(m.suggestions()) == 0 {
			m.vim.mode = vimNormal
			return true, m, nil
		}
		return false, m, nil
	}
	if pressed == "backspace" {
		// Some terminals send ctrl+h as backspace.
		pressed = "ctrl+h"
	}
	switch pressed {
	case "ctrl+h", "ctrl+j", "ctrl+k", "ctrl+l":
		m.input.ClearSelection()
		m.movePane(pressed)
		return true, m, nil
	case "ctrl+c", "ctrl+d", "ctrl+x", "ctrl+b", "ctrl+o", "ctrl+v", "shift+tab", "pgup", "pgdown", "shift+up", "shift+down":
		return false, m, nil
	case "esc":
		if m.vim.mode == vimVisual {
			m.input.ClearSelection()
			m.vim.mode = vimNormal
			return true, m, nil
		}
		if m.vim.pending != "" {
			m.vim.pending = ""
			return true, m, nil
		}
		return false, m, nil
	case ":":
		m.remember()
		m.vim.mode, m.vim.pane = vimInsert, paneInput
		m.input.SetValue("/")
		m.input.CursorEnd()
		return true, m, nil
	}
	switch m.vim.pane {
	case paneConversation:
		next, cmd := m.conversationKey(pressed)
		return true, next, cmd
	case paneSidebar:
		next, cmd := m.sidebarKey(pressed)
		return true, next, cmd
	}
	next, cmd, handled := m.inputKey(pressed)
	return handled, next, cmd
}

// movePane goes to the pane in the direction of ctrl+h/j/k/l: the
// conversation above the input, the side panel at the right of both.
func (m *model) movePane(pressed string) {
	m.vim.pending = ""
	if m.vim.mode == vimVisual {
		m.vim.mode = vimNormal
	}
	switch {
	case pressed == "ctrl+j":
		m.vim.pane = paneInput
	case pressed == "ctrl+k" && m.vim.pane == paneInput:
		m.enterConversation()
	case pressed == "ctrl+l" && m.sidebarShown():
		m.vim.pane = paneSidebar
		m.vim.item = min(m.vim.item, max(len(m.sidebarItems())-1, 0))
	case pressed == "ctrl+h" && m.vim.pane == paneSidebar:
		m.enterConversation()
	}
}

// enterConversation puts the cursor on the last line in view.
func (m *model) enterConversation() {
	m.vim.pane = paneConversation
	conversation := m.conversation(m.conversationHeight())
	m.vim.line = min(conversation.YOffset()+conversation.Height()-1, max(conversation.TotalLineCount()-1, 0))
}

// inputKey moves and edits the input as vim's normal mode does; insert
// keys go back to writing, v selects.
func (m model) inputKey(pressed string) (tea.Model, tea.Cmd, bool) {
	send := func(msg tea.KeyPressMsg) {
		m.input, _ = m.input.Update(msg)
	}
	if m.vim.mode == vimVisual {
		next, cmd := m.inputVisualKey(pressed)
		return next, cmd, true
	}
	if pending := m.vim.pending; pending != "" {
		m.vim.pending = ""
		switch pending + pressed {
		case "dd":
			m.remember()
			cmd := m.yank(m.currentInputLine(), true)
			m.deleteInputLine()
			return m, cmd, true
		case "yy":
			cmd := m.yank(m.currentInputLine(), true)
			return m, tea.Batch(cmd, m.flashNote("copied the line")), true
		}
		return m, nil, true
	}
	switch pressed {
	case "i":
		m.startInsert()
	case "a":
		m.startInsert()
		send(tea.KeyPressMsg{Code: tea.KeyRight})
	case "A":
		m.startInsert()
		m.input.CursorEnd()
	case "I":
		m.startInsert()
		m.input.CursorStart()
	case "o":
		m.startInsert()
		m.input.CursorEnd()
		m.input.InsertString("\n")
	case "h", "left":
		send(tea.KeyPressMsg{Code: tea.KeyLeft})
	case "l", "right":
		send(tea.KeyPressMsg{Code: tea.KeyRight})
	case "w", "e":
		send(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	case "b":
		send(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	case "0", "^":
		m.input.CursorStart()
	case "$":
		m.input.CursorEnd()
	case "x":
		if line := []rune(m.currentInputLine()); m.input.Column() < len(line) {
			m.remember()
			cmd := m.yank(string(line[m.input.Column()]), false)
			send(tea.KeyPressMsg{Code: tea.KeyDelete})
			return m, cmd, true
		}
	case "D":
		if line := []rune(m.currentInputLine()); m.input.Column() < len(line) {
			m.remember()
			cmd := m.yank(string(line[m.input.Column():]), false)
			send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
			return m, cmd, true
		}
	case "d", "y":
		m.vim.pending = pressed
	case "p", "P":
		return m, pasteFromClipboard(pressed == "P"), true
	case "u":
		m.undoInput(true)
	case "ctrl+r":
		m.undoInput(false)
	case "v", "V":
		m.vim.mode = vimVisual
		m.input.ClearSelection()
		if pressed == "V" {
			m.input.CursorStart()
			m.selectInput(func() { m.input.CursorEnd() })
		}
	case "j", "down":
		next, cmd := m.plainKey(tea.KeyPressMsg{Code: tea.KeyDown})
		return next, cmd, true
	case "k", "up":
		next, cmd := m.plainKey(tea.KeyPressMsg{Code: tea.KeyUp})
		return next, cmd, true
	case "enter":
		return m, nil, false
	default:
		return m, nil, true
	}
	return m, nil, true
}

// inputVisualKey extends the selection of the input with the moves of
// normal mode, and copies or deletes it.
func (m model) inputVisualKey(pressed string) (tea.Model, tea.Cmd) {
	send := func(msg tea.KeyPressMsg) {
		m.input, _ = m.input.Update(msg)
	}
	switch pressed {
	case "h", "left":
		send(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	case "l", "right":
		send(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	case "w", "e":
		send(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift | tea.ModAlt})
	case "b":
		send(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift | tea.ModAlt})
	case "j", "down":
		send(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	case "k", "up":
		send(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	case "0", "^":
		m.selectInput(func() { m.input.CursorStart() })
	case "$":
		m.selectInput(func() { m.input.CursorEnd() })
	case "y":
		text := m.input.SelectedText()
		cmd := m.yank(text, false)
		m.input.ClearSelection()
		m.vim.mode = vimNormal
		return m, tea.Batch(cmd, m.flashNote(fmt.Sprintf("copied %d characters", len([]rune(text)))))
	case "d", "x":
		m.vim.mode = vimNormal
		if m.input.HasSelection() {
			m.remember()
			cmd := m.yank(m.input.SelectedText(), false)
			m.input.DeleteSelection()
			return m, cmd
		}
	case "v", "V":
		m.input.ClearSelection()
		m.vim.mode = vimNormal
	}
	return m, nil
}

// selectInput extends the selection to where move puts the cursor, a
// character at a time as shift and the arrows do.
func (m *model) selectInput(move func()) {
	target := m.input
	move()
	row, col := m.input.Line(), m.input.Column()
	m.input = target
	for range 10000 {
		switch {
		case m.input.Line() < row || (m.input.Line() == row && m.input.Column() < col):
			m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
		case m.input.Line() > row || (m.input.Line() == row && m.input.Column() > col):
			m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
		default:
			return
		}
	}
}

// startInsert goes to writing, remembering the input to undo all of it at
// once.
func (m *model) startInsert() {
	m.remember()
	m.vim.mode = vimInsert
}

// remember keeps the input as it is, to undo what comes next.
func (m *model) remember() {
	m.vim.undo = append(m.vim.undo, m.snapshot())
	if len(m.vim.undo) > vimUndoDepth {
		m.vim.undo = m.vim.undo[1:]
	}
	m.vim.redo = nil
}

func (m *model) snapshot() inputSnapshot {
	return inputSnapshot{value: m.input.Value(), row: m.input.Line(), col: m.input.Column()}
}

// undoInput puts the input back as it was before the last change, or
// again as it was after it.
func (m *model) undoInput(undo bool) {
	from, to := &m.vim.undo, &m.vim.redo
	if !undo {
		from, to = to, from
	}
	for len(*from) > 0 {
		last := (*from)[len(*from)-1]
		*from = (*from)[:len(*from)-1]
		if last.value == m.input.Value() {
			continue
		}
		*to = append(*to, m.snapshot())
		m.restore(last)
		return
	}
}

func (m *model) restore(snapshot inputSnapshot) {
	m.input.SetValue(snapshot.value)
	m.input.MoveToBegin()
	for range snapshot.row {
		before := m.input.Line()
		m.input.CursorDown()
		if m.input.Line() == before {
			break
		}
	}
	m.input.SetCursorColumn(snapshot.col)
}

// yank keeps text for p, and puts it on the clipboard, a line break after
// whole lines as vim does.
func (m *model) yank(text string, linewise bool) tea.Cmd {
	m.vim.register, m.vim.linewise = text, linewise
	if linewise {
		text += "\n"
	}
	return tea.Batch(tea.SetClipboard(text), copyToSystem(text))
}

// vimPasteMsg is the clipboard read for p or P.
type vimPasteMsg struct {
	text   string
	ok     bool
	before bool
}

// pasteFromClipboard reads the clipboard for p, which pastes the register
// when it cannot be read.
func pasteFromClipboard(before bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		text, err := readClipboardText(ctx)
		return vimPasteMsg{text: text, ok: err == nil, before: before}
	}
}

// vimPasted pastes what the clipboard held: whole lines when it ends in a
// line break or is what a line yank put there.
func (m model) vimPasted(msg vimPasteMsg) (tea.Model, tea.Cmd) {
	if msg.ok && msg.text != "" {
		text := strings.TrimSuffix(msg.text, "\n")
		linewise := strings.HasSuffix(msg.text, "\n") || (m.vim.linewise && text == m.vim.register)
		m.vim.register, m.vim.linewise = text, linewise
	}
	m.paste(msg.before)
	return m, nil
}

// paste puts the register after the cursor, or before with P; whole lines
// go below the line, or above.
func (m *model) paste(before bool) {
	if m.vim.register == "" {
		return
	}
	m.remember()
	switch {
	case m.vim.linewise && before:
		m.input.CursorStart()
		m.input.InsertString(m.vim.register + "\n")
	case m.vim.linewise:
		m.input.CursorEnd()
		m.input.InsertString("\n" + m.vim.register)
	case before:
		m.input.InsertString(m.vim.register)
	default:
		if m.input.Column() < len([]rune(m.currentInputLine())) {
			m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		}
		m.input.InsertString(m.vim.register)
	}
}

// currentInputLine is the line of the input the cursor is on.
func (m model) currentInputLine() string {
	lines := strings.Split(m.input.Value(), "\n")
	return lines[min(m.input.Line(), len(lines)-1)]
}

// flashNote says something in the status line for a moment.
func (m *model) flashNote(text string) tea.Cmd {
	m.flashes++
	m.flash = text
	number := m.flashes
	return tea.Tick(flashTime, func(time.Time) tea.Msg { return flashDoneMsg(number) })
}

// deleteInputLine deletes the line of the input the cursor is on.
func (m *model) deleteInputLine() {
	lines := strings.Split(m.input.Value(), "\n")
	if len(lines) <= 1 {
		m.input.Reset()
		return
	}
	at := min(m.input.Line(), len(lines)-1)
	m.input.SetValue(strings.Join(append(lines[:at:at], lines[at+1:]...), "\n"))
}

// conversationKey moves the cursor over the conversation, opens and folds
// what it is on, and selects and copies lines.
func (m model) conversationKey(pressed string) (tea.Model, tea.Cmd) {
	total := m.conversation(m.conversationHeight()).TotalLineCount()
	half := max(m.conversationHeight()/2, 1)
	if m.vim.pending == "g" {
		m.vim.pending = ""
		if pressed == "g" {
			m.moveLine(-total)
		}
		return m, nil
	}
	switch pressed {
	case "j", "down":
		m.moveLine(1)
	case "k", "up":
		m.moveLine(-1)
	case "ctrl+d":
		m.moveLine(half)
	case "ctrl+u":
		m.moveLine(-half)
	case "g":
		m.vim.pending = "g"
	case "G":
		m.moveLine(total)
	case "l", "enter", "o":
		m.foldAt(m.vim.line, true)
	case "h":
		m.foldAt(m.vim.line, false)
	case "v", "V":
		if m.vim.mode == vimVisual {
			m.vim.mode = vimNormal
		} else {
			m.vim.mode, m.vim.anchor = vimVisual, m.vim.line
		}
	case "y":
		from, to := m.vim.line, m.vim.line
		if m.vim.mode == vimVisual {
			from, to = min(m.vim.anchor, m.vim.line), max(m.vim.anchor, m.vim.line)
		}
		m.vim.mode = vimNormal
		return m.copyLines(from, to)
	case "i", "a", "A", "I":
		m.vim.mode, m.vim.pane = vimInsert, paneInput
	}
	return m, nil
}

// moveLine moves the conversation's cursor by lines and scrolls to keep it
// in view.
func (m *model) moveLine(by int) {
	height := m.conversationHeight()
	conversation := m.conversation(height)
	m.vim.line = min(max(m.vim.line+by, 0), max(conversation.TotalLineCount()-1, 0))
	switch {
	case m.vim.line < conversation.YOffset():
		conversation.SetYOffset(m.vim.line)
	case m.vim.line >= conversation.YOffset()+height:
		conversation.SetYOffset(m.vim.line - height + 1)
	}
	m.top = conversation.YOffset()
	m.scrolled = !conversation.AtBottom()
}

// foldAt opens or folds the block the line belongs to.
func (m *model) foldAt(line int, open bool) {
	i, found := m.conversationLines().blockAt(line)
	if !found || !m.blocks[i].foldable || m.blocks[i].open == open {
		return
	}
	m.blocks = slices.Clone(m.blocks)
	m.blocks[i].open = open
	m.blocks[i].redraw(open)
	m.vim.line = min(m.vim.line, max(m.conversation(m.conversationHeight()).TotalLineCount()-1, 0))
}

// copyLines copies lines of the conversation, as they read.
func (m model) copyLines(from, to int) (tea.Model, tea.Cmd) {
	content := m.conversationContent()
	lines := strings.Split(content, "\n")
	if from >= len(lines) {
		return m, nil
	}
	to = min(to, len(lines)-1)
	var picked []string
	for _, line := range lines[from : to+1] {
		picked = append(picked, strings.TrimRight(ansi.Strip(line), " "))
	}
	text := strings.Join(picked, "\n")
	cmd := m.yank(text, true)
	return m, tea.Batch(cmd, m.flashNote(fmt.Sprintf("copied %d lines", to-from+1)))
}

// conversationContent is what the conversation pane holds, every line.
func (m model) conversationContent() string {
	transcript, _ := m.transcript()
	return turning(strings.TrimRight(transcript+m.liveText(), "\n"))
}

// markLines shows the cursor line, or the lines selected, when the
// conversation has the keys, in lines of it from line first.
func (m model) markLines(lines []string, first int) []string {
	if !m.vimOn() || m.vim.pane != paneConversation || m.vim.mode == vimInsert {
		return lines
	}
	from, to := m.vim.line-first, m.vim.line-first
	style := vimCursorLine
	if m.vim.mode == vimVisual {
		from, to, style = min(m.vim.anchor, m.vim.line)-first, max(m.vim.anchor, m.vim.line)-first, vimPicked
	}
	width := m.mainWidth()
	for i := max(from, 0); i <= to && i < len(lines); i++ {
		plain := ansi.Strip(lines[i])
		lines[i] = style.Render(plain + strings.Repeat(" ", max(width-ansi.StringWidth(plain), 0)))
	}
	return lines
}

// sidebarItems are the first rows of the tasks and jobs of the side panel,
// each opening its menu.
func (m model) sidebarItems() []region {
	_, regions := m.sidebarLines()
	var items []region
	seen := map[string]bool{}
	for _, r := range regions {
		if r.menu != nil && r.item != "" && !seen[r.item] {
			seen[r.item] = true
			items = append(items, r)
		}
	}
	return items
}

// sidebarKey moves between the tasks and jobs of the side panel and opens
// the menu of the one the cursor is on.
func (m model) sidebarKey(pressed string) (tea.Model, tea.Cmd) {
	items := m.sidebarItems()
	switch pressed {
	case "j", "down":
		m.vim.item = min(m.vim.item+1, max(len(items)-1, 0))
	case "k", "up":
		m.vim.item = max(m.vim.item-1, 0)
	case "l", "enter":
		if m.vim.item < len(items) {
			return items[m.vim.item].menu(m)
		}
	case "h":
		m.enterConversation()
	case "i", "a":
		m.vim.mode, m.vim.pane = vimInsert, paneInput
	}
	return m, nil
}

// markSidebar shows which item of the side panel the cursor is on.
func (m model) markSidebar(lines []string, regions []region) []string {
	if !m.vimOn() || m.vim.pane != paneSidebar {
		return lines
	}
	items := m.sidebarItems()
	if m.vim.item >= len(items) {
		return lines
	}
	y := items[m.vim.item].y
	if y >= 0 && y < len(lines) {
		lines[y] = vimPicked.Render(ansi.Strip(lines[y]))
	}
	return lines
}

// vimSettingsKey turns h and l into back and in on a list of the settings.
func (m model) vimSettingsKey(msg tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	if !m.vimOn() {
		return msg, false
	}
	list, isList := m.settings.form.GetFocusedField().(*huh.Select[string])
	if !isList || list.GetFiltering() {
		return msg, false
	}
	switch msg.String() {
	case "l":
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case "h":
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	}
	return msg, false
}

// lowerLayoutText is the input and status line as drawn, for tests and the
// screen alike.
func (m model) lowerLayoutText() string {
	text, _ := m.lowerLayout()
	return text
}
