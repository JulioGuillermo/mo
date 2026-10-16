package render

import (
	"fmt"
	"image"
	"strings"
)

const maxWidthShare = 0.8

// Renderer turns visual content into text for the scrollback, sized to a
// terminal width.
type Renderer struct {
	Mode  Mode
	Width int
	// Dark tells the background is dark, for the colours of diagrams.
	Dark bool
}

// Image draws an image in the renderer's mode. With images off it names
// the image instead.
func (r Renderer) Image(img image.Image) (string, error) {
	bounds := img.Bounds()
	switch r.Mode {
	case ModeKitty:
		cellWidth, cellHeight := CellSize()
		return kittyImage(img, fit(img, r.maxCols(), cellWidth, cellHeight), cellWidth, cellHeight)
	case ModeBlocks:
		return halfBlocks(img, fit(img, r.maxCols(), 1, 2)), nil
	}
	return fmt.Sprintf("[image %dx%d]", bounds.Dx(), bounds.Dy()), nil
}

func (r Renderer) ImageData(data []byte) (string, error) {
	img, err := Decode(data)
	if err != nil {
		return "", err
	}
	return r.Image(img)
}

func (r Renderer) SVG(svg []byte) (string, error) {
	if r.Mode == ModeOff {
		return "[svg image]", nil
	}
	cellWidth, cellHeight := CellSize()
	img, err := rasterizeSVG(svg, r.maxCols()*cellWidth, maxImageRows*cellHeight)
	if err != nil {
		return "", err
	}
	return r.Image(img)
}

// Mermaid draws a diagram as an image when mermaid-cli is installed and the
// terminal speaks kitty graphics, and with box characters otherwise. A
// diagram neither can draw comes back as its source.
func (r Renderer) Mermaid(source string) string {
	if r.Mode == ModeKitty {
		if svg, ok := mermaidSVG(source); ok {
			if drawn, err := r.SVG(svg); err == nil {
				return drawn
			}
		}
	}
	drawn, err := mermaidText(source, r.Width)
	if err != nil || strings.TrimSpace(drawn) == "" {
		return "```mermaid\n" + strings.TrimSpace(source) + "\n```"
	}
	return colorDiagram(drawn, r.Dark)
}

func (r Renderer) maxCols() int {
	return max(10, int(float64(r.Width)*maxWidthShare))
}
