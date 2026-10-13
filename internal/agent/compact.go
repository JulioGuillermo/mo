package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const (
	// CompactingActivity is what OnActivity says while a summary is asked.
	CompactingActivity   = "compacting the conversation"
	DefaultCompactPrompt = `You summarize a coding session so the work can continue from the summary alone.
Keep the user's goals and constraints, decisions taken, files changed and their state, commands that worked or failed, and what is left to do.
Use short bullet points.`
	summaryHeader        = "Summary of the conversation so far:\n"
	continueWork         = "Continue from where the work stopped. Do not redo the steps the summary says are done."
	currentRequestHeader = "The request being worked on:\n"
	writeSummaryNow      = "Summarize the session above."

	keptTurnShare         = 0.3
	transcriptShare       = 0.6
	recompactGrowthShare  = 0.1
	transcriptResultBytes = 2000
	transcriptArgsBytes   = 500
	bytesPerTokenGuess    = 4
)

// needsCompaction is true past compact_at, unless the conversation has barely
// grown since the last compaction, which could not get it any smaller. A
// full window needs it unless the last one could not get below the window.
func (a *Agent) needsCompaction() bool {
	if a.Window <= 0 || a.CompactAt <= 0 {
		return false
	}
	used := a.ContextUsed()
	if used >= a.Window {
		return a.sizeAfterCompaction < a.Window
	}
	if used < int(float64(a.Window)*a.CompactAt) {
		return false
	}
	return used > a.sizeAfterCompaction+int(float64(a.Window)*recompactGrowthShare)
}

// ContextUsed estimates the size of the next request: the provider's count
// at the last response plus a guess for the messages added since. Without a
// count from the provider it is all a guess.
func (a *Agent) ContextUsed() int {
	if a.LastUsage == (llm.Usage{}) {
		return a.fixedPartTokens() + estimateTokens(a.History)
	}
	counted := a.LastUsage.InputTokens + a.LastUsage.OutputTokens
	return counted + estimateTokens(a.History[a.historyAtUsage:])
}

func (a *Agent) fixedPartTokens() int {
	size := len(a.System)
	for _, spec := range a.Tools.Specs(a.History) {
		size += len(spec.Name) + len(spec.Description) + len(spec.Schema)
	}
	return size / bytesPerTokenGuess
}

// Compact replaces the history with a summary written by the model. The turn
// in progress, from the last user message on, is kept as it is when it takes
// less than keptTurnShare of the window.
func (a *Agent) Compact(ctx context.Context, hooks Hooks) error {
	if len(a.History) == 0 {
		return nil
	}
	cut := a.lastUserMessage()
	kept := a.History[cut:]
	if cut == 0 || estimateTokens(kept) > int(float64(a.Window)*keptTurnShare) {
		cut = len(a.History)
		kept = nil
	}

	hooks.activity(CompactingActivity)
	summary, err := a.summarize(ctx, a.History[:cut])
	hooks.activity("")
	if err != nil {
		return err
	}
	before := a.ContextUsed()
	a.History = withSummary(summary, kept, a.currentRequest)
	a.LastUsage = llm.Usage{}
	a.historyAtUsage = 0
	a.sizeAfterCompaction = a.ContextUsed()
	if hooks.OnCompact != nil {
		hooks.OnCompact(before, a.sizeAfterCompaction)
	}
	return nil
}

// summarize sends the history as a plain-text transcript, without tools, so
// the model can only answer in text and the request fits in the window.
func (a *Agent) summarize(ctx context.Context, history []llm.Message) (string, error) {
	prompt := a.CompactPrompt
	if prompt == "" {
		prompt = DefaultCompactPrompt
	}
	transcript := transcriptOf(history)
	if a.Window > 0 {
		transcript = tools.Truncate(transcript, int(float64(a.Window)*transcriptShare)*bytesPerTokenGuess)
	}
	request := llm.Request{
		Model:     a.Model,
		System:    prompt,
		Messages:  []llm.Message{{Role: llm.RoleUser, Text: transcript + "\n\n" + writeSummaryNow}},
		MaxTokens: a.MaxTokens,
	}
	response, err := a.Provider.Stream(ctx, request, func(llm.Event) {})
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(response.Message.Text)
	if summary == "" {
		return "", errors.New("the model returned an empty summary")
	}
	return summary, nil
}

func transcriptOf(history []llm.Message) string {
	var out strings.Builder
	for _, message := range history {
		switch message.Role {
		case llm.RoleUser:
			fmt.Fprintf(&out, "User: %s\n\n", message.Text)
		case llm.RoleAssistant:
			if message.Text != "" {
				fmt.Fprintf(&out, "Assistant: %s\n\n", message.Text)
			}
			for _, call := range message.ToolCalls {
				fmt.Fprintf(&out, "Assistant called %s %s\n\n", call.Name, tools.Truncate(string(call.Args), transcriptArgsBytes))
			}
		case llm.RoleTool:
			fmt.Fprintf(&out, "Result: %s\n\n", tools.Truncate(message.Text, transcriptResultBytes))
		}
	}
	return out.String()
}

// withSummary starts the new history with the summary. It goes into the kept
// user message, or into a new one that repeats the user's current request,
// so no two user messages follow each other.
func withSummary(summary string, kept []llm.Message, request string) []llm.Message {
	header := summaryHeader + summary + "\n\n"
	if len(kept) == 0 {
		text := header + currentRequestHeader + request + "\n\n" + continueWork
		return []llm.Message{{Role: llm.RoleUser, Text: text}}
	}
	history := slices.Clone(kept)
	history[0].Text = header + history[0].Text
	return history
}

func (a *Agent) lastUserMessage() int {
	for i := len(a.History) - 1; i >= 0; i-- {
		if isPrompt(a.History[i]) {
			return i
		}
	}
	return 0
}

func estimateTokens(messages []llm.Message) int {
	size := 0
	for _, message := range messages {
		size += len(message.Text)
		for _, call := range message.ToolCalls {
			size += len(call.Name) + len(call.Args)
		}
	}
	return size / bytesPerTokenGuess
}
