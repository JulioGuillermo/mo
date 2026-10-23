package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/jg/mo/internal/llm"
)

// queue holds what the user wrote while the agent worked, for its next
// step. The UI adds to it while the agent's goroutine takes from it.
type queue struct {
	mu       sync.Mutex
	messages []Queued
}

// Queued is a message waiting for the model: Text as it goes to it, and
// Typed as the user wrote it.
type Queued struct {
	Text, Typed string
	// At is when the user sent it; now when not set.
	At time.Time
}

// Queue keeps a message the user wrote while the agent works; the model
// reads it at its next step.
func (a *Agent) Queue(text string) {
	a.QueueTyped(Queued{Text: text, Typed: text})
}

// QueueTyped is Queue with what the user typed, kept with the message when
// what reaches the model differs from it.
func (a *Agent) QueueTyped(message Queued) {
	if message.At.IsZero() {
		message.At = time.Now()
	}
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	a.queued.messages = append(a.queued.messages, message)
}

// TakeQueued returns the messages not read yet and forgets them, as when
// the user stops the agent and gets them back.
func (a *Agent) TakeQueued() []Queued {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	messages := a.queued.messages
	a.queued.messages = nil
	return messages
}

// QueuedCount is how many messages wait for the model.
func (a *Agent) QueuedCount() int {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	return len(a.queued.messages)
}

// HasQueued tells whether a message waits for the model.
func (a *Agent) HasQueued() bool {
	a.queued.mu.Lock()
	defer a.queued.mu.Unlock()
	return len(a.queued.messages) > 0
}

// deliverQueued puts the waiting messages in the conversation as one message
// of the user's, a blank line between them, with what they attach, before the
// model's next step: some chat templates refuse two user messages in a row.
// It goes on the last message when that is the user's: after a prompt, at
// its end; before a note of mo's, so the message still begins with what the
// user wrote and is taken for the prompt.
func (a *Agent) deliverQueued(hooks Hooks) {
	queued := a.TakeQueued()
	if len(queued) == 0 {
		return
	}
	var texts, own, typed []string
	for _, each := range queued {
		texts = append(texts, each.Text)
		own = append(own, ownText(each.Text))
		typed = append(typed, each.Typed)
	}
	joined := strings.Join(texts, "\n\n")
	joinedTyped := strings.Join(typed, "\n\n")
	message := withInput(a.promptMessageNaming(joined, strings.Join(own, "\n\n"), hooks), joinedTyped)
	message.Timing = (&llm.Timing{Start: queued[0].At}).Ended()
	if last := len(a.History) - 1; last >= 0 && a.History[last].Role == llm.RoleUser {
		previous := a.History[last]
		merged := previous
		if isPrompt(previous) {
			merged.Text = previous.Text + "\n\n" + message.Text
			merged = withInput(merged, PromptText(previous)+"\n\n"+joinedTyped)
		} else {
			merged.Text = message.Text + "\n\n" + previous.Text
			merged = withInput(merged, joinedTyped)
		}
		merged.Images = append(merged.Images, message.Images...)
		if merged.Timing == nil {
			merged.Timing = message.Timing
		}
		a.History[last] = merged
	} else {
		a.History = append(a.History, message)
	}
	a.currentRequest = joined
	if hooks.OnQueued != nil {
		hooks.OnQueued(len(queued))
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
