package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

var ErrLooping = errors.New("stopped: the model kept repeating itself; send a message to go on")

// ErrStepLimit stops a run that asked the model for MaxSteps replies.
var ErrStepLimit = errors.New("stopped at its step limit")

const (
	loopNotePrefix     = "[mo loop guard] "
	repeatedCallResult = loopNotePrefix + "Not run: you already made this exact call %d times in this turn and got the same result each time. You are going in circles: use the result you have or try something different."
	stoppedCallResult  = loopNotePrefix + "Not run: mo stopped the turn because the model kept repeating itself."
	cutReplyNote       = loopNotePrefix + "Your last reply was cut off and removed because it kept repeating this, over and over:\n%s\nDo not write it again. Go on with the task another way."
	longestQuote       = 200
)

// LoopGuard sets how mo notices a model going round in circles: the same
// tool call with the same result within the last Replies replies of a turn,
// or a streamed piece of text written StreamRepeats times in a row. The model is told Warnings times, the next loop stops the
// turn. Steps is how many replies a turn of the main agent may take, 0 for
// no limit.
type LoopGuard struct {
	Enabled       bool
	Replies       int
	Repeats       int
	StreamRepeats int
	Warnings      int
	Steps         int
}

// quietTools never change anything; git, memory and bash do or not by their
// action, and any other tool is taken to change something.
var quietTools = map[string]bool{"read": true, "ls": true, "grep": true, "fetch": true, "web_search": true, "tool_search": true, "tool_list": true}

// loopCall is a call of the recent replies with its result, as the guard
// sees it.
type loopCall struct {
	reply   int
	tool    string
	key     string
	changes bool
	output  string
	isError bool
}

// isPoll tells a look at a background job. What it shows can stay the same
// while the job goes on, and waiting for it is not going in circles, so a
// poll is never stopped; a model that only polls ends at the steps.
func isPoll(call llm.ToolCall) bool {
	return call.Name == "bash" && tools.BashAction(call.Args) == "output"
}

// mayChange tells whether a call can change something, which ends the count
// of the calls before it.
func (a *Agent) mayChange(call llm.ToolCall) bool {
	if quietTools[call.Name] {
		return false
	}
	if a.Tools != nil && (call.Name == "git" || call.Name == "memory" || call.Name == "bash") {
		return a.Tools.Changes(call)
	}
	return true
}

// unwrapped is the call a tool_call stands for, or the call itself.
func (a *Agent) unwrapped(call llm.ToolCall) llm.ToolCall {
	if a.Tools != nil {
		if inner, _, err := a.Tools.Unwrap(call); err == nil {
			return inner
		}
	}
	return call
}

// window lists the calls of the last Replies replies of this turn that got a
// result, the newest first. A compaction starts the count again: the model
// no longer holds what it read, and reading it again is no loop.
func (a *Agent) window() []loopCall {
	history := a.History
	if len(a.inReply) > 0 {
		history = append(slices.Clone(a.History), a.inReply...)
	}
	var calls []loopCall
	replies := 0
	for i := len(history) - 1; i >= 0 && replies < a.Loop.Replies; i-- {
		message := history[i]
		if isPrompt(message) {
			break
		}
		if message.Role != llm.RoleAssistant {
			continue
		}
		replies++
		results := history[i+1:]
		for j := len(message.ToolCalls) - 1; j >= 0; j-- {
			if j >= len(results) || results[j].Role != llm.RoleTool || results[j].ToolCallID != message.ToolCalls[j].ID {
				continue
			}
			made := a.unwrapped(message.ToolCalls[j])
			calls = append(calls, loopCall{reply: replies, tool: made.Name, key: callKey(made), changes: a.mayChange(made),
				output: results[j].Text, isError: results[j].IsError})
		}
	}
	return calls
}

// written tells, for each call of the window, how often it was made and
// whether its results differed: a call that keeps giving new results is
// looking at some state that moves, which is progress.
type written struct {
	times  int
	output string
	varied bool
}

func timesWritten(calls []loopCall) map[string]*written {
	seen := map[string]*written{}
	for _, call := range calls {
		entry := seen[call.key]
		if entry == nil {
			seen[call.key] = &written{times: 1, output: call.output}
			continue
		}
		entry.times++
		if !entry.varied && call.output != entry.output && !isLoopNote(call.output) && !isLoopNote(entry.output) {
			entry.varied = call.output != entry.output
		}
	}
	return seen
}

// stopsCount tells whether a call that is not the one counted ends the
// count. A call that may change something does, unless it is itself written
// over and over with the same results, as in a cycle of commands.
func stopsCount(call loopCall, seen map[string]*written) bool {
	entry := seen[call.key]
	return call.changes && (entry.times < 2 || entry.varied)
}

// earlierRuns counts how often the same call already ran with one same
// result, the last results alike, in the recent replies of this turn, back
// to the last different call that may change something. Results are alike
// only when they are the same to the letter: a time or a duration in them
// is a result that moved.
func earlierRuns(calls []loopCall, call llm.ToolCall) int {
	key := callKey(call)
	written := timesWritten(calls)
	earlier := 0
	last, seen := "", false
	for _, made := range calls {
		if made.key != key {
			if stopsCount(made, written) {
				break
			}
			continue
		}
		if !isLoopNote(made.output) {
			output := made.output
			if seen && output != last {
				break
			}
			last, seen = output, true
		}
		earlier++
	}
	return earlier
}

// shellExtraRepeats are the times more a shell command may be run with the
// same result: it is often how the model looks at something that moves
// outside, which cannot be told from a loop.
const shellExtraRepeats = 2

// repeatedCall tells whether a call is not run because it goes round in
// circles, and what the model is told.
func (a *Agent) repeatedCall(call llm.ToolCall) (string, bool) {
	if !a.Loop.Enabled || isPoll(call) {
		return "", false
	}
	repeats := a.Loop.Repeats
	if call.Name == "bash" {
		repeats += shellExtraRepeats
	}
	if earlier := earlierRuns(a.window(), call); earlier >= repeats-1 {
		return fmt.Sprintf(repeatedCallResult, earlier), true
	}
	return "", false
}

func (a *Agent) looping() bool {
	return a.loops > a.Loop.Warnings
}

func (a *Agent) blockRepeatedCall(call llm.ToolCall, result string, hooks Hooks) string {
	a.loops++
	a.announceBlocked(call, result, hooks)
	return result
}

// cutLoopingReply leaves the looping reply out of the history and, while
// warnings are left, tells the model what happened, in the last user
// message when there is one.
func (a *Agent) cutLoopingReply(repeated string, hooks Hooks) error {
	if hooks.OnLoopCut != nil {
		hooks.OnLoopCut(repeated)
	}
	a.loops++
	if a.looping() {
		return ErrLooping
	}
	note := fmt.Sprintf(cutReplyNote, Quote(repeated))
	last := len(a.History) - 1
	if a.History[last].Role == llm.RoleUser {
		a.History[last].Text += "\n\n" + note
		return nil
	}
	a.History = append(a.History, llm.Message{Role: llm.RoleUser, Text: note})
	return nil
}

// Quote shortens a repeated piece to one readable line.
func Quote(piece string) string {
	line := strings.Join(strings.Fields(strings.ToValidUTF8(piece, "")), " ")
	if len(line) > longestQuote {
		line = strings.ToValidUTF8(line[:longestQuote], "") + "…"
	}
	return "«" + line + "»"
}

func isLoopNote(text string) bool {
	return strings.HasPrefix(text, loopNotePrefix)
}

// isPrompt tells a message the user wrote from the notes mo adds for the model.
func isPrompt(message llm.Message) bool {
	return message.Role == llm.RoleUser && !isLoopNote(message.Text) && !strings.HasPrefix(message.Text, moNotePrefix)
}

// callKey names a call by its tool and its arguments, in one form: keys
// sorted, arguments left empty or at zero left out, and a path cleaned, so
// "./a.txt" and "a.txt" or an offset of 0 are one same call.
func callKey(call llm.ToolCall) string {
	decoder := json.NewDecoder(bytes.NewReader(call.Args))
	decoder.UseNumber()
	var args any
	if decoder.Decode(&args) != nil {
		return call.Name + "\x00" + string(call.Args)
	}
	if fields, ok := args.(map[string]any); ok {
		for name, value := range fields {
			switch value := value.(type) {
			case nil:
				delete(fields, name)
			case bool:
				if !value {
					delete(fields, name)
				}
			case string:
				if value == "" {
					delete(fields, name)
				} else if name == "path" {
					fields[name] = path.Clean(value)
				}
			case json.Number:
				if number, err := value.Float64(); err == nil && number == 0 {
					delete(fields, name)
				}
			}
		}
	}
	canonical, _ := json.Marshal(args)
	return call.Name + "\x00" + string(canonical)
}
