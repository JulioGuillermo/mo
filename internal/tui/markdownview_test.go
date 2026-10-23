package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | MV-01 | Happy    | m on a markdown file shows it rendered, its picture drawn and named, m again the text | P0 |
// | MV-02 | Happy    | i shows the pictures named only, and drawn again                         | P0       |
// | MV-03 | Happy    | m on a diff of /git show renders the whole file as it is after           | P0       |
// | MV-04 | Edge     | A file that is not markdown is not rendered; m there does nothing        | P1       |
// | MV-05 | Edge     | Rendered, the cursor and comments are off; y copies the source; esc goes back to the text | P1 |

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/render"
)

const sampleMarkdown = "# Title\n\nSome **bold** text.\n\n![the sun](img/sun.png)\n\n- one\n- two\n"

func markdownModel(t *testing.T) (model, string) {
	t.Helper()
	m, dir := gitModel(t)
	m.width, m.height = 100, 40
	m.printer.setImages(render.ModeBlocks)
	writeFile(t, filepath.Join(dir, "docs/guide.md"), sampleMarkdown)
	picture := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			picture.Set(x, y, color.RGBA{250, 200, 40, 255})
		}
	}
	os.MkdirAll(filepath.Join(dir, "docs/img"), 0o755)
	file, _ := os.Create(filepath.Join(dir, "docs/img/sun.png"))
	png.Encode(file, picture)
	file.Close()
	return m, dir
}

func TestMarkdownView(t *testing.T) {
	t.Run("MV-01 m on a markdown file shows it rendered, its picture drawn and named, m again the text", func(t *testing.T) {
		m, _ := markdownModel(t)
		opened, _ := m.openFile("docs/guide.md")
		m = opened.(model)

		rendered := pressedKeys(m, "m")
		shown := viewerScreen(rendered)
		back := pressedKeys(rendered, "m")

		if strings.Contains(shown, "# Title") || strings.Contains(shown, "**bold**") || !strings.Contains(shown, "Title") || !strings.Contains(shown, "• one") {
			t.Errorf("not rendered:\n%s", shown)
		}
		if !strings.Contains(shown, "▀") || !strings.Contains(shown, "the sun · img/sun.png") || !strings.Contains(shown, "markdown, rendered") {
			t.Errorf("no picture:\n%s", shown)
		}
		if !strings.Contains(viewerScreen(back), "# Title") || back.viewer.markdown != nil {
			t.Errorf("m did not go back:\n%s", viewerScreen(back))
		}
	})

	t.Run("MV-02 i shows the pictures named only, and drawn again", func(t *testing.T) {
		m, _ := markdownModel(t)
		opened, _ := m.openFile("docs/guide.md")
		m = pressedKeys(opened.(model), "m", "i")

		off := viewerScreen(m)
		on := viewerScreen(pressedKeys(m, "i"))

		if strings.Contains(off, "▀") || !strings.Contains(off, "[image 40x20]") || !strings.Contains(off, "images off") {
			t.Errorf("images off:\n%s", off)
		}
		if !strings.Contains(on, "▀") {
			t.Errorf("images on again:\n%s", on)
		}
	})

	t.Run("MV-03 m on a diff of /git show renders the whole file as it is after", func(t *testing.T) {
		m, dir := markdownModel(t)
		writeFile(t, filepath.Join(dir, "src/edit.go"), "package src\n")
		m = runGit(t, m, "show")
		m.openChange(m.viewer.changes, filepath.Join(dir, "docs/guide.md"))

		shown := viewerScreen(pressedKeys(m, "m"))

		if !strings.Contains(shown, "Some bold text.") || !strings.Contains(shown, "• two") || strings.Contains(shown, "+ ") {
			t.Errorf("not the whole file rendered:\n%s", shown)
		}
	})

	t.Run("MV-04 a file that is not markdown is not rendered; m there does nothing", func(t *testing.T) {
		m, _ := markdownModel(t)
		opened, _ := m.openFile("src/edit.go")
		m = pressedKeys(opened.(model), "m")

		if m.viewer.markdown != nil || strings.Contains(viewerScreen(m), "m rendered") {
			t.Error("a Go file was rendered as markdown")
		}
	})

	t.Run("MV-05 rendered, the cursor and comments are off; y copies the source; esc goes back to the text", func(t *testing.T) {
		m, _ := markdownModel(t)
		opened, _ := m.openFile("docs/guide.md")
		m = pressedKeys(opened.(model), "m", "c")
		commenting, annotatable := m.viewer.commenting, m.viewer.annotatable()
		updated, cmd := m.viewerKey("y")
		copied := sent(cmd)
		back := pressedKeys(updated.(model), "esc")

		if commenting || annotatable {
			t.Errorf("rendered markdown takes comments: commenting %v annotatable %v", commenting, annotatable)
		}
		if !strings.Contains(copied, "# Title") || back.viewer == nil || back.viewer.markdown != nil {
			t.Errorf("copied %q, esc left %+v", copied, back.viewer)
		}
	})
}
