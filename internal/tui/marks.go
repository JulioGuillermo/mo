package tui

import (
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/spinner"

	"github.com/jg/mo/internal/config"
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

// animations are the times between each animation's frames, as the
// settings say.
type animations struct {
	spinner, running, writing, reasoning, asking time.Duration
}

var paces atomic.Pointer[animations]

func init() {
	setAnimations(config.Defaults().Render)
}

// setAnimations takes the times between frames from the settings.
func setAnimations(settings config.Render) {
	ms := func(value int) time.Duration { return time.Duration(max(value, config.MinFrameMS)) * time.Millisecond }
	paces.Store(&animations{spinner: ms(settings.SpinnerMS), running: ms(settings.RunningMS), writing: ms(settings.WritingMS),
		reasoning: ms(settings.ReasoningMS), asking: ms(settings.AskingMS)})
}

// quickest is the shortest time between frames of any animation: the
// screen is drawn again at it while something moves.
func quickest() time.Duration {
	p := paces.Load()
	return min(p.spinner, p.running, p.writing, p.reasoning, p.asking)
}

// frameAt is an animation's frame now, every so long a frame.
func frameAt(frames []string, every time.Duration) string {
	return frameAtTime(frames, every, time.Now())
}

func frameAtTime(frames []string, every time.Duration, now time.Time) string {
	return frames[int(now.UnixMilli()/every.Milliseconds())%len(frames)]
}

// askingSign is the sign of waiting for the user, as it beats now.
func askingSign() string {
	return frameAt(askingFrames, paces.Load().asking)
}

// arc is the arc as it turns now.
func arc() string {
	return frameAt(arcFrames, paces.Load().running)
}

// statusFrame is the status line's spinner as it turns now.
func statusFrame() string {
	return frameAt(spinner.MiniDot.Frames, paces.Load().spinner)
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

// forming is the sign of a call being written, as it breathes now.
func forming() string {
	return frameAt(formingFrames, paces.Load().writing)
}

// sparkFrames are the model reasoning: a spark fading in and out.
var sparkFrames = []string{"✦", "✧", "⊹", "·", "⊹", "✧"}

// spark is the sign of reasoning, as it twinkles now.
func spark() string {
	return frameAt(sparkFrames, paces.Load().reasoning)
}

// spinnerView is the status line's spinner, turning at its own pace.
func (m model) spinnerView() string {
	return m.spinner.Style.Render(statusFrame())
}

// paceAnimations takes the animations' times from the settings; the
// spinner's ticks, which draw the screen again, come as often as the
// quickest needs.
func (m *model) paceAnimations() {
	setAnimations(m.session.Config.Render)
	m.spinner.Spinner.FPS = quickest()
}
