package tui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

// tokensAtMost is how long a reply drawn every so many pieces waits at most
// for them, so a slow model is still seen writing.
const tokensAtMost = time.Second

// replyPace is how often what the model writes reaches the screen.
type replyPace struct {
	mode   string
	every  time.Duration
	tokens int
}

func paceOf(settings config.Render) replyPace {
	return replyPace{
		mode:   settings.ReplyRefresh,
		every:  time.Duration(settings.ReplyEveryMS) * time.Millisecond,
		tokens: settings.ReplyEveryTokens,
	}
}

// timedEvent is a piece of the reply and when it came, for the speeds.
type timedEvent struct {
	event llm.Event
	at    time.Time
}

// streamBatchMsg is the pieces of the reply held since the last were drawn,
// drawn at once.
type streamBatchMsg []timedEvent

// pacer holds the pieces the model writes and sends them on together, as
// the pace says; everything else the agent tells first sends what it holds,
// so nothing comes out of order.
type pacer struct {
	// mu guards the pace and what is held; sending keeps the batches and
	// what follows them in order, and is never held by the UI's goroutine.
	mu      sync.Mutex
	sending sync.Mutex
	pace    replyPace
	held    []timedEvent
	since   time.Time
	timer   *time.Timer
	send    func(tea.Msg)
}

func (p *pacer) setPace(pace replyPace) {
	p.mu.Lock()
	p.pace = pace
	p.mu.Unlock()
	if pace.mode == config.RefreshLive || pace.mode == "" {
		go p.flush()
	}
}

// paced tells the pieces the pace holds back: what the model writes, its
// reasoning and the calls it forms.
func paced(event llm.Event) bool {
	switch event.Kind {
	case llm.EventText, llm.EventReasoning, llm.EventToolCall:
		return true
	}
	return false
}

// event takes a piece of the reply, on the agent's goroutine.
func (p *pacer) event(event llm.Event) {
	p.mu.Lock()
	pace := p.pace
	if pace.mode == config.RefreshLive || pace.mode == "" || !paced(event) {
		p.mu.Unlock()
		p.flush()
		p.sending.Lock()
		p.send(streamMsg(event))
		p.sending.Unlock()
		return
	}
	now := time.Now()
	if len(p.held) == 0 {
		p.since = now
	}
	p.held = append(p.held, timedEvent{event, now})
	due := false
	wait := pace.every
	switch pace.mode {
	case config.RefreshTime:
		due = now.Sub(p.since) >= pace.every
	case config.RefreshTokens:
		due, wait = len(p.held) >= pace.tokens, tokensAtMost
	}
	if !due && p.timer == nil {
		p.timer = time.AfterFunc(wait, p.flush)
	}
	p.mu.Unlock()
	if due {
		p.flush()
	}
}

// flush sends what is held, in one message.
func (p *pacer) flush() {
	p.sending.Lock()
	defer p.sending.Unlock()
	p.mu.Lock()
	held := p.held
	p.held = nil
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	if len(held) > 0 {
		p.send(streamBatchMsg(held))
	}
}
