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
