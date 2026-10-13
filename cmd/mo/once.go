package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

// runOnce answers a single prompt without the UI: the answer goes to stdout,
// tool activity to stderr. Calls that need confirmation are denied; -y skips
// the confirmation of the configured tools.
func runOnce(ctx context.Context, a *agent.Agent, prompt string) error {
	hooks := agent.Hooks{
		OnEvent: func(event llm.Event) {
			if event.Kind == llm.EventText {
				fmt.Print(event.Text)
			}
		},
		OnAssistant: func(message llm.Message) {
			if message.Text != "" {
				fmt.Println()
			}
		},
		OnToolStart: func(call llm.ToolCall, summary string) {
			fmt.Fprintf(os.Stderr, "● %s %s\n", call.Name, summary)
		},
		OnToolDone: func(call llm.ToolCall, output string, isError bool) {
			if isError {
				fmt.Fprintf(os.Stderr, "  ⎿ error: %s\n", firstLine(output))
			}
		},
		OnNotice: func(text string) {
			fmt.Fprintf(os.Stderr, "(%s)\n", text)
		},
		OnLoopCut: func(repeated string) {
			fmt.Println()
			fmt.Fprintf(os.Stderr, "\n(loop: reply cut, it kept repeating %s)\n", agent.Quote(repeated))
		},
		OnCompact: func(before, after int) {
			fmt.Fprintf(os.Stderr, "(context compacted: %d → %d tokens)\n", before, after)
		},
		Approve: func(_ context.Context, request agent.ApprovalRequest) agent.Decision {
			fmt.Fprintf(os.Stderr, "● %s %s\n  ⎿ denied (needs confirmation; run with -y to allow)\n", request.Call.Name, request.Summary)
			return agent.Deny
		},
	}
	return a.Run(ctx, prompt, hooks)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}
