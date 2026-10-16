package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
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
	// drawings are the diagrams drawn wider than the conversation, to
	// open whole when clicked, by their text.
	drawings     []drawing
	drawingIndex map[string]int
	// kind tells what a tool call does, for the colour of its mark.
	kind func(call llm.ToolCall) toolKind
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
	return render.Renderer{Mode: p.images, Width: p.width, Dark: p.dark}
}

// Answer prints an answer of the model: markdown, with its mermaid and svg
// blocks and its images of local files drawn, after a blank line; a
// drawing, with its line under it, is set apart from the text around it
// by a blank line, the text itself as it is.
func (p *printer) Answer(text string) string {
	var out strings.Builder
	previous := render.SegmentKind(-1)
	for _, segment := range render.Split(text) {
		drawn := trimBlankLines(p.segment(segment))
		if drawn == "" {
			continue
		}
		out.WriteString("\n")
		if previous >= 0 && (previous != render.SegmentText || segment.Kind != render.SegmentText) {
			out.WriteString("\n")
		}
		out.WriteString(drawn)
		previous = segment.Kind
	}
	return out.String()
}

// trimBlankLines takes off the blank lines a part starts or ends with.
func trimBlankLines(text string) string {
	lines := strings.Split(text, "\n")
	blank := func(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func (p *printer) segment(segment render.Segment) string {
	renderer := p.rendererAt(answerIndent)
	switch segment.Kind {
	case render.SegmentMermaid:
		return p.diagram(segment.Content, indentLines(renderer.Mermaid(segment.Content), answerIndent))
	case render.SegmentSVG:
		drawn, err := renderer.SVG([]byte(segment.Content))
		if err != nil {
			return p.Markdown("```svg\n"+segment.Content+"```") + "\n" + p.Error(err)
		}
		return p.picture("svg", segment.Content, indentLines(drawn, answerIndent))
	case render.SegmentImageFile:
		drawn, err := p.drawFile(segment.Content, answerIndent)
		if err != nil {
			return p.Error(err)
		}
		return drawn + "\n" + p.caption(segment.Name, segment.Content, answerIndent)
	}
	return p.Markdown(segment.Content)
}

// drawing is a diagram an answer drew with characters: its source, its
// lines coloured and plain as drawn, how far it is moved across when wider
// than the conversation, whether its code shows instead, and its lines as
// shown so, with the line under it.
type drawing struct {
	source       string
	lines, plain []string
	widest, left int
	code         bool
	shown        []string
	// picture is set for one drawn as an image, an SVG or a diagram
	// mermaid-cli drew: its text as the conversation keeps it, uploads
	// left out, and its code as shown in its place. An image cannot be
	// drawn again, so the one shown is swapped for the other.
	picture  string
	codeText string
}

// drawingStep is how many columns ctrl+←→ moves the wide diagrams.
const drawingStep = 8

// diagram keeps a diagram drawn with characters, to show its code or open
// it whole: drawn, or as code when the user asked; one wider than the
// conversation moved across as far as ctrl+←→ left it, saying so in a
// line under it. One drawn as an image is as it was drawn.
func (p *printer) diagram(source, drawn string) string {
	if strings.Contains(drawn, "\x1b_G") || strings.Contains(drawn, "\x1bPtmux;") {
		return p.picture("mermaid", source, drawn)
	}
	if strings.HasPrefix(strings.TrimSpace(ansi.Strip(drawn)), "```") {
		return drawn
	}
	lines := strings.Split(drawn, "\n")
	widest := 0
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = strings.TrimRight(ansi.Strip(line), " ")
		widest = max(widest, ansi.StringWidth(plain[i]))
	}
	p.mu.Lock()
	if p.drawingIndex == nil {
		p.drawingIndex = map[string]int{}
	}
	at, ok := p.drawingIndex[source]
	if !ok {
		at = len(p.drawings)
		p.drawingIndex[source] = at
		p.drawings = append(p.drawings, drawing{source: source})
	}
	d := &p.drawings[at]
	d.lines, d.plain, d.widest = lines, plain, widest
	code := d.code
	p.mu.Unlock()
	if code {
		text := p.Markdown("```mermaid\n" + strings.TrimSpace(source) + "\n```")
		var shown []string
		for _, line := range strings.Split(text, "\n") {
			shown = append(shown, strings.TrimRight(ansi.Strip(line), " "))
		}
		p.mu.Lock()
		p.drawings[at].shown = shown
		p.mu.Unlock()
		return text
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	d = &p.drawings[at]
	if widest <= p.width {
		d.left, d.shown = 0, plain
		return drawn
	}
	d.left = min(d.left, widest-p.width)
	shown := make([]string, len(lines))
	d.shown = nil
	for i, line := range lines {
		shown[i] = ansi.Cut(line, 0, answerIndent) + ansi.Cut(line, answerIndent+d.left, everyColumn)
		d.shown = append(d.shown, strings.TrimRight(ansi.Strip(shown[i]), " "))
	}
	hidden := fmt.Sprintf("%d columns out of view", widest-p.width)
	if d.left > 0 {
		hidden = fmt.Sprintf("◂ %d · %d ▸ columns out of view", d.left, widest-p.width-d.left)
	}
	hint := strings.Repeat(" ", answerIndent) + hidden + " · ctrl+←→ moves across · a click shows it whole"
	d.shown = append(d.shown, hint)
	return strings.Join(shown, "\n") + "\n" + dimStyle.Render(hint)
}

// picture keeps an image an answer drew from code, an SVG or a diagram, to
// show its code in its place: drawn, or as code when the user asked.
func (p *printer) picture(language, source, drawn string) string {
	codeText := trimBlankLines(p.Markdown("```" + language + "\n" + strings.TrimSpace(source) + "\n```"))
	shownText := trimBlankLines(kittyUploads.ReplaceAllString(drawn, ""))
	var shown []string
	for _, text := range []string{shownText, codeText} {
		for _, line := range strings.Split(text, "\n") {
			shown = append(shown, strings.TrimRight(ansi.Strip(line), " "))
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.drawingIndex == nil {
		p.drawingIndex = map[string]int{}
	}
	at, ok := p.drawingIndex[source]
	if !ok {
		at = len(p.drawings)
		p.drawingIndex[source] = at
		p.drawings = append(p.drawings, drawing{source: source})
	}
	d := &p.drawings[at]
	d.picture, d.codeText, d.shown = shownText, codeText, shown
	if d.code {
		return codeText
	}
	return drawn
}

// toggleAllCode shows every diagram as its code, or, when all already
// are, drawn again; it tells whether they show as code now.
func (p *printer) toggleAllCode() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	code := slices.ContainsFunc(p.drawings, func(d drawing) bool { return !d.code && d.picture == "" })
	p.drawings = setCode(p.drawings, code)
	return code
}

// setAllCode shows every diagram as its code, or drawn.
func (p *printer) setAllCode(code bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drawings = setCode(p.drawings, code)
}

// setCode sets how the diagrams drawn with characters show; images are
// swapped one at a time, from their menu.
func setCode(drawings []drawing, code bool) []drawing {
	for i := range drawings {
		if drawings[i].picture == "" {
			drawings[i].code = code
		}
	}
	return drawings
}

// toggleCode shows a diagram's code in place of its drawing, or back.
func (p *printer) toggleCode(source string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if at, ok := p.drawingIndex[source]; ok {
		p.drawings[at].code = !p.drawings[at].code
	}
}

// panDrawings moves every wide diagram across by columns, each within its
// own width, and tells whether any moved.
func (p *printer) panDrawings(by int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	moved := false
	for i := range p.drawings {
		d := &p.drawings[i]
		if d.code {
			continue
		}
		left := min(max(d.left+by, 0), max(d.widest-p.width, 0))
		moved = moved || left != d.left
		d.left = left
	}
	return moved
}

// hasDrawings tells whether a diagram is wider than the conversation.
func (p *printer) hasDrawings() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.ContainsFunc(p.drawings, func(d drawing) bool { return !d.code && d.widest > p.width })
}

// drawingWith is the diagram one of whose lines, as shown, or the line
// under it is line.
func (p *printer) drawingWith(line string) (drawing, bool) {
	line = strings.TrimRight(line, " ")
	if strings.TrimSpace(line) == "" {
		return drawing{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.drawings) - 1; i >= 0; i-- {
		if slices.Contains(p.drawings[i].shown, line) {
			return p.drawings[i], true
		}
	}
	return drawing{}, false
}

// caption is the line under an image an answer showed: its name, then
// where it is, dim, cut to the width.
func (p *printer) caption(name, path string, indent int) string {
	p.mu.Lock()
	width := p.width
	p.mu.Unlock()
	room := max(width-indent-2, 10)
	text := dimStyle.Render(path)
	if name != "" {
		text = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6")).Render(name) + dimStyle.Render(" · "+path)
	}
	return strings.Repeat(" ", indent) + ansi.Truncate(text, room, "…")
}

// File draws an image, SVG or Mermaid file, or says why it cannot.
func (p *printer) File(path string) string {
	return p.fileAt(path, answerIndent)
}

// fileAt draws a file indented as the text around it, in the width left.
func (p *printer) fileAt(path string, indent int) string {
	drawn, err := p.drawFile(path, indent)
	if err != nil {
		return p.Error(err)
	}
	return drawn
}

// drawFile draws a file indented as the text around it, a path relative
// to the working directory.
func (p *printer) drawFile(path string, indent int) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.workDir, path)
	}
	drawn, err := p.rendererAt(indent).File(path)
	if err != nil {
		return "", err
	}
	return indentLines(drawn, indent), nil
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
	return fitLines(strings.Trim(rendered, "\n"), p.width)
}

// linePrefix is what starts a rendered line and goes on the lines its text
// wraps to: indent and quote bars, kept, then a list marker, turned into
// as many spaces.
var linePrefix = regexp.MustCompile(`^(\s*(?:[│┃▌|]\s?)*\s*)((?:[-*•·+]|\d+[.)])\s+)?`)

// fitLines wraps any line wider than width again, its continuation lines
// starting as it does, so a quote's bar or a list's indent is never lost
// to the terminal breaking the line on its own.
func fitLines(text string, width int) string {
	if width <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		found := linePrefix.FindStringSubmatch(ansi.Strip(line))
		keep, marker := ansi.StringWidth(found[1]), ansi.StringWidth(found[2])
		prefix := keep + marker
		if prefix >= width-10 {
			out = append(out, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
			continue
		}
		rest := strings.TrimRight(ansi.TruncateLeft(line, prefix, ""), " ")
		wrapped := strings.Split(ansi.Wrap(rest, width-prefix, " "), "\n")
		bars := ansi.Cut(line, 0, keep)
		for i, piece := range wrapped {
			if i == 0 {
				out = append(out, ansi.Cut(line, 0, prefix)+piece)
				continue
			}
			out = append(out, bars+strings.Repeat(" ", marker)+piece)
		}
	}
	return strings.Join(out, "\n")
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
func (p *printer) ToolStart(name string, args json.RawMessage, summary string) string {
	header := "\n" + p.toolHeader(name, args)
	if !strings.Contains(summary, "\n") && len(summary) < 60 && name != "bash" {
		return header + " " + summary
	}
	return header + "\n" + p.command(summary)
}

// toolHeader is a tool's name after a mark coloured by what the call does.
func (p *printer) toolHeader(name string, args json.RawMessage) string {
	p.mu.Lock()
	kind := p.kind
	p.mu.Unlock()
	call := llm.ToolCall{Name: name, Args: args}
	mark := toolMark(toolKindOf(call, nil))
	if kind != nil {
		mark = toolMark(kind(call))
	}
	return mark + toolName.Render(" "+name)
}

func (p *printer) setKind(kind func(call llm.ToolCall) toolKind) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kind = kind
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
		parts = append(parts, "\n"+p.toolHeader(run.name, run.args)+" "+run.summary+dimStyle.Render("  "+change.summary())+"\n"+p.fileChangeLines(change))
	case open:
		parts = append(parts, "\n"+p.toolHeader(run.name, run.args)+"\n"+p.commandLines(run.summary, true))
		if run.name == "ui" {
			parts = append(parts, p.uiCall(run.args)...)
		}
	case changed:
		parts = append(parts, p.ToolStart(run.name, run.args, run.summary+dimStyle.Render("  "+change.summary())))
	default:
		parts = append(parts, p.ToolStart(run.name, run.args, run.summary))
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

// uiCall is what a ui call gave, to read when it is opened: the tree, as
// the tool mended it, drawn as a tree, and the script highlighted as Lua.
func (p *printer) uiCall(args json.RawMessage) []string {
	var call struct {
		Tree   json.RawMessage `json:"tree"`
		Script string          `json:"script"`
	}
	if json.Unmarshal(args, &call) != nil {
		return nil
	}
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	colors := changePalette(dark)
	inner := max(width-8, 20)
	block := lipgloss.NewStyle().Background(colors.block)
	pad := func(line string) string {
		fill := block.Render(strings.Repeat(" ", max(inner+2-ansi.StringWidth(line), 0)))
		return commandEdge.Render("  ▌") + block.Render(" ") + line + fill
	}
	var parts []string
	if len(call.Tree) > 0 && string(call.Tree) != "null" {
		var lines []string
		if tree, err := tools.ParseUITree(call.Tree); err == nil && tree != nil {
			lines = uiTreeLines(*tree, colors.block, dark, inner)
		} else {
			lines = highlightLines(string(call.Tree), "json", inner, colors.block, dark)
		}
		var out []string
		for _, line := range lines {
			out = append(out, pad(line))
		}
		parts = append(parts, dimStyle.Render("    tree")+"\n"+strings.Join(out, "\n"))
	}
	if script := strings.Trim(call.Script, "\n"); strings.TrimSpace(script) != "" {
		var out []string
		for _, line := range highlightLines(script, "lua", inner, colors.block, dark) {
			out = append(out, pad(line))
		}
		parts = append(parts, dimStyle.Render("    script")+"\n"+strings.Join(out, "\n"))
	}
	return parts
}

// uiTreeLines draws an interface's tree a node to a line under guides:
// its type, #id and words, then its other fields, each coloured as code
// is.
func uiTreeLines(root tools.UINode, background color.Color, dark bool, width int) []string {
	colors := syntaxPalette(dark)
	paint := func(c color.Color, bold bool) func(string) string {
		style := lipgloss.NewStyle().Background(background).Foreground(c).Bold(bold)
		return func(text string) string { return style.Render(text) }
	}
	guide, kind, id, words := paint(colors.comment, false), paint(colors.keyword, true), paint(colors.variable, false), paint(colors.str, false)
	name, equals := paint(colors.builtin, false), paint(colors.operator, false)
	value := func(raw string) string {
		switch {
		case strings.HasPrefix(raw, `"`):
			return paint(colors.str, false)(raw)
		case raw == "true" || raw == "false":
			return paint(colors.keyword, false)(raw)
		case strings.HasPrefix(raw, "["):
			return paint(colors.text, false)(raw)
		}
		return paint(colors.number, false)(raw)
	}
	var lines []string
	var walk func(n tools.UINode, lead, branch string)
	walk = func(n tools.UINode, lead, branch string) {
		pieces := []string{guide(lead+branch) + kind(n.Type)}
		if n.ID != "" {
			pieces = append(pieces, id("#"+n.ID))
		}
		for _, text := range []string{n.Title, n.Label, n.Text} {
			if text != "" {
				pieces = append(pieces, words(fmt.Sprintf("%q", text)))
			}
		}
		for _, field := range n.Fields() {
			pieces = append(pieces, name(field[0])+equals("=")+value(field[1]))
		}
		space := lipgloss.NewStyle().Background(background).Render(" ")
		line := strings.Join(pieces, space)
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width-1, "") + guide("…")
		}
		lines = append(lines, line)
		next := lead
		switch branch {
		case "├─ ":
			next += "│  "
		case "└─ ":
			next += "   "
		}
		for i, child := range n.Children {
			mark := "├─ "
			if i == len(n.Children)-1 {
				mark = "└─ "
			}
			walk(child, next, mark)
		}
	}
	walk(root, "", "")
	return lines
}

// toolHidesSome tells whether the folded view leaves lines out or cuts them.
func (p *printer) toolHidesSome(run toolRun) bool {
	if _, changed := fileChangeOf(run.name, run.args); changed {
		return true
	}
	if run.name == "ui" && len(p.uiCall(run.args)) > 0 {
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

// FoldedError is a long error: its first line and how long it is, or all
// of it when open.
func (p *printer) FoldedError(err error, open bool) string {
	text := err.Error()
	if open {
		return p.Error(err) + "\n" + dimStyle.Render("  ▴ ctrl+o or click to fold")
	}
	first, _, _ := strings.Cut(text, "\n")
	p.mu.Lock()
	width := p.width
	p.mu.Unlock()
	if runes := []rune(first); width > 0 && len(runes) > width/2 {
		first = string(runes[:max(width/2, 20)]) + "…"
	}
	lines := strings.Count(text, "\n") + 1
	return errorStyle.Render("✗ "+first) + dimStyle.Render(fmt.Sprintf(" · %d lines · ctrl+o or click to open", lines))
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

// noteMark starts every line mo itself writes in the conversation, so it
// reads apart from the model's.
const noteMark = "◇ "

// Note is a line of mo's own, marked; the lines after the first line up
// under its text.
func (p *printer) Note(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			lines[i] = noteMark + line
		} else {
			lines[i] = strings.Repeat(" ", len([]rune(noteMark))) + line
		}
	}
	return dimStyle.Render(strings.Join(lines, "\n"))
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
