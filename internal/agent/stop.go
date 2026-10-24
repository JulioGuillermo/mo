package agent

import (
	"context"
	"sync"
)

// StopMode is a stop asked while the agent works, short of stopping it at
// once, which cancelling its context does.
type StopMode int32

const (
	StopNone StopMode = iota
	// StopAfterRound lets the round end, the reply and all its calls, and
	// stops before the model is asked again.
	StopAfterRound
	// StopAfterCalls cuts the reply being written, lets the calls running
	// end with their results and runs no other.
	StopAfterCalls
)

// stopRequest is the stop asked, and how to reach what it cuts: the reply
// being written and the calls run while it is.
type stopRequest struct {
	mu       sync.Mutex
	mode     StopMode
	cutReply context.CancelFunc
	early    *earlyRunner
}

// RequestStop asks the agent to stop as mode says, in place of a stop asked
// before.
func (a *Agent) RequestStop(mode StopMode) {
	a.stop.mu.Lock()
	defer a.stop.mu.Unlock()
	a.stop.mode = mode
	if mode != StopAfterCalls {
		return
	}
	if a.stop.cutReply != nil {
		a.stop.cutReply()
	}
	if a.stop.early != nil {
		a.stop.early.holdBack()
	}
}

// ClearStop takes back a stop asked, as /stop cancel and a run starting do:
// what it cut already stays cut.
func (a *Agent) ClearStop() {
	a.RequestStop(StopNone)
}

// Stopping tells whether a stop is asked.
func (a *Agent) Stopping() bool {
	return a.stopMode() != StopNone
}

func (a *Agent) stopMode() StopMode {
	a.stop.mu.Lock()
	defer a.stop.mu.Unlock()
	return a.stop.mode
}

// writingReply is the context of a reply about to be written, which a stop
// that lets the calls end cuts; ended lets it go once the reply is over.
func (a *Agent) writingReply(ctx context.Context) (replyCtx context.Context, ended func()) {
	replyCtx, cut := context.WithCancel(ctx)
	a.stop.mu.Lock()
	defer a.stop.mu.Unlock()
	a.stop.cutReply = cut
	if a.stop.mode == StopAfterCalls {
		cut()
	}
	return replyCtx, func() {
		a.stop.mu.Lock()
		defer a.stop.mu.Unlock()
		a.stop.cutReply = nil
		cut()
	}
}

func (a *Agent) runningEarly(early *earlyRunner) {
	a.stop.mu.Lock()
	defer a.stop.mu.Unlock()
	a.stop.early = early
	if early != nil && a.stop.mode == StopAfterCalls {
		early.holdBack()
	}
}

// heldBack tells whether calls not run yet stay so.
func (a *Agent) heldBack() bool {
	return a.stopMode() == StopAfterCalls
}
