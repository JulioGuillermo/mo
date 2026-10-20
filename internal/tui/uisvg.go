package tui

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

const (
	uiSVGWidth  = 40
	uiSVGHeight = 12
)

// svgReadyMsg says a picture of an svg node was drawn, to show it.
type svgReadyMsg struct{}

// svgFrames draws the svg nodes of interfaces away from the screen's
// drawing, which shows the last frame done meanwhile. A change while one
// is drawn waits; the changes between are skipped, only the last drawn.
type svgFrames struct {
	mu     sync.Mutex
	frames map[string]*svgFrame
	// ready tells the screen a frame is done; without it, as in tests,
	// frames are drawn at once.
	ready func()
	// kitty draws pictures as kitty images at the terminal's resolution,
	// each node's under an id of its own sent again for each frame; else
	// in half blocks.
	kitty bool
}

// svgFrame is one node's picture: the one shown and the one wanted.
type svgFrame struct {
	shown, wanted       string
	lines               []string
	failed              string
	cols, rows          int
	busy                bool
	wantedCols, wantRow int
	// id is the kitty image the frames are sent under, and upload the
	// last frame's, not sent yet.
	id     uint32
	upload string
}

// lines are the picture of a node's svg at a size: the last one drawn,
// while the one asked for is drawn; nil before any.
func (f *svgFrames) lines(slot, svg string, cols, rows int) ([]string, string) {
	key := fmt.Sprintf("%dx%d\x00%s", cols, rows, svg)
	f.mu.Lock()
	if f.frames == nil {
		f.frames = map[string]*svgFrame{}
	}
	frame, ok := f.frames[slot]
	if !ok {
		frame = &svgFrame{id: render.NewImageID()}
		f.frames[slot] = frame
	}
	if frame.shown == key {
		lines, failed := frame.lines, frame.failed
		f.mu.Unlock()
		return lines, failed
	}
	frame.wanted, frame.wantedCols, frame.wantRow = key, cols, rows
	start := !frame.busy
	frame.busy = true
	ready := f.ready
	f.mu.Unlock()
	if start {
		if ready == nil {
			f.draw(frame, func() {})
		} else {
			go f.draw(frame, ready)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return frame.lines, frame.failed
}

// uploads are the kitty frames done and not sent yet, to send now.
func (f *svgFrames) uploads() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out strings.Builder
	for _, frame := range f.frames {
		out.WriteString(frame.upload)
		frame.upload = ""
	}
	return out.String()
}

// draw draws what the frame wants until what it drew is the last asked
// for, telling the screen of each frame done.
func (f *svgFrames) draw(frame *svgFrame, ready func()) {
	for {
		f.mu.Lock()
		key, cols, rows := frame.wanted, frame.wantedCols, frame.wantRow
		f.mu.Unlock()
		source := key[strings.IndexByte(key, 0)+1:]
		var lines []string
		var upload string
		var err error
		if f.kitty {
			upload, lines, err = render.SVGKitty([]byte(source), cols, rows, frame.id)
		} else {
			lines, err = render.SVGCells([]byte(source), cols, rows)
		}
		f.mu.Lock()
		if upload != "" {
			// Frames under one id each take the last one's place: only the
			// last needs sending.
			frame.upload = upload
		}
		frame.shown, frame.cols, frame.rows = key, cols, rows
		if err != nil {
			frame.failed = err.Error()
		} else {
			frame.lines, frame.failed = lines, ""
		}
		done := frame.wanted == key
		if done {
			frame.busy = false
		}
		f.mu.Unlock()
		ready()
		if done {
			return
		}
	}
}

// svg draws an svg node's picture, the last drawn while the next is drawn;
// before any, or when its svg cannot be drawn, a box saying so.
func (r uiRenderer) svg(n tools.UINode, room uiRoom, bg string) uiBox {
	cols, rows := uiSVGWidth, uiSVGHeight
	if room.fillWidth {
		cols = room.width
	}
	if room.fillHeight {
		rows = room.height
	}
	cols = max(min(cols, room.width), 1)
	frames := r.svgs
	if frames == nil {
		frames = &svgFrames{}
	}
	lines, failed := frames.lines(fmt.Sprintf("%d/%s", r.ui, n.ID), n.SVG, cols, rows)
	if lines == nil {
		say := "drawing…"
		if failed != "" {
			say = "svg: " + failed
		}
		lines = make([]string, rows)
		for i := range lines {
			lines[i] = blank(cols, bg)
		}
		lines[rows/2] = uiStyle("dim", bg, false).Render(lipgloss.PlaceHorizontal(cols, lipgloss.Center, ansi.Truncate(say, cols, "…")))
	}
	var hits []uiHit
	if n.OnClick != "" && !n.Disabled {
		hits = []uiHit{{x1: cols, y1: rows, id: n.ID}}
	}
	return uiBox{lines: lines, width: cols, hits: hits}
}
