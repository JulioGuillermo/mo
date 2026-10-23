package tui

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const keptSixelFrames = 16

var frameMark = regexp.MustCompile("\x1b_mo-frame;([0-9]+)\x1b\\\\")

// sixelScreen draws the sixel strips of a frame once the frame reaches the
// terminal. Each frame carries a mark with its number at its last cell; the
// output puts there the strips not on screen since the frame before.
type sixelScreen struct {
	mu     sync.Mutex
	last   int
	frames map[int]sixelFrame
	shown  map[render.SixelPlacement]bool
}

type sixelFrame struct {
	placements []render.SixelPlacement
	lastLine   int
}

func newSixelScreen() *sixelScreen {
	return &sixelScreen{frames: map[int]sixelFrame{}, shown: map[render.SixelPlacement]bool{}}
}

// mark numbers a frame and keeps the strips it shows.
func (s *sixelScreen) mark(frame string) string {
	if s == nil || !render.HasSixelImages() {
		return frame
	}
	shows := sixelFrame{placements: render.SixelPlacements(frame), lastLine: strings.Count(frame, "\n")}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	s.frames[s.last] = shows
	delete(s.frames, s.last-keptSixelFrames)
	return frame + fmt.Sprintf("\x1b_mo-frame;%d\x1b\\", s.last)
}

// draw replaces the marks in what goes to the terminal by the strips their
// frames bring. A screen erased shows none of the strips it had.
func (s *sixelScreen) draw(output []byte) []byte {
	marks := frameMark.FindAllSubmatchIndex(output, -1)
	if len(marks) == 0 {
		return output
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var drawn bytes.Buffer
	written := 0
	for _, mark := range marks {
		before := output[written:mark[0]]
		if bytes.Contains(before, []byte(ansi.EraseEntireScreen)) {
			clear(s.shown)
		}
		drawn.Write(before)
		number, _ := strconv.Atoi(string(output[mark[2]:mark[3]]))
		drawn.WriteString(s.stripsOf(number))
		written = mark[1]
	}
	drawn.Write(output[written:])
	return drawn.Bytes()
}

// stripsOf draws the strips of a frame not on screen yet, the cursor kept
// where it was. One on the frame's last line is drawn every time, since the
// mark rewrites the cell it ends on.
func (s *sixelScreen) stripsOf(number int) string {
	frame, ok := s.frames[number]
	if !ok {
		return ""
	}
	var strips strings.Builder
	shown := map[render.SixelPlacement]bool{}
	for _, placement := range frame.placements {
		shown[placement] = true
		if s.shown[placement] && placement.Y != frame.lastLine {
			continue
		}
		if strip, ok := render.SixelStrip(placement.ID, placement.Row); ok {
			strips.WriteString(ansi.CursorPosition(placement.X+1, placement.Y+1) + strip)
		}
	}
	s.shown = shown
	if strips.Len() == 0 {
		return ""
	}
	return ansi.SaveCursor + strips.String() + ansi.RestoreCursor
}

// sixelOutput is the terminal's output with the strips of each frame drawn
// in it.
type sixelOutput struct {
	*os.File
	screen *sixelScreen
}

func (o sixelOutput) Write(output []byte) (int, error) {
	if _, err := o.File.Write(o.screen.draw(output)); err != nil {
		return 0, err
	}
	return len(output), nil
}
