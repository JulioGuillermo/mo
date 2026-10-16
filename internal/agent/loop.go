package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
)

var ErrLooping = errors.New("stopped: the model kept repeating itself; send a message to go on")

const (
	loopNotePrefix     = "[mo loop guard] "
	repeatedCallResult = loopNotePrefix + "Not run: you already made this exact call %d times in this turn and got the same result each time. You are going in circles: use the result you have or try something different."
	stoppedCallResult  = loopNotePrefix + "Not run: mo stopped the turn because the model kept repeating itself."
	cutReplyNote       = loopNotePrefix + "Your last reply was cut off and removed because it kept repeating this, over and over:\n%s\nDo not write it again. Go on with the task another way."
	longestQuote       = 200
)

// LoopGuard sets how mo notices a model going round in circles: the same
// tool call with the same result within the last Replies replies of a turn,
// or a streamed piece of text written StreamRepeats times in a row. The
// model is told Warnings times, the next loop stops the turn.
type LoopGuard struct {
	Enabled       bool
	Replies       int
	Repeats       int
	StreamRepeats int
	Warnings      int
}

var readOnlyTools = map[string]bool{"read": true, "ls": true, "grep": true, "fetch": true, "web_search": true, "tool_search": true}

// earlierRuns counts how often the same call already ran, all with one same
// result, in the recent replies of this turn, back to the last different call
// that may change something.
func (a *Agent) earlierRuns(call llm.ToolCall) int {
	key := callKey(call)
	earlier := 0
	outputs := map[string]bool{}
	replies := 0
	history := a.History
	if len(a.inReply) > 0 {
		history = append(slices.Clone(a.History), a.inReply...)
	}
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
			made := message.ToolCalls[j]
			if j >= len(results) || results[j].Role != llm.RoleTool || results[j].ToolCallID != made.ID {
				continue
			}
			if callKey(made) != key {
				if !readOnlyTools[made.Name] {
					return sameResult(earlier, outputs)
				}
				continue
			}
			earlier++
			if !isLoopNote(results[j].Text) {
				outputs[results[j].Text] = true
			}
		}
	}
	return sameResult(earlier, outputs)
}

func sameResult(earlier int, outputs map[string]bool) int {
	if len(outputs) > 1 {
		return 0
	}
	return earlier
}

func (a *Agent) repeatedCall(call llm.ToolCall) (int, bool) {
	if !a.Loop.Enabled {
		return 0, false
	}
	earlier := a.earlierRuns(call)
	return earlier, earlier >= a.Loop.Repeats-1
}

func (a *Agent) looping() bool {
	return a.loops > a.Loop.Warnings
}

func (a *Agent) blockRepeatedCall(call llm.ToolCall, earlier int, hooks Hooks) string {
	a.loops++
	result := fmt.Sprintf(repeatedCallResult, earlier)
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

// callKey names a call by its tool and its arguments, with the keys sorted.
func callKey(call llm.ToolCall) string {
	decoder := json.NewDecoder(bytes.NewReader(call.Args))
	decoder.UseNumber()
	var args any
	if decoder.Decode(&args) != nil {
		return call.Name + "\x00" + string(call.Args)
	}
	canonical, _ := json.Marshal(args)
	return call.Name + "\x00" + string(canonical)
}
