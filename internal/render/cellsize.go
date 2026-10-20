package render

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	fallbackCellWidth  = 8
	fallbackCellHeight = 16
	maxCellPixels      = 200
)

// CellSizeAuto asks the terminal for the size of its cells.
const CellSizeAuto = "auto"

type cellPixels struct{ width, height int }

var chosenCellSize atomic.Pointer[cellPixels]

// SetCellSize fixes the pixels of a terminal cell; 0 for either asks the
// terminal again.
func SetCellSize(width, height int) {
	if width <= 0 || height <= 0 {
		chosenCellSize.Store(nil)
		return
	}
	chosenCellSize.Store(&cellPixels{width, height})
}

// CellSize is the size of a terminal cell in pixels: the one set, else the
// one the terminal reports, else a common guess.
func CellSize() (width, height int) {
	if chosen := chosenCellSize.Load(); chosen != nil {
		return chosen.width, chosen.height
	}
	if width, height, ok := terminalCellSize(); ok {
		return width, height
	}
	return fallbackCellWidth, fallbackCellHeight
}

// ParseCellSize reads "WIDTHxHEIGHT" in pixels, or "auto" (or nothing) as 0, 0.
func ParseCellSize(text string) (width, height int, err error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" || text == CellSizeAuto {
		return 0, 0, nil
	}
	wrong := fmt.Errorf("cell size %q: use auto or WIDTHxHEIGHT in pixels, 1 to %d each, e.g. 8x16", text, maxCellPixels)
	widthText, heightText, found := strings.Cut(text, "x")
	if !found {
		return 0, 0, wrong
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(widthText))
	height, heightErr := strconv.Atoi(strings.TrimSpace(heightText))
	if widthErr != nil || heightErr != nil || !validCellPixels(width) || !validCellPixels(height) {
		return 0, 0, wrong
	}
	return width, height, nil
}

func validCellPixels(pixels int) bool {
	return pixels >= 1 && pixels <= maxCellPixels
}
