package render

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SX-01 | Happy    | A sixel image is one line of strip cells per row, each row a strip    | P0       |
// | SX-02 | Happy    | Each strip is a sixel sequence one cell tall, transparent background  | P0       |
// | SX-03 | Happy    | A strip after other text is found at the cell where it starts         | P1       |
// | SX-04 | Edge     | A strip partly covered is left out, the rest of the image kept        | P0       |
// | SX-05 | Edge     | A frame with no strip cells has no placements                         | P2       |
// | SX-06 | Happy    | A sixel thumbnail has no upload and as many lines as rows             | P1       |
// | SX-07 | Happy    | A reported cell size is used when the window has none                 | P1       |

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkerboard(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			if (x/4+y/4)%2 == 0 {
				img.Set(x, y, color.RGBA{R: 200, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 200, A: 255})
			}
		}
	}
	return img
}

func sixelDrawn(t *testing.T, width, height int) string {
	t.Helper()
	SetCellSize(8, 16)
	t.Cleanup(func() { SetCellSize(0, 0) })
	drawn, err := Renderer{Mode: ModeSixel, Width: 80}.Image(checkerboard(width, height))
	if err != nil {
		t.Fatal(err)
	}
	return drawn
}

func TestSixel(t *testing.T) {
	t.Run("SX-01 A sixel image is one line of strip cells per row, each row a strip", func(t *testing.T) {
		drawn := sixelDrawn(t, 40, 48)

		placements := SixelPlacements(drawn)

		if len(placements) != 3 {
			t.Fatalf("got %d placements for 3 rows: %+v", len(placements), placements)
		}
		for row, placement := range placements {
			if placement.X != 0 || placement.Y != row || placement.Row != row || placement.ID != placements[0].ID {
				t.Errorf("row %d: %+v", row, placement)
			}
		}
		if cells := strings.Count(drawn, string(sixelCell)); cells != 5*3 {
			t.Errorf("got %d cells for 5 × 3", cells)
		}
	})

	t.Run("SX-02 Each strip is a sixel sequence one cell tall, transparent background", func(t *testing.T) {
		placement := SixelPlacements(sixelDrawn(t, 40, 48))[1]

		strip, ok := SixelStrip(placement.ID, placement.Row)

		if !ok || !strings.HasPrefix(strip, "\x1bP0;1q\"1;1;40;16") || !strings.HasSuffix(strip, "\x1b\\") {
			t.Errorf("strip %q…", strip[:min(len(strip), 30)])
		}
	})

	t.Run("SX-03 A strip after other text is found at the cell where it starts", func(t *testing.T) {
		lines := strings.Split(sixelDrawn(t, 16, 16), "\n")

		placements := SixelPlacements("title\n\x1b[1mab\x1b[0m " + lines[0])

		if len(placements) != 1 || placements[0].X != 3 || placements[0].Y != 1 {
			t.Errorf("got %+v", placements)
		}
	})

	t.Run("SX-04 A strip partly covered is left out, the rest of the image kept", func(t *testing.T) {
		lines := strings.Split(sixelDrawn(t, 40, 32), "\n")
		covered := []rune(lines[0])
		covered[len(covered)-8] = 'x'
		lines[0] = string(covered)

		placements := SixelPlacements(strings.Join(lines, "\n"))

		if len(placements) != 1 || placements[0].Row != 1 {
			t.Errorf("got %+v", placements)
		}
	})

	t.Run("SX-05 A frame with no strip cells has no placements", func(t *testing.T) {
		if placements := SixelPlacements("plain\n\x1b[31mtext\x1b[0m"); placements != nil {
			t.Errorf("got %+v", placements)
		}
	})

	t.Run("SX-06 A sixel thumbnail has no upload and as many lines as rows", func(t *testing.T) {
		SetCellSize(8, 16)
		defer SetCellSize(0, 0)
		path := filepath.Join(t.TempDir(), "board.png")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, checkerboard(64, 64)); err != nil {
			t.Fatal(err)
		}
		file.Close()

		upload, lines, err := Thumbnail(path, 14, 3, ModeSixel)

		if err != nil || upload != "" || len(lines) != 3 || len(SixelPlacements(strings.Join(lines, "\n"))) != 3 {
			t.Errorf("upload %q, %d lines, %v", upload, len(lines), err)
		}
	})

	t.Run("SX-07 A reported cell size is used when the window has none", func(t *testing.T) {
		defer reportedCellSize.Store(nil)
		SetReportedCellSize(10, 21)
		width, height := CellSize()
		_, _, fromWindow := terminalCellSize()

		if !fromWindow && (width != 10 || height != 21) {
			t.Errorf("got %d x %d", width, height)
		}
	})
}
