package render

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxImageRows = 40
	maxPixels    = 50_000_000
)

// Box is the space an image takes on screen, in cells.
type Box struct {
	Cols int
	Rows int
}

// Decode reads an image, refusing first by its header one too large to hold
// in memory.
func Decode(data []byte) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not an image mo can read: %w", err)
	}
	if config.Width*config.Height > maxPixels {
		return nil, fmt.Errorf("image of %dx%d pixels is too large to show", config.Width, config.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not an image mo can read: %w", err)
	}
	return img, nil
}

// fit sizes an image to at most maxCols columns and maxImageRows rows,
// keeping its proportions, and never larger than its own pixels.
func fit(img image.Image, maxCols, cellWidth, cellHeight int) Box {
	return fitWithin(img, maxCols, maxImageRows, cellWidth, cellHeight)
}

func fitWithin(img image.Image, maxCols, maxRows, cellWidth, cellHeight int) Box {
	bounds := img.Bounds()
	cols := ceilDiv(bounds.Dx(), cellWidth)
	rows := ceilDiv(bounds.Dy(), cellHeight)
	scale := min(1.0, float64(maxCols)/float64(cols), float64(maxRows)/float64(rows))
	return Box{Cols: max(1, int(float64(cols)*scale)), Rows: max(1, int(float64(rows)*scale))}
}

// shrink scales an image down to at most width x height pixels; a smaller
// one is returned as it is.
func shrink(img image.Image, width, height int) image.Image {
	bounds := img.Bounds()
	if bounds.Dx() <= width && bounds.Dy() <= height {
		return img
	}
	scale := min(float64(width)/float64(bounds.Dx()), float64(height)/float64(bounds.Dy()))
	target := image.Rect(0, 0, max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale)))
	scaled := image.NewRGBA(target)
	draw.ApproxBiLinear.Scale(scaled, target, img, bounds, draw.Src, nil)
	return scaled
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

// svgCopySide is the longest side, in pixels, of an SVG drawn to be copied.
const svgCopySide = 1600

// DecodeFile reads a picture file: an image as it is, an SVG drawn at
// svgCopySide pixels on its longest side.
func DecodeFile(path string, data []byte) (image.Image, error) {
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		return rasterizeSVG(data, svgCopySide, svgCopySide)
	}
	return Decode(data)
}
