package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const (
	noAnswer       = "(no answer: the user skipped it)"
	nobodyToAnswer = "The user cannot answer here: decide yourself and say what you assumed."
)

// Interaction is what the calls of one reply need from the user, asked all
// at once so they see how many there are: the calls to approve and the
// questions of ask calls, in the order the calls came.
type Interaction struct {
	Approvals []ApprovalRequest
	Questions []tools.Question
	// From names the subagent that asks; empty for the main agent.
	From string
}

// Reply holds a decision for each approval and an answer for each question;
// an empty answer is a question skipped.
type Reply struct {
	Decisions []Decision
	Answers   []string
	// Unavailable is set when nobody can answer, as when mo runs once.
	Unavailable bool
}

// pendingCall is a call of the reply being answered: settled already, or
// waiting for an approval or for the answers to its questions.
type pendingCall struct {
	outer     llm.ToolCall
	call      llm.ToolCall
	summary   string
	result    string
	isError   bool
	settled   bool
	need      approval
	approval  int
	questions []tools.Question
	firstAsk  int
}

func settled(call llm.ToolCall, result string) pendingCall {
	return pendingCall{outer: call, call: call, result: result, isError: true, settled: true, approval: -1}
}

// answerToolCalls answers every call of a reply: it checks them all, asks
// the user at once what they need, then runs them in order.
func (a *Agent) answerToolCalls(ctx context.Context, calls []llm.ToolCall, hooks Hooks) map[string]bool {
	var interaction Interaction
	pending := make([]pendingCall, len(calls))
	for i, call := range calls {
		pending[i] = a.prepareCall(ctx, call, hooks, &interaction)
	}
	reply := a.interact(ctx, interaction, hooks)
	unfinished := map[string]bool{}
	for _, call := range pending {
		stoppedBefore := ctx.Err() != nil
		output, isError, interrupted := a.finishCall(ctx, call, reply, hooks)
		if stoppedBefore || interrupted {
			unfinished[call.outer.ID] = true
		}
		a.History = append(a.History, llm.Message{Role: llm.RoleTool, ToolCallID: call.outer.ID, Text: output, IsError: isError})
	}
	return unfinished
}

// prepareCall settles what needs no user, and adds what does to the
// interaction. A call that repeats earlier replies is blocked here, so it is
// never offered for approval; finishCall checks again for the same call
// made twice in this reply, whose results only exist once they run.
func (a *Agent) prepareCall(ctx context.Context, outer llm.ToolCall, hooks Hooks, interaction *Interaction) pendingCall {
	switch {
	case ctx.Err() != nil:
		return settled(outer, cancelledResult)
	case outer.ArgsError != "":
		return settled(outer, outer.ArgsError)
	}
	call, _, err := a.Tools.Unwrap(outer)
	if err != nil {
		return settled(outer, err.Error())
	}
	call.Name = tools.Canonical(call.Name)
	if a.looping() {
		return settled(outer, stoppedCallResult)
	}
	if result, repeated := a.repeatedCall(call); repeated {
		return settled(outer, a.blockRepeatedCall(call, result, hooks))
	}
	if a.BlockToolCall != nil {
		if reason := a.BlockToolCall(ctx, call); reason != "" {
			a.announceBlocked(call, reason, hooks)
			return settled(outer, reason)
		}
	}
	pending := pendingCall{outer: outer, call: call, approval: -1}
	if call.Name == tools.AskName {
		questions, err := tools.ParseQuestions(call.Args)
		if err != nil {
			return settled(outer, err.Error())
		}
		pending.questions, pending.firstAsk = questions, len(interaction.Questions)
		interaction.Questions = append(interaction.Questions, questions...)
		return pending
	}
	pending.summary = a.Tools.Summary(call)
	pending.need = a.approvalFor(ctx, call, hooks)
	if blocked := pending.need.blocked; blocked != "" {
		a.announceBlocked(call, blocked, hooks)
		return settled(outer, blocked)
	}
	if pending.need.ask {
		pending.approval = len(interaction.Approvals)
		interaction.Approvals = append(interaction.Approvals, ApprovalRequest{
			Call: call, Summary: pending.summary, Warning: pending.need.warning, Reason: pending.need.reason,
			Commands: pending.need.commands, Ruled: pending.need.ruled,
		})
	}
	return pending
}

// interact asks the user everything at once. Without Interact, approvals go
// one by one to Approve and nobody answers questions; without either, every
// call that asks is denied.
func (a *Agent) interact(ctx context.Context, interaction Interaction, hooks Hooks) Reply {
	reply := Reply{Decisions: make([]Decision, len(interaction.Approvals)), Answers: make([]string, len(interaction.Questions))}
	switch {
	case len(interaction.Approvals) == 0 && len(interaction.Questions) == 0:
		return reply
	case hooks.Interact != nil:
		answered := hooks.Interact(ctx, interaction)
		copy(reply.Decisions, answered.Decisions)
		copy(reply.Answers, answered.Answers)
		reply.Unavailable = answered.Unavailable
		return reply
	}
	reply.Unavailable = true
	for i, request := range interaction.Approvals {
		if hooks.Approve != nil {
			reply.Decisions[i] = hooks.Approve(ctx, request)
		}
	}
	return reply
}

// finishCall runs a call the user let through, or says why it did not run.
func (a *Agent) finishCall(ctx context.Context, pending pendingCall, reply Reply, hooks Hooks) (string, bool, bool) {
	output, isError, run := a.decideCall(ctx, pending, reply, hooks)
	if !run {
		return output, isError, false
	}
	if hooks.OnToolStart != nil {
		hooks.OnToolStart(pending.call, pending.summary)
	}
	output, isError = a.runTool(ctx, pending.call)
	interrupted := ctx.Err() != nil
	if hooks.OnToolDone != nil {
		hooks.OnToolDone(pending.call, output, isError)
	}
	return output, isError, interrupted
}

// decideCall tells whether a call runs now that the user answered: run, or
// its result without running, as when it was denied or stopped.
func (a *Agent) decideCall(ctx context.Context, pending pendingCall, reply Reply, hooks Hooks) (string, bool, bool) {
	switch {
	case pending.settled:
		return pending.result, pending.isError, false
	case ctx.Err() != nil:
		return cancelledResult, true, false
	case pending.questions != nil:
		return answersText(pending.questions, reply.Answers[pending.firstAsk:pending.firstAsk+len(pending.questions)], reply.Unavailable), false, false
	case a.looping():
		return stoppedCallResult, true, false
	}
	if result, repeated := a.repeatedCall(pending.call); repeated {
		return a.blockRepeatedCall(pending.call, result, hooks), true, false
	}
	if pending.approval >= 0 {
		decision := reply.Decisions[pending.approval]
		if decision == Deny {
			return deniedResult, true, false
		}
		if decision == AllowAlways && pending.need.warning == "" && !pending.need.ruled {
			if a.alwaysAllowed == nil {
				a.alwaysAllowed = map[string]bool{}
			}
			a.alwaysAllowed[pending.call.Name] = true
		}
	}
	return "", false, true
}

// answersText gives the model each question with its answer.
func answersText(questions []tools.Question, answers []string, unavailable bool) string {
	if unavailable {
		return nobodyToAnswer
	}
	var out []string
	for i, question := range questions {
		answer := strings.TrimSpace(answers[i])
		if answer == "" {
			answer = noAnswer
		}
		out = append(out, fmt.Sprintf("Q: %s\nA: %s", question.Text, answer))
	}
	return strings.Join(out, "\n\n")
}
