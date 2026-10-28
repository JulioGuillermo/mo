package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	editInputLines = 10
	editHelp       = "enter save · shift+enter new line · esc cancel"
	// editTab is how a tab of the indentation is written while editing: the
	// input draws a tab as one column.
	editTab = "    "
)

func editMark() lipgloss.Style { return lipgloss.NewStyle().Foreground(accent()).Bold(true) }

// startEdit opens the line under the cursor, or the lines selected, to
// change them in the file.
func (m model) startEdit() {
	v := m.viewer
	if v.file == "" || !v.annotatable() || v.history != nil {
		return
	}
	if len(v.lines) == 0 {
		v.lines = []string{""}
	}
	from, to := v.cursor, v.cursor
	if v.selected {
		from, to = min(v.pickFrom, v.pickTo), max(v.pickFrom, v.pickTo)
	}
	to = min(to, len(v.lines)-1)
	v.editing, v.inputFrom, v.inputTo, v.notice = true, from, to, ""
	v.input = newEditInput(editText(v.lines[from:to+1]), m.viewerWidth(), m.printer == nil || m.printer.isDark())
	m.keepCommentedInView()
}

func newEditInput(text string, width int, dark bool) textarea.Model {
	input := textarea.New()
	input.Prompt = "✎ "
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = editInputLines
	input.MaxContentHeight = 1 << 20
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	styles := inputStyles(input.Styles(), dark)
	styles.Focused.Prompt = styles.Focused.Prompt.Foreground(editMark().GetForeground())
	input.SetStyles(styles)
	input.SetWidth(width)
	input.SetValue(text)
	input.Focus()
	return input
}

// typeEdit edits the lines being changed; enter writes them to the file,
// esc leaves it as it was.
func (m model) typeEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.viewer
	switch msg.String() {
	case "enter":
		v.editing, v.selected, v.selecting = false, false, false
		if err := m.writeEdit(); err != nil {
			v.notice = err.Error()
			return m, nil
		}
		if v.blameOn {
			v.blame, v.blameNote = nil, ""
			return m, m.readBlame()
		}
	case "esc":
		v.editing = false
	default:
		return m, m.editComment(msg)
	}
	return m, nil
}

// writeEdit writes the lines edited in place of those they were, when the
// file on disk is still the one shown, keeping its line endings.
func (m model) writeEdit() error {
	v := m.viewer
	original := v.lines[v.inputFrom : v.inputTo+1]
	edited := unEditText(v.input.Value(), original)
	if strings.Join(edited, "\n") == strings.Join(original, "\n") {
		v.notice = "nothing changed"
		return nil
	}
	path := m.viewedPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	crlf := strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	finalNewline := strings.HasSuffix(text, "\n")
	if strings.TrimSuffix(text, "\n") != strings.Join(v.lines, "\n") {
		return errors.New("the file changed since it was opened: open it again to edit it")
	}
	lines := append(append(append([]string{}, v.lines[:v.inputFrom]...), edited...), v.lines[v.inputTo+1:]...)
	out := strings.Join(lines, "\n")
	if finalNewline {
		out += "\n"
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return err
	}
	m.shiftComments(v.inputTo, len(edited)-len(original))
	v.setLines(lines)
	v.cursor = min(v.inputFrom+len(edited)-1, len(lines)-1)
	v.notice = "saved " + v.editedLines()
	return nil
}

// editedLines names the lines being edited, or just saved.
func (v *outputViewer) editedLines() string {
	if v.inputTo > v.inputFrom {
		return fmt.Sprintf("lines %d-%d", v.inputFrom+1, v.inputTo+1)
	}
	return fmt.Sprintf("line %d", v.inputFrom+1)
}

// setLines shows the file's lines as they are now, coloured again.
func (v *outputViewer) setLines(lines []string) {
	v.lines = lines
	if v.language == "" {
		return
	}
	expanded := make([]string, len(lines))
	for i, line := range lines {
		expanded[i] = strings.ReplaceAll(line, "\t", "    ")
	}
	v.code = newCodeLines(expanded, v.language, 1<<30)
	v.findMatches()
}

// shiftComments moves the comments below the lines edited by the lines
// they grew or shrank by.
func (m model) shiftComments(after, by int) {
	key := m.viewer.commentKey()
	for i, comment := range m.fileComments[key] {
		if comment.from > after {
			m.fileComments[key][i].from += by
			m.fileComments[key][i].to += by
		}
	}
}

// editText is lines as the input shows them: the tabs of the indentation
// as spaces.
func editText(lines []string) string {
	shown := make([]string, len(lines))
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, "\t"))
		shown[i] = strings.Repeat(editTab, indent) + line[indent:]
	}
	return strings.Join(shown, "\n")
}

// unEditText is the lines typed as the file takes them: one left as it
// was shown is the original, and in a file indented with tabs the spaces
// of the indentation go back to tabs.
func unEditText(typed string, original []string) []string {
	tabs := false
	for _, line := range original {
		tabs = tabs || strings.HasPrefix(line, "\t")
	}
	lines := strings.Split(typed, "\n")
	for i, line := range lines {
		switch {
		case i < len(original) && line == editText(original[i:i+1]):
			lines[i] = original[i]
		case tabs:
			body := strings.TrimLeft(line, " ")
			spaces := len(line) - len(body)
			lines[i] = strings.Repeat("\t", spaces/len(editTab)) + strings.Repeat(" ", spaces%len(editTab)) + body
		}
	}
	return lines
}
