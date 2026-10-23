package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const markdownHelp = "↑↓ scroll · m source · i images · / search · n N next · y copy the source · esc source"

// markdownView is a markdown file shown rendered in the viewer, as the
// conversation draws an answer; source is what it was drawn from, and
// shown what the viewer showed before, to go back to.
type markdownView struct {
	source string
	images bool
	shown  outputViewer
}

// isMarkdown tells a file whose name says it is markdown.
func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx", ".mdown", ".mkd":
		return true
	}
	return false
}

// canRender tells whether the viewer shows a markdown file it can draw.
func (v *outputViewer) canRender() bool {
	switch {
	case v.markdown != nil:
		return true
	case v.file != "":
		return isMarkdown(v.file)
	case v.changes != nil:
		return isMarkdown(v.diffFile)
	}
	return false
}

// markdownSource is the whole text of the markdown the viewer shows: the
// file, or of a diff the file as it is after, or before when it is gone.
func (m model) markdownSource() string {
	v := m.viewer
	if v.changes != nil {
		before, after := v.changes.comparison.Contents(filepath.ToSlash(v.diffFile))
		if after == "" {
			return before
		}
		return after
	}
	if data, err := os.ReadFile(m.viewedPath()); err == nil {
		return string(data)
	}
	return strings.Join(v.lines, "\n")
}

// toggleMarkdown shows the markdown rendered in place of its text or diff,
// or the text or diff again.
func (m model) toggleMarkdown() (tea.Model, tea.Cmd) {
	v := m.viewer
	if v.markdown != nil {
		shown := v.markdown.shown
		*v = shown
		return m, nil
	}
	if !v.canRender() {
		return m, nil
	}
	v.markdown = &markdownView{source: m.markdownSource(), images: true, shown: *v}
	return m, m.drawMarkdown()
}

// drawMarkdown draws the markdown at the viewer's width, its pictures as
// kitty images or blocks when they are shown, and sends their uploads.
func (m model) drawMarkdown() tea.Cmd {
	v := m.viewer
	drawer := m.printer.forFile(filepath.Dir(m.viewedPath()), m.viewerWidth(), v.markdown.images)
	drawn := strings.TrimLeft(drawer.Answer(v.markdown.source), "\n")
	uploads := strings.Join(kittyUploads.FindAllString(drawn, -1), "")
	drawn = kittyUploads.ReplaceAllString(drawn, "")
	v.colored = strings.Split(drawn, "\n")
	v.lines = make([]string, len(v.colored))
	for i, line := range v.colored {
		v.lines[i] = ansi.Strip(line)
	}
	v.code, v.diff = nil, nil
	v.top, v.left, v.cursor, v.selected = 0, 0, 0, false
	v.findMatches()
	return sendRaw(uploads)
}

// markdownKey is a key of the viewer showing markdown rendered, before the
// usual ones; it tells whether it took it.
func (m model) markdownKey(pressed string) (bool, tea.Model, tea.Cmd) {
	v := m.viewer
	switch pressed {
	case "m":
		next, cmd := m.toggleMarkdown()
		return true, next, cmd
	case "esc":
		if v.query != "" {
			return false, m, nil
		}
		next, cmd := m.toggleMarkdown()
		return true, next, cmd
	case "i":
		v.markdown.images = !v.markdown.images
		top := v.top
		cmd := m.drawMarkdown()
		v.top = min(top, max(len(v.lines)-1, 0))
		return true, m, cmd
	case "y":
		return true, m, m.copyViewer(v.markdown.source, "copied the markdown")
	case "B", "H", "c", "d", "a", "e", "E", "f":
		v.notice = "m goes back to the text, where " + pressed + " works"
		return true, m, nil
	}
	return false, m, nil
}

// markdownHeader is what the viewer says over markdown rendered.
func (v *outputViewer) markdownHeader(width int) string {
	name := v.file
	if name == "" {
		name = v.diffFile
	}
	images := "images on"
	if !v.markdown.images {
		images = "images off"
	}
	return toolName().Render(oneLine(name, width/2)) + " " + dimStyle.Render("markdown, rendered · "+images+" · only you see it")
}

// forFile is a printer as this one, to draw a file of dir at width, its
// pictures named instead of drawn when images is false.
func (p *printer) forFile(dir string, width int, images bool) *printer {
	p.mu.Lock()
	defer p.mu.Unlock()
	mode := p.images
	if !images {
		mode = render.ModeOff
	}
	return &printer{width: width, dark: p.dark, images: mode, workDir: dir, kind: p.kind}
}
