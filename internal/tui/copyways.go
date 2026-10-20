package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// kittyClipboardMode is the mode of kitty's clipboard protocol, OSC 5522,
// which copies any type of data through the terminal itself; a terminal
// that knows it says so when asked.
const kittyClipboardMode = ansi.DECMode(5522)

const osc5522Chunk = 4096

// clipData is data of a type to put on the clipboard.
type clipData struct {
	mime string
	data []byte
}

// copyProgram is a program that puts data of a type on the clipboard.
type copyProgram struct {
	name string
	args func(mime string) []string
	// typed is whether it takes a type; one that does not copies text only.
	typed bool
}

// copyPrograms are the programs this desktop can copy with, in the order
// they are tried: Wayland's, then X11's, which XWayland answers too.
func copyPrograms() []copyProgram {
	var programs []copyProgram
	if runtime.GOOS == "darwin" {
		return []copyProgram{{name: "pbcopy", args: func(string) []string { return nil }}}
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" && found("wl-copy") {
		programs = append(programs, copyProgram{name: "wl-copy", typed: true, args: func(mime string) []string {
			if mime == "" {
				return nil
			}
			return []string{"--type", mime}
		}})
	}
	if os.Getenv("DISPLAY") != "" {
		if found("xclip") {
			programs = append(programs, copyProgram{name: "xclip", typed: true, args: func(mime string) []string {
				if mime == "" {
					return []string{"-selection", "clipboard"}
				}
				return []string{"-selection", "clipboard", "-t", mime}
			}})
		}
		if found("xsel") {
			programs = append(programs, copyProgram{name: "xsel", args: func(string) []string { return []string{"--clipboard", "--input"} }})
		}
	}
	return programs
}

// copyWith tries each program that takes the type in turn, until one puts
// the data on the clipboard; it gives why none could.
func copyWith(item clipData) error {
	var failures []string
	for _, program := range copyPrograms() {
		if item.mime != "" && !program.typed {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
		err := runWith(ctx, strings.NewReader(string(item.data)), program.name, program.args(item.mime)...)
		cancel()
		if err == nil {
			return nil
		}
		failures = append(failures, program.name+": "+err.Error())
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return errors.New(missingCopier())
}

// missingCopier says what to install, or which terminal copies by itself,
// when nothing here can copy typed data.
func missingCopier() string {
	switch {
	case os.Getenv("SSH_CONNECTION") != "":
		return "over SSH only a terminal with kitty's clipboard protocol (kitty) copies files and images"
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return "install wl-clipboard (wl-copy), or xclip, or use a terminal with kitty's clipboard protocol"
	case os.Getenv("DISPLAY") != "":
		return "install xclip, or use a terminal with kitty's clipboard protocol"
	}
	return "no clipboard here: no Wayland or X11 display"
}

// osc5522 is the sequence that writes data of several types to the
// clipboard through the terminal, each in chunks as the protocol asks.
func osc5522(items []clipData) string {
	var sequence strings.Builder
	sequence.WriteString("\x1b]5522;type=write\x1b\\")
	for _, item := range items {
		mime := base64.StdEncoding.EncodeToString([]byte(item.mime))
		for start := 0; start < len(item.data); start += osc5522Chunk {
			chunk := item.data[start:min(start+osc5522Chunk, len(item.data))]
			fmt.Fprintf(&sequence, "\x1b]5522;type=wdata:mime=%s;%s\x1b\\", mime, base64.StdEncoding.EncodeToString(chunk))
		}
	}
	sequence.WriteString("\x1b]5522;type=wdata\x1b\\")
	return sequence.String()
}

// copyToSystem also puts the text on the desktop's clipboard, for terminals
// that do not take OSC 52, with the first program that works.
func copyToSystem(text string) tea.Cmd {
	if len(copyPrograms()) == 0 {
		return nil
	}
	return func() tea.Msg {
		_ = copyWith(clipData{data: []byte(text)})
		return nil
	}
}

// runWith runs a program with input on its stdin. Its output is not kept:
// wl-copy stays in the background to serve the clipboard, holding any pipe
// it was given open.
func runWith(ctx context.Context, input io.Reader, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = input
	return command.Run()
}
