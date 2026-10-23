package tui

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const keptSixelFrames = 16

var (
	frameMark = regexp.MustCompile("\x1b_mo-frame;([0-9]+)\x1b\\\\")
	// lineMoves are the margins, insertions and deletions of lines a
	// renderer scrolls part of the screen with.
	lineMoves = regexp.MustCompile(`\x1b\[[0-9;]*[rLM]`)
)

// sixelScreen draws the sixel strips of a frame once the frame reaches the
// terminal. Each frame carries a mark with its number at its last cell; the
// output puts there the strips not on screen since the frame before. A frame
// that scrolls part of the screen is painted again whole, as terminals move
// the pictures of the rows outside the scrolled part too.
type sixelScreen struct {
	mu     sync.Mutex
	last   int
	frames map[int]sixelFrame
	shown  map[render.SixelPlacement]bool
}

type sixelFrame struct {
	text       string
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
	shows := sixelFrame{text: frame, placements: render.SixelPlacements(frame), lastLine: strings.Count(frame, "\n")}
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
		if lineMoves.Match(before) && len(s.shown) > 0 {
			drawn.WriteString(s.repaint(number))
		} else if strips := s.stripsOf(number); strips != "" {
			drawn.WriteString(ansi.SaveCursor + strips + ansi.RestoreCursor)
		}
		written = mark[1]
	}
	drawn.Write(output[written:])
	return drawn.Bytes()
}

// stripsOf draws the strips of a frame not on screen yet, moving the cursor.
// One on the frame's last line is drawn every time, since the
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
	return strips.String()
}

// repaint erases the screen, pictures and all, and paints the frame again
// with its strips, the cursor and its colours kept where they were.
func (s *sixelScreen) repaint(number int) string {
	frame, ok := s.frames[number]
	if !ok {
		return ""
	}
	var painted strings.Builder
	painted.WriteString(ansi.SaveCursor + ansi.ResetStyle + ansi.EraseEntireScreen)
	for y, line := range uv.NewStyledString(frame.text).Lines(ansi.GraphemeWidth) {
		painted.WriteString(ansi.CursorPosition(1, y+1) + line.Render() + ansi.ResetStyle)
	}
	clear(s.shown)
	painted.WriteString(s.stripsOf(number) + ansi.RestoreCursor)
	return painted.String()
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
