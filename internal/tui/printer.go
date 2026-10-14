package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const toolPreviewLines = 4

// Images and diagrams start where the text beside them does: an answer's
// two columns in, a tool result's four, after its ⎿.
const (
	answerIndent = 2
	toolIndent   = 4
)

const shownCommandLines = 6

var (
	accent       = lipgloss.Color("#7aa2f7")
	warning      = lipgloss.Color("#e0af68")
	dimStyle     = lipgloss.NewStyle().Faint(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e"))
	userStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	toolStyle    = lipgloss.NewStyle().Foreground(accent)
	toolName     = lipgloss.NewStyle().Foreground(accent).Bold(true)
	keyStyle     = lipgloss.NewStyle().Foreground(warning).Bold(true)
	workingStyle = lipgloss.NewStyle().Foreground(warning)
	commandEdge  = lipgloss.NewStyle().Foreground(accent).Faint(true)
)

// panel is the background of the user's messages and of commands, a shade
// off the terminal's own.
func panel(dark bool) lipgloss.Style {
	if dark {
		return lipgloss.NewStyle().Background(lipgloss.Color("#24283b")).Foreground(lipgloss.Color("#c0caf5"))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color("#e1e2e7")).Foreground(lipgloss.Color("#3760bf"))
}

// printer formats what goes into the terminal scrollback. It is shared by the
// UI goroutine and the agent goroutine.
type printer struct {
	mu       sync.Mutex
	width    int
	dark     bool
	markdown *glamour.TermRenderer
	images   render.Mode
	workDir  string
}

func newPrinter() *printer {
	return &printer{width: 80, dark: true, images: render.ModeOff}
}

func (p *printer) setImages(mode render.Mode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.images = mode
}

func (p *printer) renderer() render.Renderer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return render.Renderer{Mode: p.images, Width: p.width}
}

// Answer prints an answer of the model: markdown, with its mermaid and svg
// blocks and its images of local files drawn, after a blank line.
func (p *printer) Answer(text string) string {
	parts := []string{""}
	for _, segment := range render.Split(text) {
		parts = append(parts, p.segment(segment))
	}
	return strings.Join(parts, "\n")
}

func (p *printer) segment(segment render.Segment) string {
	renderer := p.rendererAt(answerIndent)
	switch segment.Kind {
	case render.SegmentMermaid:
		return indentLines(renderer.Mermaid(segment.Content), answerIndent)
	case render.SegmentSVG:
		drawn, err := renderer.SVG([]byte(segment.Content))
		if err != nil {
			return p.Markdown("```svg\n"+segment.Content+"```") + "\n" + p.Error(err)
		}
		return indentLines(drawn, answerIndent)
	case render.SegmentImageFile:
		return p.File(segment.Content)
	}
	return p.Markdown(segment.Content)
}

// File draws an image, SVG or Mermaid file, or says why it cannot.
func (p *printer) File(path string) string {
	return p.fileAt(path, answerIndent)
}

// fileAt draws a file indented as the text around it, in the width left.
func (p *printer) fileAt(path string, indent int) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.workDir, path)
	}
	drawn, err := p.rendererAt(indent).File(path)
	if err != nil {
		return p.Error(err)
	}
	return indentLines(drawn, indent)
}

// rendererAt draws in the width left beside an indent.
func (p *printer) rendererAt(indent int) render.Renderer {
	renderer := p.renderer()
	renderer.Width = max(renderer.Width-indent, 10)
	return renderer
}

// indentLines moves every drawn line right, so an image or diagram lines
// up with the text it belongs to. Escape sequences at a line's start, as a
// kitty upload, stay where they are: they take no room.
func indentLines(text string, indent int) string {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// Images draws the images a tool result points at.
func (p *printer) Images(output string) string {
	var drawn []string
	for _, path := range render.MarkedImages(output) {
		drawn = append(drawn, p.fileAt(path, toolIndent))
	}
	return strings.Join(drawn, "\n")
}

func (p *printer) setWidth(width int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if width != p.width {
		p.width = width
		p.markdown = nil
	}
}

func (p *printer) isDark() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dark
}

func (p *printer) setDark(dark bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if dark != p.dark {
		p.dark = dark
		p.markdown = nil
	}
}

// Markdown renders text, with its tables drawn as stripes.
func (p *printer) Markdown(text string) string {
	parts := splitTables(text)
	if len(parts) == 1 && !parts[0].isTable {
		return p.glamour(text)
	}
	var out []string
	for _, part := range parts {
		switch {
		case part.isTable:
			out = append(out, p.Table(part.text))
		case strings.TrimSpace(part.text) != "":
			out = append(out, p.glamour(part.text))
		}
	}
	return strings.Join(out, "\n\n")
}

func (p *printer) glamour(text string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.markdown == nil {
		renderer, err := glamour.NewTermRenderer(glamour.WithStyles(markdownStyle(p.dark)), glamour.WithWordWrap(p.width-4))
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

// User prints a message of the user as a full-width band, every line of it.
func (p *printer) User(text string) string {
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	band := panel(dark).Width(max(width, 20)).Padding(0, 1)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		prefix := "  "
		if i == 0 {
			prefix = "› "
		}
		lines[i] = prefix + line
	}
	return "\n" + band.Render(strings.Join(lines, "\n"))
}

// ToolStart names the tool and shows what it is about to do, a command on
// its own shaded lines, after a blank line.
func (p *printer) ToolStart(name, summary string) string {
	header := "\n" + toolName.Render("● "+name)
	if !strings.Contains(summary, "\n") && len(summary) < 60 && name != "bash" {
		return header + " " + summary
	}
	return header + "\n" + p.command(summary)
}

func (p *printer) command(text string) string {
	return p.commandLines(text, false)
}

// commandLines shades a command, its first lines cut to the width, or all
// of it wrapped.
func (p *printer) commandLines(text string, all bool) string {
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	hidden := len(lines) - shownCommandLines
	if hidden > 0 && !all {
		lines = lines[:shownCommandLines]
	}
	shade := panel(dark).Width(max(width-4, 16)).Padding(0, 1)
	var out []string
	for _, line := range lines {
		pieces := []string{p.clip(line)}
		if all {
			pieces = p.wrapped(line, 8)
		}
		for _, piece := range pieces {
			out = append(out, commandEdge.Render("  ▌")+shade.Render(piece))
		}
	}
	if hidden > 0 && !all {
		out = append(out, dimStyle.Render(fmt.Sprintf("    … +%d lines", hidden)))
	}
	return strings.Join(out, "\n")
}

// wrapped splits a line, without its styles, into pieces that fit the width
// less margin.
func (p *printer) wrapped(line string, margin int) []string {
	p.mu.Lock()
	limit := max(p.width-margin, 10)
	p.mu.Unlock()
	line = strings.ReplaceAll(ansi.Strip(line), "\t", "    ")
	return strings.Split(ansi.Hardwrap(line, limit, true), "\n")
}

// Tool shows a tool call and its result: folded, the first lines of each
// and a hint when there is more; open, all of both.
func (p *printer) Tool(run toolRun, open bool) string {
	var parts []string
	change, changed := fileChangeOf(run.name, run.args)
	switch {
	case open && changed:
		parts = append(parts, "\n"+toolName.Render("● "+run.name)+" "+run.summary+dimStyle.Render("  "+change.summary())+"\n"+p.fileChangeLines(change))
	case open:
		parts = append(parts, "\n"+toolName.Render("● "+run.name)+"\n"+p.commandLines(run.summary, true))
	case changed:
		parts = append(parts, p.ToolStart(run.name, run.summary+dimStyle.Render("  "+change.summary())))
	default:
		parts = append(parts, p.ToolStart(run.name, run.summary))
	}
	switch {
	case !run.done:
		parts = append(parts, dimStyle.Render("  ⎿ running…"))
	case open:
		parts = append(parts, p.fullResult(run.output, run.isError))
	default:
		parts = append(parts, p.ToolResult(run.output, run.isError))
	}
	switch {
	case open:
		parts = append(parts, dimStyle.Render("    ▾ ctrl+o or click to fold"))
	case p.toolHidesSome(run):
		parts = append(parts, dimStyle.Render("    ▸ ctrl+o or click for the whole call and output"))
	}
	return strings.Join(parts, "\n")
}

// toolHidesSome tells whether the folded view leaves lines out or cuts them.
func (p *printer) toolHidesSome(run toolRun) bool {
	if _, changed := fileChangeOf(run.name, run.args); changed {
		return true
	}
	command := strings.Split(strings.TrimRight(run.summary, "\n"), "\n")
	output := strings.Split(strings.TrimRight(run.output, "\n"), "\n")
	if len(command) > shownCommandLines || len(output) > toolPreviewLines {
		return true
	}
	for _, line := range append(command, output...) {
		if p.clip(line) != strings.ReplaceAll(ansi.Strip(line), "\t", "    ") {
			return true
		}
	}
	return false
}

// fullResult is every line of a result, wrapped to the width.
func (p *printer) fullResult(output string, isError bool) string {
	style := dimStyle
	if isError {
		style = errorStyle
	}
	var out []string
	for i, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		for j, piece := range p.wrapped(line, 6) {
			prefix := "    "
			if i == 0 && j == 0 {
				prefix = "  ⎿ "
			}
			out = append(out, style.Render(prefix+piece))
		}
	}
	return strings.Join(out, "\n")
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

// Error shows an error; a line too long for the terminal goes on under
// itself, aligned with where it starts, or under a JSON key's value.
func (p *printer) Error(err error) string {
	p.mu.Lock()
	width := p.width - 2
	p.mu.Unlock()
	return errorStyle.Render(hangingWrap("✗ "+err.Error(), width))
}

var jsonKey = regexp.MustCompile(`^\s*"[^"]*":\s*`)

// hangingWrap wraps each line at width, its next lines indented as far as
// its text starts, or its value when it is a "key": value line.
func hangingWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if key := jsonKey.FindString(line); key != "" {
			indent = ansi.StringWidth(key)
		}
		if indent > width/2 {
			indent = 0
		}
		first := ansi.Wrap(line, width, " ")
		head, rest, _ := strings.Cut(first, "\n")
		out = append(out, head)
		rest = strings.ReplaceAll(rest, "\n", " ")
		for _, piece := range strings.Split(ansi.Wrap(strings.TrimLeft(rest, " "), width-indent, " "), "\n") {
			out = append(out, strings.Repeat(" ", indent)+piece)
		}
	}
	return strings.Join(out, "\n")
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
