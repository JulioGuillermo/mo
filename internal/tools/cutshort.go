package tools

import (
	"context"
	"sync/atomic"
)

type cutShortKey struct{}

// WithCutShortMark lets a call say that a stop cut its work short though it
// ended, as subagents that stopped when asked: its result is not the whole
// of it, and it is kept to be resumed.
func WithCutShortMark(ctx context.Context) (context.Context, *atomic.Bool) {
	mark := &atomic.Bool{}
	return context.WithValue(ctx, cutShortKey{}, mark), mark
}

// MarkCutShort says that a stop cut the call's work short.
func MarkCutShort(ctx context.Context) {
	if mark, ok := ctx.Value(cutShortKey{}).(*atomic.Bool); ok {
		mark.Store(true)
	}
}
