// Package render draws images, SVG and Mermaid diagrams as text a terminal
// can show: kitty graphics where the terminal speaks it, coloured half
// blocks elsewhere.
package render

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeKitty  Mode = "kitty"
	ModeBlocks Mode = "blocks"
	ModeOff    Mode = "off"
)

const tmuxQueryTimeout = time.Second

// Detect turns auto into what the terminal supports, judging by its
// environment: kitty and ghostty speak kitty graphics, unless tmux sits in
// between without passthrough; any truecolor terminal gets half blocks.
func Detect(mode Mode) Mode {
	if mode != ModeAuto && mode != "" {
		return mode
	}
	truecolor := os.Getenv("COLORTERM") == "truecolor" || os.Getenv("COLORTERM") == "24bit"
	switch {
	case speaksKitty() && (!insideTmux() || tmuxPassesThrough()):
		return ModeKitty
	case truecolor || speaksKitty():
		return ModeBlocks
	}
	return ModeOff
}

func speaksKitty() bool {
	term := os.Getenv("TERM")
	return os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" || term == "xterm-ghostty" ||
		strings.EqualFold(os.Getenv("TERM_PROGRAM"), "ghostty") || os.Getenv("GHOSTTY_RESOURCES_DIR") != ""
}

func tmuxPassesThrough() bool {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxQueryTimeout)
	defer cancel()
	query := exec.CommandContext(ctx, "tmux", "display", "-p", "#{allow-passthrough}")
	query.WaitDelay = tmuxQueryTimeout / 10
	output, err := query.Output()
	setting := strings.TrimSpace(string(output))
	return err == nil && (setting == "on" || setting == "all")
}

func insideTmux() bool {
	return os.Getenv("TMUX") != ""
}
