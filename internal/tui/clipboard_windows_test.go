package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CW-01 | Happy    | A Windows path makes a file:// URI and comes back from it                 | P0       |
// | CW-02 | Happy    | Text copied reaches Windows' clipboard whole, accents and all             | P0       |
// | CW-03 | Happy    | A file copied is on the clipboard as a file, and pasting attaches it      | P0       |
// | CW-04 | Happy    | A picture copied is on the clipboard as a picture, and pasting saves it   | P1       |

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowsClipboardText(t *testing.T) string {
	t.Helper()
	output, err := clipboardScript(context.Background(), `$C::GetText()`, nil)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(output), "\r\n")
}

func TestWindowsClipboard(t *testing.T) {
	if !osCopies() {
		t.Skip("no Windows PowerShell")
	}

	t.Run("CW-01 a Windows path makes a file:// URI and comes back from it", func(t *testing.T) {
		uri := fileURI(`C:\Users\me\My Notes\a.txt`)

		if uri != "file:///C:/Users/me/My%20Notes/a.txt" || localPath("/C:/Users/me/My Notes/a.txt") != `C:\Users\me\My Notes\a.txt` {
			t.Errorf("uri %q", uri)
		}
	})

	t.Run("CW-02 text copied reaches Windows' clipboard whole, accents and all", func(t *testing.T) {
		err := copyWith(clipData{data: []byte("canción ñ 日本\nsecond line")})

		if got := windowsClipboardText(t); err != nil || got != "canción ñ 日本\r\nsecond line" && got != "canción ñ 日本\nsecond line" {
			t.Errorf("err %v got %q", err, got)
		}
	})

	t.Run("CW-03 a file copied is on the clipboard as a file, and pasting attaches it", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "notes con espacio.txt")
		os.WriteFile(path, []byte("hi"), 0o644)

		msg := copyFile(path)()
		board, _ := osClipboard()
		pasted, attached := readAttachment(context.Background(), board)

		if copied := msg.(fileCopiedMsg); copied.err != nil || !attached || len(pasted.files) != 1 || !strings.EqualFold(pasted.files[0], path) {
			t.Errorf("copy %+v pasted %+v", msg, pasted)
		}
	})

	t.Run("CW-04 a picture copied is on the clipboard as a picture, and pasting saves it", func(t *testing.T) {
		t.Setenv("LOCALAPPDATA", t.TempDir())
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		img.Set(1, 1, color.RGBA{255, 0, 0, 255})
		var data bytes.Buffer
		png.Encode(&data, img)
		path := filepath.Join(t.TempDir(), "dot.png")
		os.WriteFile(path, data.Bytes(), 0o644)

		msg := copyPicture(path)()
		board, _ := osClipboard()
		pasted, attached := readAttachment(context.Background(), board)

		saved, _ := os.ReadFile(pasted.image)
		if copied := msg.(fileCopiedMsg); copied.err != nil || !attached || !bytes.HasPrefix(saved, []byte("\x89PNG")) {
			t.Errorf("copy %+v pasted %+v", msg, pasted)
		}
	})
}
