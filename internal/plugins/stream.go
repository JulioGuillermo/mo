package plugins

import (
	"context"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/llm"
)

// streamGap is the least time between two stream hook calls: what comes
// faster is gathered and given in one call.
const streamGap = 300 * time.Millisecond

// streamFeed gathers the pieces of the reply for the stream hooks and gives
// them at most every streamGap, off the reply's own goroutine, so a slow
// hook never slows the reply: a piece that comes after a quiet gap goes at
// once, the ones that follow it wait for the gap to pass, then go together.
type streamFeed struct {
	mu        sync.Mutex
	text      strings.Builder
	reasoning strings.Builder
	last      time.Time
	waiting   bool
	gap       time.Duration
	send      func(text, reasoning string)
}

func newStreamFeed(gap time.Duration, send func(text, reasoning string)) *streamFeed {
	return &streamFeed{gap: gap, send: send}
}

// add takes a piece; it never waits for a hook.
func (f *streamFeed) add(kind llm.EventKind, piece string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if kind == llm.EventReasoning {
		f.reasoning.WriteString(piece)
	} else {
		f.text.WriteString(piece)
	}
	if f.waiting {
		return
	}
	f.waiting = true
	wait := f.gap - time.Since(f.last)
	if wait < 0 {
		wait = 0
	}
	time.AfterFunc(wait, f.flush)
}

// flush gives what was gathered and starts the next gap.
func (f *streamFeed) flush() {
	f.mu.Lock()
	text, reasoning := f.text.String(), f.reasoning.String()
	f.text.Reset()
	f.reasoning.Reset()
	f.waiting = false
	f.last = time.Now()
	f.mu.Unlock()
	if text != "" || reasoning != "" {
		f.send(text, reasoning)
	}
}

// StreamPiece hands a piece of the reply to the stream hooks, gathered as
// streamFeed says. Without stream hooks it does nothing.
func (s *Set) StreamPiece(kind llm.EventKind, piece string) {
	if s == nil || s.feed == nil {
		return
	}
	s.feed.add(kind, piece)
}

// streamHooks starts the feed when a plugin listens to the stream.
func (s *Set) streamHooks() {
	for _, plugin := range s.loaded() {
		if len(plugin.hooks[hookStream]) > 0 {
			s.feed = newStreamFeed(streamGap, s.tellStream)
			return
		}
	}
}

// tellStream gives what came to every stream hook, in turn; one that fails
// is logged and the others go on.
func (s *Set) tellStream(text, reasoning string) {
	for _, plugin := range s.loaded() {
		for _, hook := range plugin.hooks[hookStream] {
			_, err := plugin.callFunction(context.Background(), hookTimeout, hook, optionalText, func(state *lua.LState) lua.LValue {
				event := state.NewTable()
				event.RawSetString("text", lua.LString(text))
				event.RawSetString("reasoning", lua.LString(reasoning))
				return event
			})
			if err != nil {
				plugin.log("stream hook:", err.Error())
			}
		}
	}
}
