package agent

import (
	"context"
	"sync"
)

// queue holds what the user wrote while the agent worked, for its next
// step. The UI adds to it while the agent's goroutine takes from it.
type queue struct {
	mu    sync.Mutex
	texts []string
}

// Queue keeps a message the user wrote while the agent works; the model
// reads it at its next step.
func (a *Agent) Queue(text string) {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	a.queued.texts = append(a.queued.texts, text)
}

// TakeQueued returns the messages not read yet and forgets them, as when
// the user stops the agent and gets them back.
func (a *Agent) TakeQueued() []string {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	texts := a.queued.texts
	a.queued.texts = nil
	return texts
}

// QueuedCount is how many messages wait for the model.
func (a *Agent) QueuedCount() int {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	return len(a.queued.texts)
}

// HasQueued tells whether a message waits for the model.
func (a *Agent) HasQueued() bool {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	return len(a.queued.texts) > 0
}

// deliverQueued puts the waiting messages in the conversation as the
// user's, with what they attach, before the model's next step.
func (a *Agent) deliverQueued(hooks Hooks) {
	texts := a.TakeQueued()
	for _, text := range texts {
		a.History = append(a.History, a.promptMessage(text, hooks))
		a.currentRequest = text
	}
	if len(texts) > 0 && hooks.OnQueued != nil {
		hooks.OnQueued(len(texts))
	}
}

// RunQueued answers messages written while the last turn ran, when the
// turn ended before reading them.
func (a *Agent) RunQueued(ctx context.Context, hooks Hooks) error {
	if !a.HasQueued() {
		return nil
	}
	a.settleUnanswered()
	a.loops = 0
	return a.turn(ctx, hooks)
}
