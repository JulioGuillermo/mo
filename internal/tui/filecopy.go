package tui

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/render"
)

// fileCopiedMsg says whether a file or its picture reached the clipboard.
type fileCopiedMsg struct {
	what string
	err  error
}

// fileURI is a path as a file manager copies it: file:// and the path,
// its unsafe characters escaped.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// fileClips are what a file manager puts on the clipboard for a copied
// file: its URI, and GNOME's own list Nautilus pastes from.
func fileClips(path string) []clipData {
	uri := fileURI(path)
	return []clipData{
		{mime: "text/uri-list", data: []byte(uri + "\r\n")},
		{mime: "x-special/gnome-copied-files", data: []byte("copy\n" + uri)},
	}
}

// copyFile puts a file on the clipboard as a file manager's copy does, to
// paste it in one, in a browser's upload or in a chat: with osascript on
// macOS, else the first program that takes its URI.
func copyFile(path string) tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS == "darwin" {
			ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
			defer cancel()
			script := fmt.Sprintf("set the clipboard to (POSIX file %q)", path)
			return fileCopiedMsg{what: "file", err: exec.CommandContext(ctx, "osascript", "-e", script).Run()}
		}
		return fileCopiedMsg{what: "file", err: copyWith(fileClips(path)[0])}
	}
}

// copyPicture puts a picture on the clipboard as PNG, to paste it inside a
// document or a chat; one of another kind is turned into PNG first.
func copyPicture(path string) tea.Cmd {
	return func() tea.Msg {
		data, err := pngOf(path)
		if err != nil {
			return fileCopiedMsg{what: "image", err: err}
		}
		if runtime.GOOS == "darwin" {
			return fileCopiedMsg{what: "image", err: copyPictureOnMac(data)}
		}
		return fileCopiedMsg{what: "image", err: copyWith(clipData{mime: "image/png", data: data})}
	}
}

func copyPictureOnMac(data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	saved, err := os.CreateTemp("", "mo-copy-*.png")
	if err != nil {
		return err
	}
	defer os.Remove(saved.Name())
	saved.Write(data)
	saved.Close()
	script := fmt.Sprintf("set the clipboard to (read (POSIX file %q) as «class PNGf»)", saved.Name())
	return exec.CommandContext(ctx, "osascript", "-e", script).Run()
}

// pngOf is a picture file as PNG: as it is when it is one, else decoded and
// encoded again; an SVG is drawn first.
func pngOf(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		return data, nil
	}
	img, err := render.DecodeFile(path, data)
	if err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// openOutside opens a file with the app the system has for it.
func openOutside(path string) tea.Cmd {
	return func() tea.Msg {
		name := "xdg-open"
		if runtime.GOOS == "darwin" {
			name = "open"
		}
		command := exec.Command(name, path)
		command.Stdout, command.Stderr = io.Discard, io.Discard
		if err := command.Start(); err != nil {
			return fileCopiedMsg{what: "open", err: err}
		}
		go command.Wait()
		return nil
	}
}
