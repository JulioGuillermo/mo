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
type printMsg string
type streamEndMsg struct{}
type usageMsg llm.Usage

// activityMsg says what the agent waits on besides the reply.
type activityMsg string
type runDoneMsg struct{ err error }

// queuedReadMsg says the model now reads that many queued messages.
type queuedReadMsg int

// modelMsg says the server now serves another model or has another context
// window than the agent went on with.
type modelMsg struct {
	model  string
	window int
}

// bridge turns agent callbacks, which run on the agent goroutine, into
// program messages and scrollback lines.
type bridge struct {
	program *tea.Program
	printer *printer
	exited  chan struct{}
	// pacer holds what the model writes as render.reply_refresh says.
	pacer *pacer
}

// paceReply draws what the model writes at the pace given.
func (b *bridge) paceReply(pace replyPace) {
	if b == nil {
		return
	}
	if b.pacer == nil {
		b.pacer = &pacer{send: func(msg tea.Msg) { b.program.Send(msg) }}
	}
	b.pacer.setPace(pace)
}

// flush sends what the pacer holds, before what the agent tells next.
func (b *bridge) flush() {
	if b != nil && b.pacer != nil {
		b.pacer.flush()
	}
}

// print adds lines to the scrollback in order, and gives up once the
// program has exited, since nothing reads its messages any more.
func (b *bridge) print(text string) {
	b.send(printMsg(text))
}

// draw adds lines that are drawn again when the terminal is resized.
func (b *bridge) draw(draw func() string) {
	b.send(drawMsg(draw))
}

func (b *bridge) send(msg tea.Msg) {
	printed := make(chan struct{})
	go func() {
		b.program.Send(msg)
		close(printed)
	}()
	select {
	case <-printed:
	case <-b.exited:
	}
}

func (b *bridge) hooks() agent.Hooks {
	hooks := agent.Hooks{
		OnEvent: func(event llm.Event) {
			if b.pacer != nil {
				b.pacer.event(event)
				return
			}
			b.program.Send(streamMsg(event))
		},
		OnAssistant: func(message llm.Message) {
			b.program.Send(streamEndMsg{})
			if message.Reasoning != "" {
				b.send(reasoningMsg(message.Reasoning))
			}
			if text := strings.TrimSpace(message.Text); text != "" {
				b.draw(func() string { return b.printer.Answer(text) })
			}
		},
		OnToolStart: func(call llm.ToolCall, summary string) {
			b.send(toolStartMsg{call: call, summary: summary})
		},
		OnToolDone: func(call llm.ToolCall, output string, isError bool) {
			b.send(toolDoneMsg{id: call.ID, output: output, isError: isError})
			if images := b.printer.Images(output); images != "" {
				b.print(images)
			}
		},
		OnActivity: func(text string) {
			b.program.Send(activityMsg(text))
		},
		OnUsage: func(usage llm.Usage) {
			b.program.Send(usageMsg(usage))
		},
		OnNotice: func(text string) {
			b.print(b.printer.Note(text))
		},
		OnWarning: func(text string) {
			b.print(b.printer.Warning(text))
		},
		OnQueued: func(count int) {
			b.send(queuedReadMsg(count))
		},
		OnModel: func(model string, window int) {
			b.program.Send(modelMsg{model: model, window: window})
		},
		OnLoopCut: func(repeated string) {
			b.program.Send(streamEndMsg{})
			b.print(b.printer.Note("loop: reply cut and left out, it kept repeating " + agent.Quote(repeated)))
		},
		OnCompact: func(before, after int) {
			b.program.Send(usageMsg(llm.Usage{InputTokens: after}))
			b.print(b.printer.Note(fmt.Sprintf("context compacted: %s → %s tokens", formatTokens(before), formatTokens(after))))
		},
		Interact: b.interact,
		OnStep: func(history []llm.Message) {
			b.send(historyMsg(history))
		},
	}
	return b.flushingFirst(hooks)
}

// flushingFirst makes every hook but OnEvent send what the pacer holds
// before its own: what asks, starts or ends is drawn at once, after the
// reply that came before it.
func (b *bridge) flushingFirst(hooks agent.Hooks) agent.Hooks {
	first := func(do func()) func() {
		return func() {
			b.flush()
			do()
		}
	}
	onAssistant, onToolStart, onToolDone := hooks.OnAssistant, hooks.OnToolStart, hooks.OnToolDone
	onActivity, onUsage, onNotice, onLoopCut, onCompact, onQueued, interact := hooks.OnActivity, hooks.OnUsage, hooks.OnNotice, hooks.OnLoopCut, hooks.OnCompact, hooks.OnQueued, hooks.Interact
	if onStep := hooks.OnStep; onStep != nil {
		hooks.OnStep = func(history []llm.Message) { first(func() { onStep(history) })() }
	}
	hooks.OnAssistant = func(message llm.Message) { first(func() { onAssistant(message) })() }
	hooks.OnToolStart = func(call llm.ToolCall, summary string) { first(func() { onToolStart(call, summary) })() }
	hooks.OnToolDone = func(call llm.ToolCall, output string, isError bool) {
		first(func() { onToolDone(call, output, isError) })()
	}
	hooks.OnActivity = func(text string) { first(func() { onActivity(text) })() }
	hooks.OnUsage = func(usage llm.Usage) { first(func() { onUsage(usage) })() }
	hooks.OnNotice = func(text string) { first(func() { onNotice(text) })() }
	if onWarning := hooks.OnWarning; onWarning != nil {
		hooks.OnWarning = func(text string) { first(func() { onWarning(text) })() }
	}
	hooks.OnLoopCut = func(repeated string) { first(func() { onLoopCut(repeated) })() }
	hooks.OnCompact = func(before, after int) { first(func() { onCompact(before, after) })() }
	hooks.OnQueued = func(count int) { first(func() { onQueued(count) })() }
	hooks.Interact = func(ctx context.Context, interaction agent.Interaction) agent.Reply {
		b.flush()
		return interact(ctx, interaction)
	}
	return hooks
}

// agentsChangedMsg says a subagent changed; agentEndedMsg that one the main
// agent started in the background ended.
type agentsChangedMsg struct{}
type agentEndedMsg struct{}
