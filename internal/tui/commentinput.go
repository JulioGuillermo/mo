package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

const commentInputLines = 3

const commentHelp = "enter keep · shift+enter new line · esc cancel · empty deletes it"

// newCommentInput is where a comment on lines is written: as the input of
// a message, growing to three lines and scrolled inside past them.
func newCommentInput(text string, width int, dark bool) textarea.Model {
	input := textarea.New()
	input.Prompt = "◆ "
	input.Placeholder = "what to tell the model about these lines"
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = commentInputLines
	input.MaxContentHeight = 1 << 20
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	styles := inputStyles(input.Styles(), dark)
	styles.Focused.Prompt = styles.Focused.Prompt.Foreground(commentMark().GetForeground())
	input.SetStyles(styles)
	input.SetWidth(width)
	input.SetValue(text)
	input.Focus()
	return input
}

// editComment gives the comment or the lines being written a key or a
// paste, and keeps the lines it is on in view as it grows.
func (m model) editComment(msg tea.Msg) tea.Cmd {
	v := m.viewer
	v.input.SetWidth(m.viewerWidth())
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	m.keepCommentedInView()
	return cmd
}

// inputRows is how many rows the comment or the lines being written take
// over the help line.
func (m model) inputRows() int {
	if m.viewer == nil || !m.viewer.inputOpen() {
		return 0
	}
	return m.viewer.input.Height()
}

// inputOpen tells whether a comment or the lines are being written.
func (v *outputViewer) inputOpen() bool {
	return v.commenting || v.editing
}

// onCursor tells whether row i is drawn as the cursor's: the cursor, or a
// line being commented or edited.
func (v *outputViewer) onCursor(i int) bool {
	if v.inputOpen() {
		return i >= v.inputFrom && i <= v.inputTo
	}
	return i == v.cursor
}

// keepCommentedInView scrolls so the last line commented or edited stays
// in view above the input.
func (m model) keepCommentedInView() {
	v := m.viewer
	rows := m.viewerRows()
	switch {
	case v.inputTo >= v.top+rows:
		v.top = v.inputTo - rows + 1
	case v.inputFrom < v.top:
		v.top = v.inputFrom
	}
}
