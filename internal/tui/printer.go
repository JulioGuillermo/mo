package tui

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const toolPreviewLines = 4

var (
	accent     = lipgloss.Color("#7aa2f7")
	dimStyle   = lipgloss.NewStyle().Faint(true)
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e"))
	userStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	toolStyle  = lipgloss.NewStyle().Foreground(accent)
)

// printer formats what goes into the terminal scrollback. It is shared by the
// UI goroutine and the agent goroutine.
type printer struct {
	mu       sync.Mutex
	width    int
	dark     bool
	markdown *glamour.TermRenderer
}

func newPrinter() *printer {
	return &printer{width: 80, dark: true}
}

func (p *printer) setWidth(width int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if width != p.width {
		p.width = width
		p.markdown = nil
	}
}

func (p *printer) setDark(dark bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if dark != p.dark {
		p.dark = dark
		p.markdown = nil
	}
}

func (p *printer) Markdown(text string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.markdown == nil {
		style := "dark"
		if !p.dark {
			style = "light"
		}
		renderer, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(p.width-4))
		if err != nil {
			return text
		}
		p.markdown = renderer
	}
	rendered, err := p.markdown.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(rendered, "\n")
}

func (p *printer) User(text string) string {
	return "\n" + userStyle.Render("› ") + text
}

func (p *printer) ToolStart(name, summary string) string {
	return toolStyle.Render("● "+name) + " " + firstLine(summary)
}

// ToolResult shows the first lines of a result, enough to see what happened
// without flooding the scrollback.
func (p *printer) ToolResult(output string, isError bool) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	shown := lines[:min(len(lines), toolPreviewLines)]
	style := dimStyle
	if isError {
		style = errorStyle
	}
	var out strings.Builder
	for i, line := range shown {
		prefix := "    "
		if i == 0 {
			prefix = "  ⎿ "
		}
		out.WriteString(style.Render(prefix + p.clip(line)))
		out.WriteByte('\n')
	}
	if hidden := len(lines) - len(shown); hidden > 0 {
		out.WriteString(dimStyle.Render(fmt.Sprintf("    … +%d lines", hidden)))
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

func (p *printer) Denied(name string) string {
	return errorStyle.Render("  ⎿ denied: " + name)
}

func (p *printer) Error(err error) string {
	return errorStyle.Render("✗ " + err.Error())
}

func (p *printer) Note(text string) string {
	return dimStyle.Render(text)
}

func (p *printer) clip(line string) string {
	p.mu.Lock()
	limit := p.width - 6
	p.mu.Unlock()
	line = strings.ReplaceAll(ansi.Strip(line), "\t", "    ")
	runes := []rune(line)
	if limit <= 0 || len(runes) <= limit {
		return line
	}
	return string(runes[:limit-1]) + "…"
}

func firstLine(text string) string {
	line, _, multiline := strings.Cut(text, "\n")
	if multiline {
		return line + " …"
	}
	return line
}
