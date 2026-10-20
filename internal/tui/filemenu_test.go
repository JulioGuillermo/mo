package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | FM-01 | Happy    | A right click on a path of a file that exists offers to copy it, its path, and to open it | P0 |
// | FM-02 | Happy    | A right click on an image an answer drew, or its caption, offers its file and copying the image | P0 |
// | FM-03 | Happy    | Copy the file puts its file:// URI on the clipboard as text/uri-list, as a file manager does | P0 |
// | FM-04 | Happy    | Copy the image puts PNG on the clipboard, a JPEG turned into PNG            | P1       |
// | FM-05 | Edge     | A word that names no file, or a folder, opens no file menu                  | P1       |
// | FM-06 | Edge     | Paths with :line:column, quotes, brackets or file:// are found; spaces are escaped in the URI | P1 |
// | FM-07 | Error    | Without a clipboard program the copy says what to install, over SSH which terminal copies | P2 |
// | FM-08 | Happy    | A program that fails gives way to the next: wl-copy, then xclip             | P0       |
// | FM-09 | Edge     | xsel copies text but is never used for a file or an image, which need a type | P1      |
// | FM-10 | Happy    | A terminal that reports kitty's clipboard mode gets the file through OSC 5522, URI and GNOME's list, no program run | P0 |
// | FM-11 | Edge     | OSC 5522 sends each type in base64 chunks of 4096 bytes and ends the write   | P1       |

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fakeCopier puts a wl-copy in PATH that keeps what it is given, its
// type and its bytes, in a file; it gives that file.
func fakeCopier(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	kept := filepath.Join(t.TempDir(), "clipboard")
	script := "#!/bin/sh\nprintf '%s\\n' \"$2\" > " + kept + "\ncat >> " + kept + "\n"
	if err := os.WriteFile(filepath.Join(bin, "wl-copy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	return kept
}

func writePicture(t *testing.T, path string, asJPEG bool) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		img.Set(x, x, color.RGBA{255, 0, 0, 255})
	}
	var data bytes.Buffer
	if asJPEG {
		jpeg.Encode(&data, img, nil)
	} else {
		png.Encode(&data, img)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func optionValues(m model) []string {
	if m.popup == nil {
		return nil
	}
	var values []string
	for _, option := range m.popup.options {
		values = append(values, option.Value)
	}
	return values
}

func choose(t *testing.T, m model, value string) (model, tea.Cmd) {
	t.Helper()
	for i, option := range m.popup.options {
		if option.Value == value {
			chosen, cmd := m.choosePopup(i)
			return chosen.(model), cmd
		}
	}
	t.Fatalf("no option %q in %v", value, optionValues(m))
	return m, nil
}

func fileModel(t *testing.T) (model, string) {
	t.Helper()
	m := sidebarModel(t, 120)
	dir := t.TempDir()
	m.session.WorkDir = dir
	m.printer.workDir = dir
	return m, dir
}

func TestFileMenu(t *testing.T) {
	t.Run("FM-01 a right click on a path of a file that exists offers to copy it, its path, and to open it", func(t *testing.T) {
		m, dir := fileModel(t)
		os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644)
		updated, _ := m.emit(m.printer.Note("wrote notes.txt for you"))
		m = updated.(model)

		m = rightClickOn(t, m, "notes.txt")

		values := strings.Join(optionValues(m), ",")
		if values != strings.Join([]string{fileCopy, fileCopyPath, fileView, fileOpen}, ",") || !strings.Contains(m.popup.description, filepath.Join(dir, "notes.txt")) {
			t.Errorf("options %s, description %q", values, m.popup.description)
		}
	})

	t.Run("FM-02 a right click on an image an answer drew, or its caption, offers its file and copying the image", func(t *testing.T) {
		m, dir := fileModel(t)
		writePicture(t, filepath.Join(dir, "shot.png"), false)
		updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer("![the shot](shot.png)") }))
		m = updated.(model)

		onCaption := rightClickOn(t, m, "the shot")
		at := cellOf(t, m, "the shot")
		onPicture, _ := m.Update(tea.MouseClickMsg{X: at.x + 2, Y: at.y - 1, Button: tea.MouseRight})

		for name, menu := range map[string]model{"caption": onCaption, "picture": onPicture.(model)} {
			values := strings.Join(optionValues(menu), ",")
			if !strings.Contains(values, fileCopy) || !strings.Contains(values, fileCopyImage) {
				t.Errorf("%s: options %s", name, values)
			}
		}
	})

	t.Run("FM-03 copy the file puts its file:// URI on the clipboard as text/uri-list, as a file manager does", func(t *testing.T) {
		kept := fakeCopier(t)
		m, dir := fileModel(t)
		os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644)
		updated, _ := m.emit(m.printer.Note("wrote notes.txt"))
		m = rightClickOn(t, updated.(model), "notes.txt")

		m, cmd := choose(t, m, fileCopy)
		m = deliver(m, cmd, 1)

		got, _ := os.ReadFile(kept)
		want := "text/uri-list\nfile://" + filepath.Join(dir, "notes.txt") + "\r\n"
		if string(got) != want || m.flash != "copied the file" {
			t.Errorf("clipboard %q, want %q; flash %q", got, want, m.flash)
		}
	})

	t.Run("FM-04 copy the image puts PNG on the clipboard, a JPEG turned into PNG", func(t *testing.T) {
		kept := fakeCopier(t)
		dir := t.TempDir()
		path := filepath.Join(dir, "photo.jpg")
		writePicture(t, path, true)

		msg := copyPicture(path)().(fileCopiedMsg)

		got, _ := os.ReadFile(kept)
		kind, data, _ := bytes.Cut(got, []byte("\n"))
		if msg.err != nil || string(kind) != "image/png" || !bytes.HasPrefix(data, []byte("\x89PNG")) {
			t.Errorf("err %v kind %q data %q", msg.err, kind, data[:min(len(data), 8)])
		}
	})

	t.Run("FM-05 a word that names no file, or a folder, opens no file menu", func(t *testing.T) {
		m, dir := fileModel(t)
		os.Mkdir(filepath.Join(dir, "folder"), 0o755)
		updated, _ := m.emit(m.printer.Note("nothing.txt is gone and folder is a folder"))
		m = updated.(model)

		missing := rightClickOn(t, m, "nothing.txt")
		folder := rightClickOn(t, m, "folder is")

		if missing.popup != nil || folder.popup != nil {
			t.Errorf("menus: missing %v folder %v", optionValues(missing), optionValues(folder))
		}
	})

	t.Run("FM-06 paths with :line:column, quotes, brackets or file:// are found; spaces are escaped in the URI", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
		printer := newPrinter()
		printer.workDir = dir
		for _, line := range []string{"see main.go:12:4 now", "see `main.go` now", "see (main.go), now", "see file://" + filepath.Join(dir, "main.go") + " now"} {
			at := strings.Index(line, "main.go") + 2
			if path, ok := printer.pathAt(line, at); !ok || path != filepath.Join(dir, "main.go") {
				t.Errorf("%q: %q %v", line, path, ok)
			}
		}
		if uri := fileURI("/tmp/a b/ñ.png"); uri != "file:///tmp/a%20b/%C3%B1.png" {
			t.Errorf("uri %q", uri)
		}
	})

	t.Run("FM-07 without a clipboard program the copy says what to install, over SSH which terminal copies", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("macOS copies with osascript")
		}
		t.Setenv("PATH", t.TempDir())
		t.Setenv("WAYLAND_DISPLAY", "wayland-test")
		t.Setenv("DISPLAY", "")
		t.Setenv("SSH_CONNECTION", "")

		local := copyFile("/tmp/x")().(fileCopiedMsg)
		t.Setenv("SSH_CONNECTION", "10.0.0.1 1 10.0.0.2 22")
		remote := copyFile("/tmp/x")().(fileCopiedMsg)
		m, _ := fileModel(t)
		updated, _ := m.fileCopied(local)

		if local.err == nil || !strings.Contains(shown(updated.(model)), "copy the file: install wl-clipboard (wl-copy), or xclip") {
			t.Errorf("err %v shown:\n%s", local.err, shown(updated.(model)))
		}
		if remote.err == nil || !strings.Contains(remote.err.Error(), "over SSH only a terminal with kitty's clipboard protocol") {
			t.Errorf("remote err %v", remote.err)
		}
	})
}

// fakePrograms puts in an empty PATH the programs named, each keeping its
// arguments and input in a file of its name, or failing when told.
func fakePrograms(t *testing.T, failing []string, names ...string) string {
	t.Helper()
	bin, kept := t.TempDir(), t.TempDir()
	for _, name := range names {
		script := "#!/bin/sh\nPATH=/usr/bin:/bin\nprintf '%s\\n' \"$*\" > " + filepath.Join(kept, name) + "\ncat >> " + filepath.Join(kept, name) + "\n"
		if slices.Contains(failing, name) {
			script = "#!/bin/sh\nexit 1\n"
		}
		os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755)
	}
	t.Setenv("PATH", bin)
	return kept
}

func TestCopyWays(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS copies with pbcopy and osascript")
	}

	t.Run("FM-08 a program that fails gives way to the next: wl-copy, then xclip", func(t *testing.T) {
		kept := fakePrograms(t, []string{"wl-copy"}, "wl-copy", "xclip")
		t.Setenv("WAYLAND_DISPLAY", "wayland-test")
		t.Setenv("DISPLAY", ":0")

		msg := copyFile("/tmp/a.txt")().(fileCopiedMsg)

		got, _ := os.ReadFile(filepath.Join(kept, "xclip"))
		if msg.err != nil || string(got) != "-selection clipboard -t text/uri-list\nfile:///tmp/a.txt\r\n" {
			t.Errorf("err %v xclip got %q", msg.err, got)
		}
	})

	t.Run("FM-09 xsel copies text but is never used for a file or an image, which need a type", func(t *testing.T) {
		kept := fakePrograms(t, nil, "xsel")
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", ":0")

		textErr := copyWith(clipData{data: []byte("hello")})
		fileMsg := copyFile("/tmp/a.txt")().(fileCopiedMsg)

		got, _ := os.ReadFile(filepath.Join(kept, "xsel"))
		if textErr != nil || string(got) != "--clipboard --input\nhello" {
			t.Errorf("text err %v xsel got %q", textErr, got)
		}
		if fileMsg.err == nil || !strings.Contains(fileMsg.err.Error(), "install xclip") {
			t.Errorf("file err %v", fileMsg.err)
		}
	})

	t.Run("FM-10 a terminal that reports kitty's clipboard mode gets the file through OSC 5522, URI and GNOME's list, no program run", func(t *testing.T) {
		kept := fakePrograms(t, nil, "wl-copy")
		t.Setenv("WAYLAND_DISPLAY", "wayland-test")
		m, dir := fileModel(t)
		os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644)
		updated, _ := m.Update(tea.ModeReportMsg{Mode: kittyClipboardMode, Value: ansi.ModeReset})
		m = updated.(model)

		_, _, cmd := m.fileChoice(filepath.Join(dir, "notes.txt"), fileCopy)
		var sent string
		for _, each := range cmd().(tea.BatchMsg) {
			if raw, ok := each().(tea.RawMsg); ok {
				sent = fmt.Sprint(raw.Msg)
			}
		}

		uri := base64.StdEncoding.EncodeToString([]byte("file://" + filepath.Join(dir, "notes.txt") + "\r\n"))
		gnome := base64.StdEncoding.EncodeToString([]byte("x-special/gnome-copied-files"))
		if !m.terminalCopies || !strings.Contains(sent, uri) || !strings.Contains(sent, "mime="+gnome) {
			t.Errorf("terminal copies %v sent %q", m.terminalCopies, sent)
		}
		if _, err := os.Stat(filepath.Join(kept, "wl-copy")); err == nil {
			t.Error("wl-copy ran too")
		}
	})

	t.Run("FM-11 OSC 5522 sends each type in base64 chunks of 4096 bytes and ends the write", func(t *testing.T) {
		big := bytes.Repeat([]byte("x"), osc5522Chunk+10)

		sequence := osc5522([]clipData{{mime: "image/png", data: big}, {mime: "text/plain", data: []byte("hi")}})

		packets := strings.Split(strings.TrimSuffix(sequence, "\x1b\\"), "\x1b\\")
		png := base64.StdEncoding.EncodeToString([]byte("image/png"))
		if len(packets) != 5 || packets[0] != "\x1b]5522;type=write" || packets[4] != "\x1b]5522;type=wdata" {
			t.Fatalf("packets %d: first %q last %q", len(packets), packets[0], packets[len(packets)-1])
		}
		if !strings.HasPrefix(packets[1], "\x1b]5522;type=wdata:mime="+png+";") || !strings.HasSuffix(packets[2], base64.StdEncoding.EncodeToString(big[osc5522Chunk:])) {
			t.Errorf("chunks %q / %q", packets[1][:40], packets[2])
		}
	})
}
