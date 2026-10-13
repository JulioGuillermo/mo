package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | CB-01 | Happy    | A picture on the clipboard is saved and shown as a label             | P0       |
// | CB-02 | Happy    | Files copied in a file manager win over their preview picture        | P1       |
// | CB-03 | Edge     | A clipboard with only text is left to the normal paste               | P1       |
// | CB-04 | Happy    | The labels are sent as the paths they stand for, quoted when needed  | P0       |
// | CB-05 | Happy    | A pasted path to a file becomes a label; other text is typed as is    | P1       |
// | TB-01 | Happy    | Tables are drawn apart, code blocks with bars are left to markdown    | P0       |
// | TB-02 | Edge     | Bars inside code and escaped bars stay in their cell                 | P1       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func fakeClipboard(content map[string]string) clipboard {
	return clipboard{
		types: func(context.Context) ([]string, error) {
			var types []string
			for mime := range content {
				types = append(types, mime)
			}
			return types, nil
		},
		read: func(_ context.Context, mime string) ([]byte, error) { return []byte(content[mime]), nil },
	}
}

func TestClipboard(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	t.Run("CB-01 a picture on the clipboard is saved and shown as a label", func(t *testing.T) {
		msg, attached := readAttachment(context.Background(), fakeClipboard(map[string]string{"image/png": "PNGDATA", "text/plain": "x"}))
		m := conversationModel()

		updated, _ := m.Update(msg)

		saved, _ := os.ReadFile(msg.image)
		after := updated.(model)
		if !attached || string(saved) != "PNGDATA" || filepath.Ext(msg.image) != ".png" || after.input.Value() != "[image #1] " || after.attachments["[image #1]"] != msg.image {
			t.Errorf("msg = %+v input = %q", msg, after.input.Value())
		}
	})

	t.Run("CB-02 files copied in a file manager win over their preview picture", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "my report.pdf")
		_ = os.WriteFile(file, []byte("%PDF"), 0o600)
		uri := "file://" + strings.ReplaceAll(file, " ", "%20")

		msg, _ := readAttachment(context.Background(), fakeClipboard(map[string]string{"text/uri-list": uri + "\r\n", "image/png": "thumb"}))

		if msg.image != "" || len(msg.files) != 1 || msg.files[0] != file {
			t.Errorf("msg = %+v", msg)
		}
	})

	t.Run("CB-03 a clipboard with only text is left to the normal paste", func(t *testing.T) {
		_, attached := readAttachment(context.Background(), fakeClipboard(map[string]string{"text/plain": "hello"}))

		if attached {
			t.Error("text was taken as an attachment")
		}
	})

	t.Run("CB-04 the labels are sent as the paths they stand for, quoted when needed", func(t *testing.T) {
		m := conversationModel()
		m.attach([]string{"/tmp/a b.png", "/tmp/notes.txt"}, false)

		sent := m.withAttachments("look at [image a b.png] and [file notes.txt]")

		if sent != `look at "/tmp/a b.png" and /tmp/notes.txt` || m.attachments != nil {
			t.Errorf("sent = %q", sent)
		}
	})

	t.Run("CB-05 a pasted path to a file becomes a label; other text is typed as is", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "shot.png")
		_ = os.WriteFile(file, []byte("x"), 0o600)
		inDir := func() model {
			m := conversationModel()
			m.session.WorkDir = dir
			return m
		}

		dropped, _ := inDir().Update(tea.PasteMsg{Content: "'" + file + "'"})
		typed, _ := inDir().Update(tea.PasteMsg{Content: "see " + file})

		if dropped.(model).input.Value() != "[image shot.png] " || typed.(model).input.Value() != "see "+file {
			t.Errorf("dropped = %q typed = %q", dropped.(model).input.Value(), typed.(model).input.Value())
		}
	})
}

func TestTables(t *testing.T) {
	t.Run("TB-01 tables are drawn apart, code blocks with bars are left to markdown", func(t *testing.T) {
		parts := splitTables("intro\n\n| a | b |\n|---|:-:|\n| 1 | 2 |\n\n```\n| x | y |\n|---|---|\n```")

		if len(parts) != 3 || !parts[1].isTable || parts[0].isTable || parts[2].isTable || !strings.Contains(parts[2].text, "| x | y |") {
			t.Errorf("parts = %+v", parts)
		}
		drawn := ansi.Strip(newPrinter().Table(parts[1].text))
		if !strings.Contains(drawn, "a") || strings.Contains(drawn, "|") || strings.Contains(drawn, "---") {
			t.Errorf("drawn:\n%s", drawn)
		}
	})

	t.Run("TB-02 bars inside code and escaped bars stay in their cell", func(t *testing.T) {
		cells := tableCells("| `a|b` | c \\| d | e |")

		if len(cells) != 3 || cells[0] != "`a|b`" || cells[1] != "c | d" {
			t.Errorf("cells = %q", cells)
		}
	})
}
