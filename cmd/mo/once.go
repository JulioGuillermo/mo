package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/team"
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

// followOnce shows what subagents do when mo answers once, each line under
// the subagent's name; what needs confirmation is denied as for the main
// agent, and nobody answers their questions.
func followOnce(subagents *team.Team) {
	subagents.Follow(func(info team.Info) agent.Hooks {
		prefix := fmt.Sprintf("  [%s·%d] ", info.Name, info.ID)
		return agent.Hooks{
			OnToolStart: func(call llm.ToolCall, summary string) {
				fmt.Fprintf(os.Stderr, "%s● %s %s\n", prefix, call.Name, summary)
			},
			OnToolDone: func(call llm.ToolCall, output string, isError bool) {
				if isError {
					fmt.Fprintf(os.Stderr, "%s  ⎿ error: %s\n", prefix, firstLine(output))
				}
			},
			Approve: func(_ context.Context, request agent.ApprovalRequest) agent.Decision {
				fmt.Fprintf(os.Stderr, "%s● %s %s\n%s  ⎿ denied (needs confirmation; run with -y to allow)\n", prefix, request.Call.Name, request.Summary, prefix)
				return agent.Deny
			},
		}
	}, nil, nil)
}
