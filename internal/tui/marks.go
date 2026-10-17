package tui

import (
	"strings"
	"time"
)

// The signs of how a call, a job or a subagent stands, all round so they
// read alike: dotted while it waits its turn, an arc going round while it
// runs, a dot in a ring while it waits for the user, filled once done, a
// cross when it failed and a bar when it was stopped.
const (
	markQueued  = "◌"
	markDone    = "●"
	markStopped = "⊘"
	// markFailedText is the cross without a Nerd Font; with one, the filled
	// circle with a cross closing buttons have.
	markFailedText = "⊗"
	markFailedNerd = ""
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

// markFailed is the sign of a failure, as the icons set allows.
func markFailed(nerd bool) string {
	if nerd {
		return markFailedNerd
	}
	return markFailedText
}
