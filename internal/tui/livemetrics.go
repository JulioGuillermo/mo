package tui

import (
	"time"

	"github.com/jg/mo/internal/llm"
)

// liveMetrics are the reply being written's context and speeds, counted
// from its stream until the server gives the exact ones as it ends.
type liveMetrics struct {
	// prompt is how far the server read the prompt, from when it started.
	prompt        llm.PromptProgress
	promptStarted time.Time
	// pieces and runes are what the reply wrote so far, from its first.
	pieces, runes int
	firstPiece    time.Time
	lastPiece     time.Time
}

// add counts an event of the stream.
func (l *liveMetrics) add(event llm.Event, now time.Time) {
	switch event.Kind {
	case llm.EventPromptProgress:
		if l.promptStarted.IsZero() {
			l.promptStarted = now
		}
		l.prompt = event.Progress
	case llm.EventText, llm.EventReasoning, llm.EventToolCall:
		if event.Text == "" {
			return
		}
		if l.firstPiece.IsZero() {
			l.firstPiece = now
		}
		l.pieces++
		l.runes += len([]rune(event.Text))
		l.lastPiece = now
	}
}

// written is how many tokens the reply wrote so far, about: a piece is a
// token for most servers, and four characters one for those that send more.
func (l liveMetrics) written() int {
	return max(l.pieces, l.runes/4)
}

// context is how full the context is now, about: the prompt as the server
// counts it, or used before this reply, and what the reply wrote.
func (l liveMetrics) context(before int) int {
	if l.prompt.Total > 0 {
		return l.prompt.Total + l.written()
	}
	return before + l.written()
}

// generation is the tokens a second the reply is written at, from its first
// piece to its last; nothing before a second of it.
func (l liveMetrics) generation() float64 {
	elapsed := l.lastPiece.Sub(l.firstPiece).Seconds()
	if l.pieces < 2 || elapsed < 1 {
		return 0
	}
	return float64(l.written()-1) / elapsed
}

// promptRate is the tokens a second the server reads the prompt at, the
// cached part left out, while it reads it.
func (l liveMetrics) promptRate(now time.Time) float64 {
	elapsed := now.Sub(l.promptStarted).Seconds()
	read := l.prompt.Done - l.prompt.Cached
	if l.promptStarted.IsZero() || elapsed < 0.5 || read <= 0 {
		return 0
	}
	return float64(read) / elapsed
}

// active tells whether a reply is being counted.
func (l liveMetrics) active() bool {
	return l.pieces > 0 || l.prompt.Total > 0
}
