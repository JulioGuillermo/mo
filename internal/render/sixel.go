package render

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/sixel"
	"golang.org/x/image/draw"
)

const (
	// sixelCell is the glyph of the cells an image strip covers: blank, and
	// not a space, so no trimming of lines takes it away.
	sixelCell        = '\u2800'
	maxSixelIDs      = 1 << 16
	keptSixelImages  = 512
	sixelTransparent = 1
)

// sixelImages holds every image drawn as sixel strips, one strip per text
// row, by id; the screen draws the strips it shows after each frame.
var sixelImages = struct {
	sync.Mutex
	next   int
	images map[int]sixelStrips
}{images: map[int]sixelStrips{}}

type sixelStrips struct {
	cols   int
	strips []string
}

// sixelImage cuts the image into strips one text row tall, keeps them, and
// returns the lines that stand for them.
func sixelImage(img image.Image, box Box, cellWidth, cellHeight int) (string, error) {
	width := box.Cols * cellWidth
	scaled := image.NewRGBA(image.Rect(0, 0, width, box.Rows*cellHeight))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Src, nil)
	strips := make([]string, box.Rows)
	for row := range box.Rows {
		strip, err := encodeSixel(stripAt(scaled, row*cellHeight, width, cellHeight))
		if err != nil {
			return "", err
		}
		strips[row] = strip
	}
	return sixelLines(keepSixelImage(sixelStrips{box.Cols, strips}), box), nil
}

// stripAt copies a band of the image to one of its own, starting at 0, 0
// as the encoder reads it.
func stripAt(img *image.RGBA, top, width, height int) *image.RGBA {
	strip := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(strip, strip.Bounds(), img, image.Pt(0, top), draw.Src)
	return strip
}

func encodeSixel(strip image.Image) (string, error) {
	var payload bytes.Buffer
	if err := new(sixel.Encoder).Encode(&payload, strip); err != nil {
		return "", err
	}
	return ansi.SixelGraphics(0, sixelTransparent, 0, payload.Bytes()), nil
}

func keepSixelImage(kept sixelStrips) int {
	sixelImages.Lock()
	defer sixelImages.Unlock()
	sixelImages.next = (sixelImages.next + 1) % maxSixelIDs
	id := sixelImages.next
	sixelImages.images[id] = kept
	delete(sixelImages.images, (id-keptSixelImages+maxSixelIDs)%maxSixelIDs)
	return id
}

// sixelLines are one line of blank cells per strip, the image id and the
// strip's row carried in their foreground colour.
func sixelLines(id int, box Box) string {
	cells := strings.Repeat(string(sixelCell), box.Cols)
	lines := make([]string, box.Rows)
	for row := range box.Rows {
		lines[row] = fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[39m", id>>8, id&0xff, row, cells)
	}
	return strings.Join(lines, "\n")
}

// SixelStrip is the sixel sequence of one row of an image.
func SixelStrip(id, row int) (string, bool) {
	sixelImages.Lock()
	defer sixelImages.Unlock()
	kept, ok := sixelImages.images[id]
	if !ok || row >= len(kept.strips) {
		return "", false
	}
	return kept.strips[row], true
}

func sixelStripCols(id, row int) (int, bool) {
	sixelImages.Lock()
	defer sixelImages.Unlock()
	kept, ok := sixelImages.images[id]
	if !ok || row >= len(kept.strips) {
		return 0, false
	}
	return kept.cols, true
}

// HasSixelImages tells whether any image was drawn as sixel strips.
func HasSixelImages() bool {
	sixelImages.Lock()
	defer sixelImages.Unlock()
	return len(sixelImages.images) > 0
}
