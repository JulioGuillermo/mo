package tui

import (
	"fmt"
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
}

var (
	vimCursorLine = lipgloss.NewStyle().Background(lipgloss.Color("#2a3355"))
	vimPicked     = lipgloss.NewStyle().Reverse(true)
	vimBadges     = map[vimMode]lipgloss.Style{
		vimInsert: lipgloss.NewStyle().Background(lipgloss.Color("#9ece6a")).Foreground(lipgloss.Color("#1a1b26")).Bold(true),
		vimNormal: lipgloss.NewStyle().Background(lipgloss.Color("#7aa2f7")).Foreground(lipgloss.Color("#1a1b26")).Bold(true),
		vimVisual: lipgloss.NewStyle().Background(lipgloss.Color("#bb9af7")).Foreground(lipgloss.Color("#1a1b26")).Bold(true),
	}
)

// vimOn tells whether vim keys are in use.
func (m model) vimOn() bool {
	return m.vim.enabled
}

// vimBadge names the mode and, out of the input, the pane, before the
// status line.
func (m model) vimBadge() string {
	if !m.vimOn() {
		return ""
	}
	name := map[vimMode]string{vimInsert: "INSERT", vimNormal: "NORMAL", vimVisual: "VISUAL"}[m.vim.mode]
	switch m.vim.pane {
	case paneConversation:
		name += " · conversation"
	case paneSidebar:
		name += " · panel"
	}
	if m.vim.pending != "" {
		name += " " + m.vim.pending
	}
	return vimBadges[m.vim.mode].Render(" "+name+" ") + " "
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
		m.movePane(pressed)
		return true, m, nil
	case "ctrl+c", "ctrl+d", "ctrl+b", "ctrl+o", "ctrl+v", "shift+tab", "pgup", "pgdown", "shift+up", "shift+down":
		return false, m, nil
	case "esc":
		if m.vim.mode == vimVisual {
			m.vim.mode = vimNormal
			return true, m, nil
		}
		if m.vim.pending != "" {
			m.vim.pending = ""
			return true, m, nil
		}
		return false, m, nil
	case ":":
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
// keys go back to writing.
func (m model) inputKey(pressed string) (tea.Model, tea.Cmd, bool) {
	send := func(msg tea.KeyPressMsg) {
		m.input, _ = m.input.Update(msg)
	}
	if m.vim.pending == "d" {
		m.vim.pending = ""
		if pressed == "d" {
			m.deleteInputLine()
		}
		return m, nil, true
	}
	switch pressed {
	case "i":
		m.vim.mode = vimInsert
	case "a":
		m.vim.mode = vimInsert
		send(tea.KeyPressMsg{Code: tea.KeyRight})
	case "A":
		m.vim.mode = vimInsert
		m.input.CursorEnd()
	case "I":
		m.vim.mode = vimInsert
		m.input.CursorStart()
	case "o":
		m.vim.mode = vimInsert
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
		send(tea.KeyPressMsg{Code: tea.KeyDelete})
	case "D":
		send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	case "d":
		m.vim.pending = "d"
	case "j", "down":
		next, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
		return next, cmd, true
	case "k", "up":
		next, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		return next, cmd, true
	case "enter":
		return m, nil, false
	default:
		return m, nil, true
	}
	return m, nil, true
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
	m.viewport = conversation
	m.scrolled = !conversation.AtBottom()
}

// foldAt opens or folds the block the line belongs to.
func (m *model) foldAt(line int, open bool) {
	_, foldable := m.transcript()
	i, found := foldable[line]
	if !found || m.blocks[i].open == open {
		return
	}
	m.blocks = slices.Clone(m.blocks)
	m.blocks[i].open = open
	m.blocks[i].text = m.blocks[i].draw(open)
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
	m.flashes++
	m.flash = fmt.Sprintf("copied %d lines", to-from+1)
	number := m.flashes
	return m, tea.Batch(tea.SetClipboard(text), copyToSystem(text),
		tea.Tick(flashTime, func(time.Time) tea.Msg { return flashDoneMsg(number) }))
}

// conversationContent is what the conversation pane holds, every line.
func (m model) conversationContent() string {
	transcript, _ := m.transcript()
	return strings.TrimRight(transcript+m.liveText(), "\n")
}

// markConversation shows the cursor line, or the lines selected, when the
// conversation has the keys.
func (m model) markConversation(content string) string {
	if !m.vimOn() || m.vim.pane != paneConversation || m.vim.mode == vimInsert {
		return content
	}
	lines := strings.Split(content, "\n")
	from, to := m.vim.line, m.vim.line
	style := vimCursorLine
	if m.vim.mode == vimVisual {
		from, to, style = min(m.vim.anchor, m.vim.line), max(m.vim.anchor, m.vim.line), vimPicked
	}
	width := m.mainWidth()
	for i := max(from, 0); i <= to && i < len(lines); i++ {
		plain := ansi.Strip(lines[i])
		lines[i] = style.Render(plain + strings.Repeat(" ", max(width-ansi.StringWidth(plain), 0)))
	}
	return strings.Join(lines, "\n")
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
