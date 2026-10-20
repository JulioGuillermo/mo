package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/render"
)

const (
	chipThumbnailCols = 14
	chipThumbnailRows = 3
	chatThumbnailCols = 24
	chatThumbnailRows = 6
)

type thumbnailMsg struct {
	path   string
	upload string
	lines  []string
}

func (p *printer) showsKitty() bool {
	return p != nil && p.renderer().Mode == render.ModeKitty
}

func isPicture(path string) bool {
	return imageExtensions[strings.ToLower(filepath.Ext(path))]
}

// drawThumbnails draws in the background the thumbnails of the images in
// the input that have none yet; only a kitty terminal shows them.
func (m *model) drawThumbnails() tea.Cmd {
	if !m.printer.showsKitty() {
		return nil
	}
	var draws []tea.Cmd
	for _, path := range m.attachments {
		if _, asked := m.thumbnails[path]; asked || !isPicture(path) {
			continue
		}
		if m.thumbnails == nil {
			m.thumbnails = map[string][]string{}
		}
		m.thumbnails[path] = nil
		draws = append(draws, func() tea.Msg {
			upload, lines, err := render.Thumbnail(path, chipThumbnailCols, chipThumbnailRows)
			if err != nil {
				return nil
			}
			return thumbnailMsg{path: path, upload: upload, lines: lines}
		})
	}
	return tea.Batch(draws...)
}

func (m model) thumbnailDrawn(msg thumbnailMsg) (tea.Model, tea.Cmd) {
	m.thumbnails[msg.path] = msg.lines
	return m, sendRaw(msg.upload)
}

var bareOrQuoted = regexp.MustCompile(`"((?:[^"\\]|\\.)+)"|\S+`)

// promptImages are the image files a sent prompt names by their full path,
// bare or quoted, as attachments are sent.
func promptImages(sent string) []string {
	var images []string
	for _, match := range bareOrQuoted.FindAllStringSubmatch(sent, -1) {
		path := match[0]
		if match[1] != "" {
			path = strings.ReplaceAll(match[1], `\"`, `"`)
		}
		if !filepath.IsAbs(path) || !isPicture(path) {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			images = append(images, path)
		}
	}
	return images
}

// Thumbnails draws the images of a prompt side by side under it, with
// their kitty uploads; nothing on a terminal without kitty images.
func (p *printer) Thumbnails(images []string) string {
	if len(images) == 0 || !p.showsKitty() {
		return ""
	}
	var uploads strings.Builder
	columns := []string{"  "}
	for _, path := range images {
		upload, lines, err := render.Thumbnail(path, chatThumbnailCols, chatThumbnailRows)
		if err != nil {
			continue
		}
		uploads.WriteString(upload)
		columns = append(columns, strings.Join(lines, "\n"), " ")
	}
	if len(columns) == 1 {
		return ""
	}
	return "\n" + uploads.String() + lipgloss.JoinHorizontal(lipgloss.Top, columns...)
}
