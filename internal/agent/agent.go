// Package agent runs the conversation loop: send the history to the model,
// run the tools it asks for, send the results back, until it answers in text.
package agent

import (
	"context"
	"fmt"
	"slices"

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
	Provider     llm.Provider
	Model        string
	System       string
	MaxTokens    int
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
	judgedSafe    map[string]bool
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
	OnLoopCut  func(repeated string)
	Approve    func(ctx context.Context, request ApprovalRequest) Decision
	// Interact asks the user everything the calls of a reply need at once;
	// it takes the place of Approve.
	Interact func(ctx context.Context, interaction Interaction) Reply
}

func (a *Agent) Run(ctx context.Context, prompt string, hooks Hooks) error {
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
	for {
		a.tellEndedJobs()
		if a.needsCompaction() {
			a.makeRoom(ctx, hooks)
		}
		response, repeated, err := a.stream(ctx, hooks)
		if err != nil && ctx.Err() == nil && a.withoutImages() {
			if hooks.OnNotice != nil {
				hooks.OnNotice(fmt.Sprintf("the model did not take the images (%v); sending without them", err))
			}
			response, repeated, err = a.stream(ctx, hooks)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if repeated != "" {
			if err := a.cutLoopingReply(repeated, hooks); err != nil {
				return err
			}
			continue
		}
		a.History = append(a.History, response.Message)
		a.LastUsage = response.Usage
		a.historyAtUsage = len(a.History)
		if hooks.OnUsage != nil {
			hooks.OnUsage(response.Usage)
		}
		if hooks.OnAssistant != nil {
			hooks.OnAssistant(response.Message)
		}
		if len(response.Message.ToolCalls) == 0 {
			return nil
		}
		a.answerToolCalls(ctx, response.Message.ToolCalls, hooks)
		results := a.History[len(a.History)-len(response.Message.ToolCalls):]
		if note, found := toolImages(response.Message.ToolCalls, results); found {
			a.History = append(a.History, note)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if a.looping() {
			return ErrLooping
		}
	}
}

// stream sends the request and, with the loop guard on, cuts the reply as
// soon as it repeats itself, returning the repeated piece instead of it.
func (a *Agent) stream(ctx context.Context, hooks Hooks) (llm.Response, string, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var watch *repeatWatch
	if a.Loop.Enabled {
		watch = newRepeatWatch(a.Loop.StreamRepeats)
	}
	repeated := ""
	response, err := a.Provider.Stream(streamCtx, a.request(), func(event llm.Event) {
		if repeated != "" {
			return
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
