package render

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"
)

const opaqueEnough = 128

// halfBlocks draws two pixel rows per text row: the upper half block takes
// the top pixel as its colour and the cell background the bottom one.
// Transparent pixels keep the terminal's own background.
func halfBlocks(img image.Image, box Box) string {
	scaled := image.NewRGBA(image.Rect(0, 0, box.Cols, box.Rows*2))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Src, nil)
	var out strings.Builder
	for row := range box.Rows {
		for col := range box.Cols {
			top := scaled.RGBAAt(col, row*2)
			bottom := scaled.RGBAAt(col, row*2+1)
			out.WriteString(halfBlockCell(top, bottom))
		}
		out.WriteString("\x1b[0m")
		if row < box.Rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func halfBlockCell(top, bottom color.RGBA) string {
	topShown, bottomShown := top.A >= opaqueEnough, bottom.A >= opaqueEnough
	switch {
	case topShown && bottomShown:
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.R, top.G, top.B, bottom.R, bottom.G, bottom.B)
	case topShown:
		return fmt.Sprintf("\x1b[49m\x1b[38;2;%d;%d;%dm▀", top.R, top.G, top.B)
	case bottomShown:
		return fmt.Sprintf("\x1b[49m\x1b[38;2;%d;%d;%dm▄", bottom.R, bottom.G, bottom.B)
	}
	return "\x1b[0m "
}

// svgCellScale is how many pixels a cell's width is drawn at before the
// half blocks average it down, so thin lines and small text keep their
// shape.
const svgCellScale = 4

// SVGCells draws an SVG in half blocks filling cols × rows cells, its
// proportions kept and centred; what it leaves clear shows the background.
func SVGCells(svg []byte, cols, rows int) ([]string, error) {
	if cols <= 0 || rows <= 0 {
		return nil, nil
	}
	width, height := cols*svgCellScale, rows*2*svgCellScale
	img, err := rasterizeSVG(svg, width, height)
	if err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := img.Bounds()
	offset := image.Pt((width-bounds.Dx())/2, (height-bounds.Dy())/2)
	draw.Draw(canvas, bounds.Sub(bounds.Min).Add(offset), img, bounds.Min, draw.Over)
	return strings.Split(halfBlocks(canvas, Box{Cols: cols, Rows: rows}), "\n"), nil
}
