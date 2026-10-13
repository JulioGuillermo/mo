// Package agent runs the conversation loop: send the history to the model,
// run the tools it asks for, send the results back, until it answers in text.
package agent

import (
	"context"
	"fmt"
	"slices"

	"github.com/jg/mo/internal/llm"
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

	History   []llm.Message
	LastUsage llm.Usage

	historyAtUsage      int
	sizeAfterCompaction int
	currentRequest      string

	alwaysAllowed map[string]bool
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
	OnNotice    func(text string)
	Approve     func(ctx context.Context, request ApprovalRequest) Decision
}

func (a *Agent) Run(ctx context.Context, prompt string, hooks Hooks) error {
	a.History = append(a.History, llm.Message{Role: llm.RoleUser, Text: prompt})
	a.currentRequest = prompt
	for {
		if a.needsCompaction() {
			a.compactOrGoOn(ctx, hooks)
		}
		response, err := a.Provider.Stream(ctx, a.request(), func(event llm.Event) {
			if hooks.OnEvent != nil {
				hooks.OnEvent(event)
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
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
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// answerToolCalls gives every call a result, also the ones skipped after a
// cancel.
func (a *Agent) answerToolCalls(ctx context.Context, calls []llm.ToolCall, hooks Hooks) {
	for _, call := range calls {
		output, isError := a.answerToolCall(ctx, call, hooks)
		a.History = append(a.History, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: call.ID,
			Text:       output,
			IsError:    isError,
		})
	}
}

func (a *Agent) answerToolCall(ctx context.Context, call llm.ToolCall, hooks Hooks) (string, bool) {
	if ctx.Err() != nil {
		return cancelledResult, true
	}
	if call.ArgsError != "" {
		return call.ArgsError, true
	}
	summary := a.Tools.Summary(call)
	if !a.approved(ctx, call, summary, hooks) {
		if ctx.Err() != nil {
			return cancelledResult, true
		}
		return deniedResult, true
	}
	if hooks.OnToolStart != nil {
		hooks.OnToolStart(call, summary)
	}
	output, isError := a.Tools.Run(ctx, call)
	if hooks.OnToolDone != nil {
		hooks.OnToolDone(call, output, isError)
	}
	return output, isError
}

func (a *Agent) approved(ctx context.Context, call llm.ToolCall, summary string, hooks Hooks) bool {
	warning := a.Tools.Warning(call)
	listed := slices.Contains(a.ConfirmTools, call.Name) && !a.alwaysAllowed[call.Name]
	if warning == "" && !listed {
		return true
	}
	if hooks.Approve == nil {
		return false
	}
	decision := hooks.Approve(ctx, ApprovalRequest{Call: call, Summary: summary, Warning: warning})
	if decision == AllowAlways && warning == "" {
		if a.alwaysAllowed == nil {
			a.alwaysAllowed = map[string]bool{}
		}
		a.alwaysAllowed[call.Name] = true
	}
	return decision != Deny
}

func (a *Agent) request() llm.Request {
	return llm.Request{
		Model:     a.Model,
		System:    a.System,
		Messages:  a.History,
		Tools:     a.Tools.Specs(),
		MaxTokens: a.MaxTokens,
	}
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

// compactOrGoOn compacts before a request. When that fails the turn goes on
// uncompacted, and no new attempt is made until the conversation grows.
func (a *Agent) compactOrGoOn(ctx context.Context, hooks Hooks) {
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
