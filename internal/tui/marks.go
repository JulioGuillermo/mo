package tui

import (
	"strings"
	"time"
)

// The signs of how a call, a job or a subagent stands, round so they read
// alike: a bubble breathing while a call is written, dotted while it waits
// its turn, an arc going round while it runs, beating while it waits for
// the user, filled once done, a cross when it failed, a bar when stopped.
const (
	markQueued  = "◌"
	markDone    = "●"
	markStopped = "⊘"
	// markFailed is a ballot cross: the same in every font, where the
	// round ones were too big in some.
	markFailed = "✗"
	// markRunning stands in a block drawn once for the arc, which turns
	// where the block is shown.
	markRunning = "\U0010FFFD"
)

var arcFrames = []string{"◜", "◝", "◞", "◟"}

// askingFrames beat while a call or a subagent waits for the user.
var askingFrames = []string{"⊙", "◎"}

const askingBeat = 500 * time.Millisecond

// askingSign is the sign of waiting for the user, as it beats now.
func askingSign() string {
	return askingFrames[int(time.Now().UnixMilli()/askingBeat.Milliseconds())%len(askingFrames)]
}

const arcFrame = 150 * time.Millisecond

// arc is the arc as it turns now.
func arc() string {
	return arcFrames[int(time.Now().UnixMilli()/arcFrame.Milliseconds())%len(arcFrames)]
}

// turning puts the arc as it turns now where a block left its place.
func turning(text string) string {
	if !strings.Contains(text, markRunning) {
		return text
	}
	return strings.ReplaceAll(text, markRunning, arc())
}

// formingFrames are a call being written: a bubble breathing.
var formingFrames = []string{"·", "∘", "○", "∘"}

const formingBreath = 220 * time.Millisecond

// forming is the sign of a call being written, as it breathes now.
func forming() string {
	return formingFrames[int(time.Now().UnixMilli()/formingBreath.Milliseconds())%len(formingFrames)]
}

// sparkFrames are the model reasoning: a spark fading in and out.
var sparkFrames = []string{"✦", "✧", "⊹", "·", "⊹", "✧"}

const sparkFrame = 160 * time.Millisecond

// spark is the sign of reasoning, as it twinkles now.
func spark() string {
	return sparkFrames[int(time.Now().UnixMilli()/sparkFrame.Milliseconds())%len(sparkFrames)]
}
