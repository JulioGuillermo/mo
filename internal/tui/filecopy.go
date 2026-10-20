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
	"strings"

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

// clipboardWriter is the command that puts data of a type on the
// clipboard, as wl-copy or xclip does; none where there is neither.
func clipboardWriter(mime string) (string, []string, bool) {
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && found("wl-copy"):
		return "wl-copy", []string{"--type", mime}, true
	case os.Getenv("DISPLAY") != "" && found("xclip"):
		return "xclip", []string{"-selection", "clipboard", "-t", mime}, true
	}
	return "", nil, false
}

// copyFile puts a file on the clipboard as a file manager's copy does, to
// paste it in one, in a browser's upload or in a chat.
func copyFile(path string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
		defer cancel()
		if runtime.GOOS == "darwin" {
			script := fmt.Sprintf("set the clipboard to (POSIX file %q)", path)
			return fileCopiedMsg{what: "file", err: exec.CommandContext(ctx, "osascript", "-e", script).Run()}
		}
		name, args, ok := clipboardWriter("text/uri-list")
		if !ok {
			return fileCopiedMsg{what: "file", err: fmt.Errorf("no wl-copy or xclip to copy files with")}
		}
		return fileCopiedMsg{what: "file", err: runWith(ctx, strings.NewReader(fileURI(path)+"\r\n"), name, args...)}
	}
}

// copyPicture puts a picture on the clipboard as PNG, to paste it inside a
// document or a chat; one of another kind is turned into PNG first.
func copyPicture(path string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
		defer cancel()
		data, err := pngOf(path)
		if err != nil {
			return fileCopiedMsg{what: "image", err: err}
		}
		if runtime.GOOS == "darwin" {
			saved, err := os.CreateTemp("", "mo-copy-*.png")
			if err != nil {
				return fileCopiedMsg{what: "image", err: err}
			}
			defer os.Remove(saved.Name())
			saved.Write(data)
			saved.Close()
			script := fmt.Sprintf("set the clipboard to (read (POSIX file %q) as «class PNGf»)", saved.Name())
			return fileCopiedMsg{what: "image", err: exec.CommandContext(ctx, "osascript", "-e", script).Run()}
		}
		name, args, ok := clipboardWriter("image/png")
		if !ok {
			return fileCopiedMsg{what: "image", err: fmt.Errorf("no wl-copy or xclip to copy images with")}
		}
		return fileCopiedMsg{what: "image", err: runWith(ctx, bytes.NewReader(data), name, args...)}
	}
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

func runWith(ctx context.Context, input io.Reader, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = input
	return command.Run()
}
