package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const shownTodos = 8

// followTodos keeps the model's task list from a todo call: each call sends
// the whole list.
func (m *model) followTodos(call llm.ToolCall) {
	if call.Name != "todo" {
		return
	}
	if todos, err := tools.ParseTodos(call.Args); err == nil {
		m.todos = todos
	}
}

// todosFrom finds the list in force in a history: the latest todo call's,
// also one made through tool_call.
func (m model) todosFrom(history []llm.Message) []tools.Todo {
	for i := len(history) - 1; i >= 0; i-- {
		calls := history[i].ToolCalls
		for j := len(calls) - 1; j >= 0; j-- {
			call := m.unwrap(calls[j])
			if call.Name != "todo" {
				continue
			}
			if todos, err := tools.ParseTodos(call.Args); err == nil {
				return todos
			}
		}
	}
	return nil
}

// todoPanel shows the list above the input while some task is left: the one
// in progress stands out, finished ones are struck through. A long list
// counts the finished ones and shows what is left.
func (m model) todoPanel() string {
	done, _ := tools.TodoProgress(m.todos)
	if len(m.todos) == 0 || done == len(m.todos) || m.sidebarShown() {
		return ""
	}
	colors := statusPalette(m.printer == nil || m.printer.isDark())
	shown := m.todos
	var lines []string
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  todo %d/%d", done, len(m.todos))))
	if len(shown) > shownTodos {
		var left []tools.Todo
		for _, todo := range shown {
			if todo.Status != tools.TodoCompleted {
				left = append(left, todo)
			}
		}
		if done > 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(colors.label).Render(fmt.Sprintf("  ✓ %d done", done)))
		}
		shown = left
	}
	hidden := max(len(shown)-shownTodos, 0)
	for _, todo := range shown[:len(shown)-hidden] {
		text := oneLine(todo.Text, max(m.width-6, 20))
		switch todo.Status {
		case tools.TodoCompleted:
			lines = append(lines, lipgloss.NewStyle().Foreground(colors.label).Strikethrough(true).Render("  ✓ "+text))
		case tools.TodoInProgress:
			lines = append(lines, lipgloss.NewStyle().Foreground(colors.mid).Bold(true).Render("  ◐ "+text))
		default:
			lines = append(lines, "  ○ "+text)
		}
	}
	if hidden > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("    … +%d more", hidden)))
	}
	return onPanel(lines, max(m.width, 20), sidebarPalette(m.printer == nil || m.printer.isDark()).background)
}

// onPanel puts lines on a background the whole width, as the side panel
// has: their own styles end where they do, so each is laid on it again.
func onPanel(lines []string, width int, background color.Color) string {
	base := lipgloss.NewStyle().Background(background)
	out := []string{base.Render(strings.Repeat(" ", width))}
	for _, line := range lines {
		line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+backgroundCode(background))
		line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+backgroundCode(background))
		out = append(out, backgroundCode(background)+line+base.Render(strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))))
	}
	return strings.Join(append(out, base.Render(strings.Repeat(" ", width))), "\n")
}

// backgroundCode is the sequence that sets a background colour.
func backgroundCode(background color.Color) string {
	r, g, b, _ := background.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}
