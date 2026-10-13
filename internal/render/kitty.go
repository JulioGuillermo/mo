package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math/rand/v2"
	"strings"
	"sync/atomic"
)

const (
	kittyChunkBytes = 4096
	placeholder     = '\U0010EEEE'
	maxImageID      = 1<<24 - 1
)

var nextImageID atomic.Uint32

func init() {
	nextImageID.Store(rand.Uint32N(maxImageID/2) + 1)
}

// kittyImage sends the image to the terminal and returns the text that
// shows it: the transmission, then one line of placeholder cells per row.
// Placeholders are ordinary text to the UI, so the image scrolls with the
// conversation. The image id travels in the cells' foreground colour.
func kittyImage(img image.Image, box Box, cellWidth, cellHeight int) (string, error) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, shrink(img, box.Cols*cellWidth, box.Rows*cellHeight)); err != nil {
		return "", err
	}
	id := nextImageID.Add(1) % maxImageID
	transmission := kittyTransmission(base64.StdEncoding.EncodeToString(encoded.Bytes()), id, box)
	return transmission + kittyPlaceholders(id, box), nil
}

func kittyTransmission(payload string, id uint32, box Box) string {
	var out strings.Builder
	for start := 0; ; start += kittyChunkBytes {
		end := min(start+kittyChunkBytes, len(payload))
		more := 1
		if end == len(payload) {
			more = 0
		}
		command := fmt.Sprintf("m=%d", more)
		if start == 0 {
			command = fmt.Sprintf("a=T,U=1,f=100,t=d,q=2,i=%d,c=%d,r=%d,m=%d", id, box.Cols, box.Rows, more)
		}
		out.WriteString(passthrough("\x1b_G" + command + ";" + payload[start:end] + "\x1b\\"))
		if more == 0 {
			return out.String()
		}
	}
}

// kittyPlaceholders writes the first cell of each row with its row and
// column marks; the cells after it take the next columns.
func kittyPlaceholders(id uint32, box Box) string {
	color := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xff, id>>8&0xff, id&0xff)
	rest := strings.Repeat(string(placeholder), box.Cols-1)
	var lines []string
	for row := range min(box.Rows, len(diacritics)) {
		first := string([]rune{placeholder, diacritics[row], diacritics[0]})
		lines = append(lines, color+first+rest+"\x1b[39m")
	}
	return strings.Join(lines, "\n")
}

// passthrough wraps a sequence so tmux hands it to the terminal outside.
func passthrough(sequence string) string {
	if !insideTmux() {
		return sequence
	}
	return "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
}
