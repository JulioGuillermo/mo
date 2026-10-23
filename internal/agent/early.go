package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jg/mo/internal/llm"
)

// earlyCall is a call of a reply still being written, run as soon as it is
// whole: what it needs from the user is asked at once, and it runs once the
// calls before it are done and the user said yes.
type earlyCall struct {
	call    llm.ToolCall
	pending pendingCall
	asked   bool
	reply   *Reply
	ran     bool
	result  string
	isError bool
	// done is set once it has a result; interrupted when a stop cut it.
	done        bool
	interrupted bool
	// timing is when the user was asked about it and answered, and when
	// it ran.
	timing llm.Timing
}

// earlyPiece is a call as far as the reply has written it.
type earlyPiece struct {
	id, name string
	args     strings.Builder
}

// earlyRunner runs the calls of a reply while it streams. The stream gives
// it each call once whole; one goroutine of its own prepares them, asks
// the user, and runs them in order, one at a time, so the agent's state is
// only touched there.
type earlyRunner struct {
	agent  *Agent
	hooks  Hooks
	ctx    context.Context
	cancel context.CancelFunc

	// Written by the stream's side only.
	pieces   []*earlyPiece
	sent     int
	held     bool
	incoming chan *earlyCall
	closed   bool

	finished chan struct{}
	calls    []*earlyCall
	started  sync.Once
	// beforeStart shows what the reply wrote before its first call runs,
	// so it reads before the call.
	beforeStart func()
}

func (a *Agent) startEarly(ctx context.Context, hooks Hooks, beforeStart func()) *earlyRunner {
	runCtx, cancel := context.WithCancel(ctx)
	r := &earlyRunner{agent: a, hooks: hooks, ctx: runCtx, cancel: cancel,
		incoming: make(chan *earlyCall, 16), finished: make(chan struct{}), beforeStart: beforeStart}
	go r.run()
	return r
}

// piece takes a piece of a call as it streams. A piece of a later call
// means the ones before it are whole, and they are sent to run.
func (r *earlyRunner) piece(event llm.Event) {
	for len(r.pieces) <= event.Call {
		r.pieces = append(r.pieces, &earlyPiece{})
	}
	piece := r.pieces[event.Call]
	if event.ID != "" {
		piece.id = event.ID
	}
	piece.name += event.Name
	piece.args.WriteString(event.Text)
	for r.sent < event.Call && !r.held {
		r.sendPiece(r.sent)
	}
}

// sendPiece sends a whole call to run; one whose arguments are not JSON
// yet holds it and those after it until the reply ends.
func (r *earlyRunner) sendPiece(index int) {
	piece := r.pieces[index]
	args := strings.TrimSpace(piece.args.String())
	if args == "" {
		args = "{}"
	}
	if !json.Valid([]byte(args)) || piece.name == "" {
		r.held = true
		return
	}
	id := piece.id
	if id == "" {
		id = fmt.Sprintf("call_%d", index)
	}
	r.send(llm.ToolCall{ID: id, Name: piece.name, Args: json.RawMessage(args)})
}

func (r *earlyRunner) send(call llm.ToolCall) {
	r.incoming <- &earlyCall{call: call}
	r.sent++
}

// finish sends the calls of the ended reply not sent yet and waits for every
// call to have a result; those sent early take the ids the reply gave.
func (r *earlyRunner) finish(calls []llm.ToolCall) []*earlyCall {
	for i := r.sent; i < len(calls); i++ {
		r.send(calls[i])
	}
	r.stop()
	<-r.finished
	for i, call := range r.calls {
		if i < len(calls) {
			call.call.ID = calls[i].ID
		}
	}
	return r.calls
}

// abort ends a reply that broke off: what was not asked or run yet is
// stopped; the calls already run keep their results.
func (r *earlyRunner) abort() []*earlyCall {
	r.cancel()
	r.stop()
	<-r.finished
	return r.calls
}

func (r *earlyRunner) stop() {
	if !r.closed {
		r.closed = true
		close(r.incoming)
	}
}

// showWritten shows what the reply wrote so far, once: before its first
// call runs or as it ends, whichever comes first, the other waiting.
func (r *earlyRunner) showWritten() {
	r.started.Do(r.beforeStart)
}

// any tells whether a call was sent to run before the reply ended.
func (r *earlyRunner) any() bool {
	return r.sent > 0
}

// run prepares each call as it comes, asks the user what it needs without
// waiting, and runs the calls in order as soon as each may.
func (r *earlyRunner) run() {
	defer close(r.finished)
	defer r.cancel()
	defer func() { r.agent.inReply = nil }()
	type answered struct {
		call  *earlyCall
		reply Reply
	}
	answers := make(chan answered)
	results := make(chan *earlyCall)
	incoming := r.incoming
	next, running, waiting := 0, false, 0
	for {
		for !running && next < len(r.calls) {
			call := r.calls[next]
			if call.asked && call.reply == nil {
				break
			}
			var reply Reply
			if call.reply != nil {
				reply = *call.reply
			}
			call.timing.Start = time.Now()
			result, isError, run := r.agent.decideCall(r.ctx, call.pending, reply, r.hooks)
			if !run {
				call.result, call.isError, call.done = result, isError, true
				r.seen()
				next++
				continue
			}
			r.showWritten()
			if r.hooks.OnToolStart != nil {
				r.hooks.OnToolStart(call.pending.call, call.pending.summary)
			}
			running, call.ran = true, true
			call.timing.Start = time.Now()
			go func(call *earlyCall) {
				call.result, call.isError = r.agent.runTool(r.ctx, call.pending.call)
				call.timing.End = time.Now()
				call.interrupted = r.ctx.Err() != nil
				results <- call
			}(call)
		}
		if incoming == nil && !running && waiting == 0 && next == len(r.calls) {
			return
		}
		select {
		case call, ok := <-incoming:
			if !ok {
				incoming = nil
				continue
			}
			var interaction Interaction
			call.pending = r.agent.prepareCall(r.ctx, call.call, r.hooks, &interaction)
			if len(interaction.Approvals)+len(interaction.Questions) > 0 {
				call.asked = true
				call.timing.Asked = time.Now()
				waiting++
				go func() { answers <- answered{call, r.agent.interact(r.ctx, interaction, r.hooks)} }()
			}
			r.calls = append(r.calls, call)
		case answer := <-answers:
			waiting--
			answer.call.reply = &answer.reply
			answer.call.timing.Answered = time.Now()
		case call := <-results:
			running = false
			call.done = true
			r.seen()
			if r.hooks.OnToolDone != nil {
				r.hooks.OnToolDone(call.pending.call, call.result, call.isError)
			}
			next++
		}
	}
}

// seen lets the loop guard see the calls of the reply that have a result,
// as it sees those of the replies before.
func (r *earlyRunner) seen() {
	var calls []llm.ToolCall
	var results []llm.Message
	for _, call := range r.calls {
		if !call.done {
			break
		}
		calls = append(calls, call.call)
		results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: call.call.ID, Text: call.result, IsError: call.isError})
	}
	r.agent.inReply = append([]llm.Message{{Role: llm.RoleAssistant, ToolCalls: calls}}, results...)
}

// earlyHistory is the reply with the calls that have a result, then the
// results: what was run while it was written. Calls a stop cut short go,
// as if the model had not made them.
func earlyHistory(message llm.Message, calls []*earlyCall) []llm.Message {
	message.ToolCalls = nil
	var results []llm.Message
	for _, call := range calls {
		if !call.done || call.interrupted || (call.result == cancelledResult && !call.ran) {
			continue
		}
		message.ToolCalls = append(message.ToolCalls, call.call)
		timing := call.timing
		results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: call.call.ID, Text: call.result, IsError: call.isError, Timing: timing.Ended()})
	}
	if len(message.ToolCalls) == 0 && strings.TrimSpace(message.Text) == "" {
		return results
	}
	return append([]llm.Message{message}, results...)
}

// startEarlyCalls starts running the calls of the next reply as they are
// written, when the agent is set to and someone can answer what they ask.
func (a *Agent) startEarlyCalls(ctx context.Context, hooks Hooks) *earlyRunner {
	if !a.EarlyCalls || hooks.Interact == nil {
		return nil
	}
	return a.startEarly(ctx, hooks, func() { a.showPartial(hooks) })
}

// answerEarly gives every call of the ended reply its result, in order, as
// the calls run while it was written got them, once they all ran; it
// returns those a stop cut short.
func (a *Agent) answerEarly(ctx context.Context, ran []*earlyCall, calls []llm.ToolCall) map[string]bool {
	unfinished := map[string]bool{}
	for i, call := range calls {
		result, isError := "Not run: the reply changed this call after it was written.", true
		if i < len(ran) {
			result, isError = ran[i].result, ran[i].isError
			if ran[i].interrupted || (ctx.Err() != nil && !ran[i].ran) {
				unfinished[call.ID] = true
			}
		}
		timing := llm.StartedNow()
		if i < len(ran) {
			*timing = ran[i].timing
		}
		a.History = append(a.History, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Text: result, IsError: isError, Timing: timing.Ended()})
	}
	return unfinished
}

// keepEarly puts in the history what a reply that broke off ran while it
// was written, with the text it had, and tells whether it ran anything.
func (a *Agent) keepEarly(early *earlyRunner, stopped bool) bool {
	if early == nil {
		return false
	}
	calls := early.abort()
	if !early.any() {
		return false
	}
	a.partialMu.Lock()
	text := strings.TrimSpace(a.partial.String())
	a.partialMu.Unlock()
	a.resetPartial()
	if stopped && text != "" {
		text += stoppedMark
	}
	a.History = append(a.History, earlyHistory(llm.Message{Role: llm.RoleAssistant, Text: text, Timing: a.stoppedTiming()}, calls)...)
	return true
}

// showPartial shows what the reply wrote so far, before its first call
// runs, and remembers it so the end of the reply shows only the rest.
func (a *Agent) showPartial(hooks Hooks) {
	a.partialMu.Lock()
	a.shownText, a.shownReasoning = a.partial.String(), a.partialReasoning.String()
	message := llm.Message{Role: llm.RoleAssistant, Text: a.shownText, Reasoning: a.shownReasoning}
	a.partialMu.Unlock()
	if hooks.OnAssistant != nil {
		hooks.OnAssistant(message)
	}
}

// unshown is a reply less what was shown of it while it was written.
func (a *Agent) unshown(message llm.Message) llm.Message {
	a.partialMu.Lock()
	defer a.partialMu.Unlock()
	if a.shownText != "" && strings.HasPrefix(message.Text, a.shownText) {
		message.Text = message.Text[len(a.shownText):]
	}
	if a.shownReasoning != "" && strings.HasPrefix(message.Reasoning, a.shownReasoning) {
		message.Reasoning = message.Reasoning[len(a.shownReasoning):]
	}
	return message
}

func (a *Agent) resetPartial() {
	a.partialMu.Lock()
	defer a.partialMu.Unlock()
	a.partial.Reset()
	a.partialReasoning.Reset()
	a.shownText, a.shownReasoning = "", ""
}
