package render

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// SixelPlacement is a strip of an image whole on screen, at the cell where
// it starts.
type SixelPlacement struct {
	X, Y    int
	ID, Row int
}

// SixelPlacements finds in a frame the image strips it shows whole: a run of
// strip cells of one image row as wide as the strip. A strip partly covered,
// by a popup or the screen's edge, is left out.
func SixelPlacements(frame string) []SixelPlacement {
	if !strings.ContainsRune(frame, sixelCell) {
		return nil
	}
	var placements []SixelPlacement
	for y, line := range uv.NewStyledString(frame).Lines(ansi.GraphemeWidth) {
		placements = append(placements, lineStrips(line, y)...)
	}
	return placements
}

func lineStrips(line uv.Line, y int) []SixelPlacement {
	var placements []SixelPlacement
	for x := 0; x < len(line); {
		id, row, ok := stripCell(line[x])
		if !ok {
			x++
			continue
		}
		start := x
		for x < len(line) && sameStrip(line[x], id, row) {
			x++
		}
		if cols, known := sixelStripCols(id, row); known && x-start == cols {
			placements = append(placements, SixelPlacement{X: start, Y: y, ID: id, Row: row})
		}
	}
	return placements
}

func stripCell(cell uv.Cell) (id, row int, ok bool) {
	if cell.Content != string(sixelCell) || cell.Style.Fg == nil {
		return 0, 0, false
	}
	red, green, blue, _ := cell.Style.Fg.RGBA()
	return int(red>>8)<<8 | int(green>>8), int(blue >> 8), true
}

func sameStrip(cell uv.Cell, id, row int) bool {
	cellID, cellRow, ok := stripCell(cell)
	return ok && cellID == id && cellRow == row
}
