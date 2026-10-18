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
	// A measure outside these is taken for a count that was not of the
	// whole request, as a server's that counts only what was not cached.
	minBytesPerToken = 1.0
	maxBytesPerToken = 8.0
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
		return a.fixedPartTokens() + a.estimate(a.History)
	}
	counted := a.LastUsage.InputTokens + a.LastUsage.OutputTokens
	return counted + a.estimate(a.History[a.historyAtUsage:])
}

func (a *Agent) fixedPartTokens() int {
	return a.tokensOf(a.fixedPartBytes())
}

func (a *Agent) fixedPartBytes() int {
	size := len(a.System)
	for _, spec := range a.Tools.Specs(a.History) {
		size += len(spec.Name) + len(spec.Description) + len(spec.Schema)
	}
	return size
}

// bytesPerToken is how many bytes the model's tokens take, as its last
// count said, or the usual guess before any.
func (a *Agent) bytesPerToken() float64 {
	if a.measuredBytesPerToken > 0 {
		return a.measuredBytesPerToken
	}
	return bytesPerTokenGuess
}

func (a *Agent) tokensOf(bytes int) int {
	return int(float64(bytes) / a.bytesPerToken())
}

// estimate is how many tokens messages take, as this model counts them.
func (a *Agent) estimate(messages []llm.Message) int {
	return a.tokensOf(messageBytes(messages))
}

// measure learns how many bytes a token takes from what the provider
// counted for a request: the fixed part and the history before the reply.
// Code, logs and JSON take far fewer than four, and the estimates after a
// compaction were far too small without it.
func (a *Agent) measure(usage llm.Usage) {
	if usage.InputTokens <= 0 || len(a.History) == 0 {
		return
	}
	sent := a.fixedPartBytes() + messageBytes(a.History[:len(a.History)-1])
	ratio := float64(sent) / float64(usage.InputTokens)
	if ratio >= minBytesPerToken && ratio <= maxBytesPerToken {
		a.measuredBytesPerToken = ratio
	}
}

// Compact replaces the history with a summary written by the model. The turn
// in progress, from the last user message on, is kept as it is when it takes
// less than keptTurnShare of the window.
func (a *Agent) Compact(ctx context.Context, hooks Hooks) error {
	return a.compact(ctx, hooks, "", true)
}

// CompactWith compacts between turns, as /compact does, with an
// instruction of the user's for this summary: everything is summed up, as
// no turn is in progress to keep.
func (a *Agent) CompactWith(ctx context.Context, hooks Hooks, instruction string) error {
	a.CheckModel(ctx, hooks, CheckCompact)
	return a.compact(ctx, hooks, instruction, false)
}

func (a *Agent) compact(ctx context.Context, hooks Hooks, instruction string, midTurn bool) error {
	if len(a.History) == 0 {
		return nil
	}
	cut := a.lastUserMessage()
	kept := a.History[cut:]
	if !midTurn || cut == 0 || a.estimate(kept) > int(float64(a.Window)*keptTurnShare) {
		cut = len(a.History)
		kept = nil
	}

	hooks.activity(CompactingActivity)
	summary, err := a.summarize(ctx, a.History[:cut], instruction)
	hooks.activity("")
	if err != nil {
		return err
	}
	before := a.ContextUsed()
	request := a.currentRequest
	if !midTurn {
		request = ""
	}
	whole := slices.Clone(a.History)
	a.History = withSummary(summary, kept, request)
	a.LastUsage = llm.Usage{}
	a.historyAtUsage = 0
	a.sizeAfterCompaction = a.ContextUsed()
	if hooks.OnContextCut != nil {
		hooks.OnContextCut(whole)
	}
	if hooks.OnCompact != nil {
		hooks.OnCompact(before, a.sizeAfterCompaction)
	}
	a.step(hooks)
	return nil
}

// summarize sends the history as a plain-text transcript, without tools, so
// the model can only answer in text and the request fits in the window.
func (a *Agent) summarize(ctx context.Context, history []llm.Message, instruction string) (string, error) {
	prompt := a.compactSystem(instruction)
	transcript := transcriptOf(history)
	if a.Window > 0 {
		transcript = tools.Truncate(transcript, int(float64(a.Window)*transcriptShare*a.bytesPerToken()))
	}
	request := llm.Request{
		Model:     a.Model,
		System:    prompt,
		Messages:  []llm.Message{{Role: llm.RoleUser, Text: transcript + "\n\n" + writeSummaryNow}},
		MaxTokens: a.MaxTokens,
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var watch *repeatWatch
	if a.Loop.Enabled {
		watch = newRepeatWatch(a.Loop.StreamRepeats)
	}
	repeated := ""
	response, err := a.Provider.Stream(streamCtx, request, func(event llm.Event) {
		if watch != nil && repeated == "" {
			if repeated = watch.add(event); repeated != "" {
				cancel()
			}
		}
	})
	if repeated != "" && ctx.Err() == nil {
		return "", fmt.Errorf("the summary kept repeating itself: %s", Quote(repeated))
	}
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(response.Message.Text)
	if summary == "" {
		return "", errors.New("the model returned an empty summary")
	}
	return summary, nil
}

// compactSystem is the summary's system prompt as it is and, only when the
// user gave one, their instruction under it.
func (a *Agent) compactSystem(instruction string) string {
	prompt := a.CompactPrompt
	if prompt == "" {
		prompt = DefaultCompactPrompt
	}
	if instruction = strings.TrimSpace(instruction); instruction == "" {
		return prompt
	}
	return prompt + "\n\nThe user added the following instruction:\n" + instruction
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
		if request == "" {
			// Between turns: nothing to go on with but the next prompt.
			return []llm.Message{{Role: llm.RoleUser, Text: strings.TrimSpace(header)}}
		}
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

func messageBytes(messages []llm.Message) int {
	size := 0
	for _, message := range messages {
		size += len(message.Text)
		for _, call := range message.ToolCalls {
			size += len(call.Name) + len(call.Args)
		}
	}
	return size
}
