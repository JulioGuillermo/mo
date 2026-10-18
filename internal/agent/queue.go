package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/jg/mo/internal/llm"
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

// deliverQueued puts the waiting messages in the conversation as one message
// of the user's, a blank line between them, with what they attach, before the
// model's next step: some chat templates refuse two user messages in a row.
// It goes on the last message when that is the user's: after a prompt, at
// its end; before a note of mo's, so the message still begins with what the
// user wrote and is taken for the prompt.
func (a *Agent) deliverQueued(hooks Hooks) {
	texts := a.TakeQueued()
	if len(texts) == 0 {
		return
	}
	joined := strings.Join(texts, "\n\n")
	message := a.promptMessage(joined, hooks)
	if last := len(a.History) - 1; last >= 0 && a.History[last].Role == llm.RoleUser {
		if isPrompt(a.History[last]) {
			a.History[last].Text += "\n\n" + message.Text
		} else {
			a.History[last].Text = message.Text + "\n\n" + a.History[last].Text
		}
		a.History[last].Images = append(a.History[last].Images, message.Images...)
	} else {
		a.History = append(a.History, message)
	}
	a.currentRequest = joined
	if hooks.OnQueued != nil {
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
