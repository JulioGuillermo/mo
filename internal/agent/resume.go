package agent

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
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
	a.History = append(a.History, llm.Message{Role: llm.RoleAssistant, Text: text + stoppedMark, Timing: a.stoppedTiming()})
}

// dropUnfinished takes out of the history the tool calls a stop cut short,
// not run or stopped running, with their results, as if the model had not
// made them: the reply at asked keeps its text and the calls that ran.
// A reply left with nothing goes too. A resumable call stays, without its
// result, for Continue to run again as resumed.
func (a *Agent) dropUnfinished(asked int, unfinished map[string]bool) {
	if len(unfinished) == 0 || asked < 0 || asked >= len(a.History) {
		return
	}
	reply := a.History[asked]
	var kept []llm.ToolCall
	for _, call := range reply.ToolCalls {
		if !unfinished[call.ID] || a.Tools.Resumable(call.Name) {
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

// stoppedCall answers a resumable call a stop left when the conversation
// goes on otherwise than by continuing it.
const stoppedCall = "Stopped by the user before it ended; what it started was left as it stood."

// interruptedCall answers a call that was not resumable and had no result,
// as when mo stopped between a reply and its calls' results.
const interruptedCall = "Interrupted: mo stopped before this call ended. It was not run again."

// unanswered are the calls of the last reply with no result: resumable
// ones a stop left to go on with.
func (a *Agent) unanswered() []llm.ToolCall {
	last := -1
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].Role == llm.RoleAssistant {
			last = i
			break
		}
	}
	if last < 0 {
		return nil
	}
	answered := map[string]bool{}
	for _, message := range a.History[last+1:] {
		if message.Role == llm.RoleTool {
			answered[message.ToolCallID] = true
		}
	}
	var calls []llm.ToolCall
	for _, call := range a.History[last].ToolCalls {
		if !answered[call.ID] {
			calls = append(calls, call)
		}
	}
	return calls
}

// settleUnanswered answers the calls a stop left, when the conversation
// goes on otherwise than by continuing them.
func (a *Agent) settleUnanswered() {
	for _, call := range a.unanswered() {
		a.History = append(a.History, stamped(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Text: stoppedCall, IsError: true}))
	}
}

// stoppedTiming is the timing of the reply streamed until the user stopped
// it, ended now.
func (a *Agent) stoppedTiming() *llm.Timing {
	timing := a.streamTiming
	a.streamTiming = nil
	if timing == nil {
		return llm.StartedNow().Ended()
	}
	return timing.Ended()
}

// Continue goes on with a turn the user stopped, without a new prompt.
// Stopped before any answer, the model answers the prompt waiting; stopped
// later, it is told to go on from where it was. Calls a stop left are run
// again as resumed, going on with what they started, and the turn goes on
// after them as if it had not stopped.
func (a *Agent) Continue(ctx context.Context, hooks Hooks) error {
	if len(a.History) == 0 {
		return ErrNothingToContinue
	}
	if calls := a.unanswered(); len(calls) > 0 {
		for _, call := range calls {
			if !a.Tools.Resumable(call.Name) {
				// Left by mo stopping mid-call: never run again unasked.
				a.History = append(a.History, stamped(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Text: interruptedCall, IsError: true}))
				continue
			}
			summary := a.Tools.Summary(call)
			if hooks.OnToolStart != nil {
				hooks.OnToolStart(call, summary)
			}
			timing := llm.StartedNow()
			output, isError := a.runTool(tools.WithResume(ctx), call)
			timing.Ended()
			if hooks.OnToolDone != nil {
				hooks.OnToolDone(call, output, isError)
			}
			if ctx.Err() != nil {
				// Stopped again: still to go on with.
				return ctx.Err()
			}
			a.History = append(a.History, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Text: output, IsError: isError, Timing: timing})
			a.step(hooks)
		}
		a.loops = 0
		return a.turn(ctx, hooks)
	}
	last := len(a.History) - 1
	switch message := a.History[last]; {
	case message.Role == llm.RoleUser && isPrompt(message):
	case message.Role != llm.RoleAssistant:
		a.History[last].Text += "\n\n" + continueNote
	default:
		a.History = append(a.History, stamped(llm.Message{Role: llm.RoleUser, Text: continueNote}))
	}
	a.loops = 0
	return a.turn(ctx, hooks)
}

// Stopped tells a call's result that says it was stopped, cancelled or
// denied rather than that it failed.
func Stopped(result string) bool {
	switch result {
	case deniedResult, cancelledResult, stoppedCallResult, stoppedCall, interruptedCall:
		return true
	}
	return false
}
