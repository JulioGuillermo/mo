package agent

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
)

const (
	stoppedMark  = "\n\n[stopped by the user here]"
	continueNote = moNotePrefix + "The user stopped you and now wants you to go on from where you left off."
)

// ErrNothingToContinue is returned by Continue on an empty conversation.
var ErrNothingToContinue = errors.New("nothing to continue")

// keepPartial keeps the answer the user stopped mid-way in the history,
// marked as cut, so the model knows what it had said.
func (a *Agent) keepPartial() {
	a.partialMu.Lock()
	text := strings.TrimSpace(a.partial.String())
	a.partialMu.Unlock()
	a.resetPartial()
	if text == "" {
		return
	}
	a.History = append(a.History, llm.Message{Role: llm.RoleAssistant, Text: text + stoppedMark})
}

// dropUnfinished takes out of the history the tool calls a stop cut short,
// not run or stopped running, with their results, as if the model had not
// made them: the reply at asked keeps its text and the calls that ran.
// A reply left with nothing goes too.
func (a *Agent) dropUnfinished(asked int, unfinished map[string]bool) {
	if len(unfinished) == 0 || asked < 0 || asked >= len(a.History) {
		return
	}
	reply := a.History[asked]
	var kept []llm.ToolCall
	for _, call := range reply.ToolCalls {
		if !unfinished[call.ID] {
			kept = append(kept, call)
		}
	}
	reply.ToolCalls = kept
	history := slices.Clone(a.History[:asked])
	if len(kept) > 0 || strings.TrimSpace(reply.Text) != "" {
		history = append(history, reply)
	}
	for _, message := range a.History[asked+1:] {
		if message.Role == llm.RoleTool && unfinished[message.ToolCallID] {
			continue
		}
		history = append(history, message)
	}
	a.History = history
}

// Continue goes on with a turn the user stopped, without a new prompt.
// Stopped before any answer, the model answers the prompt waiting; stopped
// later, it is told to go on from where it was.
func (a *Agent) Continue(ctx context.Context, hooks Hooks) error {
	if len(a.History) == 0 {
		return ErrNothingToContinue
	}
	last := len(a.History) - 1
	switch message := a.History[last]; {
	case message.Role == llm.RoleUser && isPrompt(message):
	case message.Role != llm.RoleAssistant:
		a.History[last].Text += "\n\n" + continueNote
	default:
		a.History = append(a.History, llm.Message{Role: llm.RoleUser, Text: continueNote})
	}
	a.loops = 0
	return a.turn(ctx, hooks)
}
