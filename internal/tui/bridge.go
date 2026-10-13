package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

type streamMsg llm.Event
type streamEndMsg struct{}
type usageMsg llm.Usage
type runDoneMsg struct{ err error }

type approvalMsg struct {
	request agent.ApprovalRequest
	reply   chan agent.Decision
}

// bridge turns agent callbacks, which run on the agent goroutine, into
// program messages and scrollback lines.
type bridge struct {
	program *tea.Program
	printer *printer
	exited  chan struct{}
}

// print adds lines to the scrollback in order, and gives up once the
// program has exited, since nothing reads its messages any more.
func (b *bridge) print(text string) {
	printed := make(chan struct{})
	go func() {
		b.program.Println(text)
		close(printed)
	}()
	select {
	case <-printed:
	case <-b.exited:
	}
}

func (b *bridge) hooks() agent.Hooks {
	return agent.Hooks{
		OnEvent: func(event llm.Event) {
			b.program.Send(streamMsg(event))
		},
		OnAssistant: func(message llm.Message) {
			b.program.Send(streamEndMsg{})
			if text := strings.TrimSpace(message.Text); text != "" {
				b.print(b.printer.Answer(text))
			}
		},
		OnToolStart: func(call llm.ToolCall, summary string) {
			b.print(b.printer.ToolStart(call.Name, summary))
		},
		OnToolDone: func(call llm.ToolCall, output string, isError bool) {
			b.print(b.printer.ToolResult(output, isError))
			if images := b.printer.Images(output); images != "" {
				b.print(images)
			}
		},
		OnUsage: func(usage llm.Usage) {
			b.program.Send(usageMsg(usage))
		},
		OnNotice: func(text string) {
			b.print(b.printer.Note(text))
		},
		OnCompact: func(before, after int) {
			b.program.Send(usageMsg(llm.Usage{InputTokens: after}))
			b.print(b.printer.Note(fmt.Sprintf("context compacted: %s → %s tokens", formatTokens(before), formatTokens(after))))
		},
		Approve: b.approve,
	}
}

func (b *bridge) approve(ctx context.Context, request agent.ApprovalRequest) agent.Decision {
	reply := make(chan agent.Decision, 1)
	b.program.Send(approvalMsg{request: request, reply: reply})
	select {
	case decision := <-reply:
		if decision == agent.Deny {
			b.print(b.printer.ToolStart(request.Call.Name, request.Summary) + "\n" + b.printer.Denied(request.Call.Name))
		}
		return decision
	case <-ctx.Done():
		return agent.Deny
	}
}
