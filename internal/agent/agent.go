// Package agent runs the conversation loop: send the history to the model,
// run the tools it asks for, send the results back, until it answers in text.
package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

const (
	deniedResult    = "The user denied this call. Ask them how to proceed."
	cancelledResult = "Cancelled by the user before it ran."
)

type Decision int

const (
	Deny Decision = iota
	Allow
	AllowAlways
)

// ApprovalRequest is asked for every call to a tool in ConfirmTools and for
// every call with a Warning, such as a change outside the working directory.
// For the latter, AllowAlways counts as Allow, so they are asked every time.
type ApprovalRequest struct {
	Call    llm.ToolCall
	Summary string
	Warning string
	// Reason says why a call that rules check was not let through.
	Reason string
	// Commands are the parts of a shell command or fetch no rule allows,
	// to offer as new rules; Ruled is set for every call rules check.
	Commands []string
	Ruled    bool
}

type Agent struct {
	Provider  llm.Provider
	Model     string
	System    string
	MaxTokens int
	// Effort is how hard a model that reasons thinks in the turns; empty
	// is its default. Compaction and the auto-mode judge keep the default.
	Effort       string
	Tools        *tools.Registry
	ConfirmTools []string

	Window        int
	CompactAt     float64
	CompactPrompt string
	Strategy      string

	// KeepReasoning sends the model its own reasoning back with the
	// conversation; without it the reasoning is only shown, saving tokens.
	KeepReasoning bool

	Loop LoopGuard
	// inReply is the reply being written with the calls it ran so far and
	// their results, for the loop guard to see while it is not in the
	// history yet; only the early calls' goroutine touches it.
	inReply []llm.Message
	// EarlyCalls runs each call of a reply as soon as it is written, asking
	// what it needs while the reply goes on, instead of once it ends.
	EarlyCalls bool
	// measuredBytesPerToken is what the provider's last count said a token
	// takes, for the estimates; 0 before any.
	measuredBytesPerToken float64
	// MaxSteps is how many replies a run may ask the model for; 0 for no
	// limit. Subagents have one.
	MaxSteps int

	// Permissions is the mode and rules in force; without it the calls in
	// ConfirmTools and those with a warning ask.
	Permissions *permissions.Policy

	RewritePrompt func(ctx context.Context, prompt string) string
	BlockToolCall func(ctx context.Context, call llm.ToolCall) string

	History   []llm.Message
	LastUsage llm.Usage

	historyAtUsage      int
	sizeAfterCompaction int
	currentRequest      string

	loops int

	alwaysAllowed map[string]bool
	queued        queue
	told          notes
	// partial is the answer being streamed, kept when the user stops it,
	// with its reasoning; shown is what of them was shown before a call
	// run while the reply was written. The stream writes them while a
	// call runs, hence the lock.
	partialMu        sync.Mutex
	partial          strings.Builder
	partialReasoning strings.Builder
	shownText        string
	shownReasoning   string
	judgedSafe       map[string]bool
}

// Hooks lets the caller follow a run. Every field is optional. Without
// Approve, every call that needs confirmation is denied.
type Hooks struct {
	OnEvent     func(llm.Event)
	OnAssistant func(llm.Message)
	OnToolStart func(call llm.ToolCall, summary string)
	OnToolDone  func(call llm.ToolCall, output string, isError bool)
	OnUsage     func(llm.Usage)
	OnCompact   func(before, after int)
	// OnActivity says what the agent waits on besides the reply, to show
	// in place of a plain "working": compacting, judging a command. An
	// empty text says it is back to the reply.
	OnActivity func(text string)
	OnNotice   func(text string)
	// OnQueued says the model now reads that many of the messages queued
	// while it worked, the oldest first.
	OnQueued func(count int)
	// OnStep gives a copy of the history after each reply and each round
	// of results, to save as the turn goes.
	OnStep    func(history []llm.Message)
	OnLoopCut func(repeated string)
	Approve   func(ctx context.Context, request ApprovalRequest) Decision
	// Interact asks the user everything the calls of a reply need at once;
	// it takes the place of Approve.
	Interact func(ctx context.Context, interaction Interaction) Reply
}

func (a *Agent) Run(ctx context.Context, prompt string, hooks Hooks) error {
	a.settleUnanswered()
	if a.RewritePrompt != nil {
		prompt = a.RewritePrompt(ctx, prompt)
	}
	a.History = append(a.History, a.promptMessage(prompt, hooks))
	a.currentRequest = prompt
	a.loops = 0
	return a.turn(ctx, hooks)
}

// turn asks the model and answers its tool calls until it ends.
func (a *Agent) turn(ctx context.Context, hooks Hooks) error {
	limit := a.MaxSteps
	if limit == 0 && a.Loop.Enabled {
		limit = a.Loop.Steps
	}
	for steps := 0; ; steps++ {
		if limit > 0 && steps >= limit {
			return ErrStepLimit
		}
		a.tellEndedJobs()
		a.tellNotes()
		a.deliverQueued(hooks)
		if a.needsCompaction() {
			a.makeRoom(ctx, hooks)
		}
		early := a.startEarlyCalls(ctx, hooks)
		response, repeated, err := a.stream(ctx, hooks, early)
		if err != nil && ctx.Err() == nil && (early == nil || !early.any()) && a.withoutImages() {
			if hooks.OnNotice != nil {
				hooks.OnNotice(fmt.Sprintf("the model did not take the images (%v); sending without them", err))
			}
			if early != nil {
				early.abort()
				early = a.startEarlyCalls(ctx, hooks)
			}
			response, repeated, err = a.stream(ctx, hooks, early)
		}
		if err != nil {
			stopped := ctx.Err() != nil
			if !a.keepEarly(early, stopped) && stopped {
				a.keepPartial()
			}
			if stopped {
				return ctx.Err()
			}
			return err
		}
		if repeated != "" {
			a.keepEarly(early, false)
			if err := a.cutLoopingReply(repeated, hooks); err != nil {
				return err
			}
			continue
		}
		// The early calls' goroutine reads the history: it is done before
		// the history changes.
		var ran []*earlyCall
		if early != nil {
			// What was written shows once, before any call runs, whether
			// the first call or the end of the reply comes first; the end
			// shows only the rest, before the calls left are waited for.
			if len(response.Message.ToolCalls) > 0 {
				early.showWritten()
			}
			if hooks.OnAssistant != nil {
				hooks.OnAssistant(a.unshown(response.Message))
			}
			ran = early.finish(response.Message.ToolCalls)
		}
		a.History = append(a.History, response.Message)
		a.LastUsage = response.Usage
		a.historyAtUsage = len(a.History)
		a.measure(response.Usage)
		if hooks.OnUsage != nil {
			hooks.OnUsage(response.Usage)
		}
		a.step(hooks)
		if early == nil && hooks.OnAssistant != nil {
			hooks.OnAssistant(response.Message)
		}
		if len(response.Message.ToolCalls) == 0 {
			return nil
		}
		asked := len(a.History) - 1
		var unfinished map[string]bool
		if early != nil {
			unfinished = a.answerEarly(ctx, ran, response.Message.ToolCalls)
		} else {
			unfinished = a.answerToolCalls(ctx, response.Message.ToolCalls, hooks)
		}
		if ctx.Err() != nil {
			a.dropUnfinished(asked, unfinished)
			return ctx.Err()
		}
		results := a.History[len(a.History)-len(response.Message.ToolCalls):]
		if note, found := toolImages(response.Message.ToolCalls, results); found {
			a.History = append(a.History, note)
		}
		a.step(hooks)
		if a.looping() {
			return ErrLooping
		}
	}
}

// step gives the history as it is now to OnStep.
func (a *Agent) step(hooks Hooks) {
	if hooks.OnStep != nil {
		hooks.OnStep(slices.Clone(a.History))
	}
}

// stream sends the request and, with the loop guard on, cuts the reply as
// soon as it repeats itself, returning the repeated piece instead of it.
func (a *Agent) stream(ctx context.Context, hooks Hooks, early *earlyRunner) (llm.Response, string, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var watch *repeatWatch
	if a.Loop.Enabled {
		watch = newRepeatWatch(a.Loop.StreamRepeats)
	}
	repeated := ""
	a.resetPartial()
	response, err := a.Provider.Stream(streamCtx, a.request(), func(event llm.Event) {
		if repeated != "" {
			return
		}
		switch event.Kind {
		case llm.EventText, llm.EventReasoning:
			a.partialMu.Lock()
			if event.Kind == llm.EventText {
				a.partial.WriteString(event.Text)
			} else {
				a.partialReasoning.WriteString(event.Text)
			}
			a.partialMu.Unlock()
		case llm.EventToolCall:
			if early != nil {
				early.piece(event)
			}
		}
		if hooks.OnEvent != nil {
			hooks.OnEvent(event)
		}
		if watch != nil {
			repeated = watch.add(event)
		}
		if repeated != "" {
			cancel()
		}
	})
	if repeated == "" {
		return response, "", err
	}
	if early == nil || !early.any() {
		a.resetPartial()
	}
	if ctx.Err() != nil {
		return llm.Response{}, "", ctx.Err()
	}
	return llm.Response{}, repeated, nil
}

// answerToolCalls gives every call a result, also the ones skipped after a
// cancel.
func (a *Agent) announceBlocked(call llm.ToolCall, reason string, hooks Hooks) {
	if hooks.OnToolStart != nil {
		hooks.OnToolStart(call, a.Tools.Summary(call))
	}
	if hooks.OnToolDone != nil {
		hooks.OnToolDone(call, reason, true)
	}
}

func (a *Agent) request() llm.Request {
	return llm.Request{
		Model:     a.Model,
		System:    a.System,
		Messages:  a.sentHistory(),
		Tools:     a.Tools.Specs(a.History),
		MaxTokens: a.MaxTokens,
		Effort:    a.Effort,
	}
}

// sentHistory is the history as the model gets it: without reasoning,
// unless it is kept.
func (a *Agent) sentHistory() []llm.Message {
	if a.KeepReasoning || !slices.ContainsFunc(a.History, func(message llm.Message) bool { return message.Reasoning != "" }) {
		return a.History
	}
	sent := slices.Clone(a.History)
	for i := range sent {
		sent[i].Reasoning = ""
	}
	return sent
}

// TakeConversation moves the conversation of another agent into this one,
// to keep it when the session is rebuilt with new settings. Approvals given
// with "always" are not taken: the new settings decide what asks.
func (a *Agent) TakeConversation(from *Agent) {
	a.History = from.History
	a.LastUsage = from.LastUsage
	a.historyAtUsage = from.historyAtUsage
	a.sizeAfterCompaction = from.sizeAfterCompaction
	a.currentRequest = from.currentRequest
}

// makeRoom shrinks the context before a request, by dropping the oldest
// turns or by compacting. When compacting fails the turn goes on as it is,
// and no new attempt is made until the conversation grows.
func (a *Agent) makeRoom(ctx context.Context, hooks Hooks) {
	if a.Strategy == StrategyDrop {
		a.dropOldest(hooks)
		return
	}
	err := a.Compact(ctx, hooks)
	if err == nil || ctx.Err() != nil {
		return
	}
	a.sizeAfterCompaction = a.ContextUsed()
	if hooks.OnNotice != nil {
		hooks.OnNotice(fmt.Sprintf("could not compact the context (%v); going on without it", err))
	}
}

func (a *Agent) Clear() {
	a.History = nil
	a.LastUsage = llm.Usage{}
	a.historyAtUsage = 0
	a.sizeAfterCompaction = 0
	a.currentRequest = ""
}

// activity tells the caller what the agent waits on, when it listens.
func (h Hooks) activity(text string) {
	if h.OnActivity != nil {
		h.OnActivity(text)
	}
}
