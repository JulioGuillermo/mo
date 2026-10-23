package render

import (
	"os"
	"strings"
)

// Thumbnail draws an image file of at most cols × rows cells, in kitty
// graphics or sixel: what to send the terminal once, kitty's upload, and the
// lines that show it.
func Thumbnail(path string, cols, rows int, mode Mode) (string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	img, err := DecodeFile(path, data)
	if err != nil {
		return "", nil, err
	}
	cellWidth, cellHeight := CellSize()
	box := fitWithin(img, cols, rows, cellWidth, cellHeight)
	if mode == ModeSixel {
		lines, err := sixelImage(img, box, cellWidth, cellHeight)
		return "", strings.Split(lines, "\n"), err
	}
	upload, lines, err := kittyParts(img, box, cellWidth, cellHeight)
	return upload, strings.Split(lines, "\n"), err
}
