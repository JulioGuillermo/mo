// Package notify tells the user that mo needs them: through the terminal's
// own notifications, the desktop's, or the bell.
package notify

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// Method is how a notification is sent.
type Method string

const (
	MethodAuto     Method = "auto"
	MethodTerminal Method = "terminal"
	MethodDesktop  Method = "desktop"
	MethodBell     Method = "bell"
	MethodOff      Method = "off"
)

// Methods are the methods a setting may name.
var Methods = []Method{MethodAuto, MethodTerminal, MethodDesktop, MethodBell, MethodOff}

const (
	desktopTimeout = 3 * time.Second
	longestBody    = 200
)

// terminal is a terminal's way of showing a notification.
type terminal int

const (
	noTerminal terminal = iota
	osc777              // Ghostty, WezTerm, foot, urxvt
	osc9                // iTerm2
	osc99               // kitty
)

// Notification is what is sent: a sequence for the terminal, or a command
// for the desktop, or neither when the method is off.
type Notification struct {
	Escape  string
	Desktop func() error
}

// Build makes a notification by method, picking for auto the terminal's
// own notifications, else the desktop's, else the bell. getenv reads the
// environment, to tell the terminal and a tmux around it.
func Build(method Method, title, body string, getenv func(string) string) Notification {
	title, body = clean(title), clean(body)
	if len([]rune(body)) > longestBody {
		body = string([]rune(body)[:longestBody-1]) + "…"
	}
	kind := detect(getenv)
	if method == MethodAuto {
		switch {
		case kind != noTerminal:
			method = MethodTerminal
		case desktopCommand() != nil:
			method = MethodDesktop
		default:
			method = MethodBell
		}
	}
	switch method {
	case MethodTerminal:
		if kind == noTerminal {
			kind = osc777
		}
		return Notification{Escape: passthrough(sequence(kind, title, body), getenv("TMUX") != "")}
	case MethodDesktop:
		if command := desktopCommand(); command != nil {
			return Notification{Desktop: func() error { return command(title, body) }}
		}
		return Notification{Escape: "\a"}
	case MethodBell:
		return Notification{Escape: "\a"}
	}
	return Notification{}
}

func detect(getenv func(string) string) terminal {
	program, term := strings.ToLower(getenv("TERM_PROGRAM")), getenv("TERM")
	switch {
	case getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty":
		return osc99
	case program == "iterm.app":
		return osc9
	case program == "ghostty" || program == "wezterm" || term == "xterm-ghostty" ||
		strings.HasPrefix(term, "foot") || strings.HasPrefix(term, "rxvt-unicode"):
		return osc777
	}
	return noTerminal
}

func sequence(kind terminal, title, body string) string {
	switch kind {
	case osc9:
		return "\x1b]9;" + title + ": " + body + "\a"
	case osc99:
		return "\x1b]99;i=mo:d=0;" + title + "\x1b\\" + "\x1b]99;i=mo:d=1:p=body;" + body + "\x1b\\"
	}
	return "\x1b]777;notify;" + strings.ReplaceAll(title, ";", ",") + ";" + body + "\a"
}

// passthrough wraps a sequence for tmux to hand it to the terminal outside.
func passthrough(sequence string, tmux bool) string {
	if !tmux {
		return sequence
	}
	return "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// clean keeps a text on one line and takes out what could end or start an
// escape sequence, since the body comes from the model.
func clean(text string) string {
	text = strings.Map(func(character rune) rune {
		switch {
		case character == '\n' || character == '\t':
			return ' '
		case character < 0x20 || character == 0x7f || (character >= 0x80 && character < 0xa0) || character == utf8.RuneError:
			return -1
		}
		return character
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

// desktopCommand is the desktop's notification command here, if any.
func desktopCommand() func(title, body string) error {
	run := func(name string, args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), desktopTimeout)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).Run()
	}
	switch {
	case runtime.GOOS == "darwin":
		return func(title, body string) error {
			script := "display notification " + appleString(body) + " with title " + appleString(title)
			return run("osascript", "-e", script)
		}
	case found("notify-send") && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""):
		return func(title, body string) error {
			return run("notify-send", "--app-name=mo", "--icon=utilities-terminal", title, body)
		}
	}
	return nil
}

func appleString(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text) + `"`
}

func found(program string) bool {
	_, err := exec.LookPath(program)
	return err == nil
}
