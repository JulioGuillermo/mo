package llm

import (
	"slices"
	"strings"
)

const (
	// RefusedNote ends an answer the provider's safety filter stopped.
	RefusedNote = "[the provider's safety filter stopped this answer. Rephrasing the request, or a new conversation, can help.]"
	// SilentNote is the answer of a turn that ended with no text and no
	// call, the provider giving no reason.
	SilentNote      = "[the model ended without answering, and the provider gave no reason: a filter may have stopped it. Rephrasing the request, or a new conversation, can help.]"
	refusedTextNote = "\n\n" + RefusedNote
)

// StopNote splits off the note an answer ends with when a filter stopped
// it, the brackets taken off; note is "" for an answer that ended well.
func StopNote(text string) (answer, note string) {
	trimmed := strings.TrimRight(text, "\n ")
	for _, each := range []string{RefusedNote, SilentNote} {
		if before, found := strings.CutSuffix(trimmed, each); found {
			return strings.TrimRight(before, "\n "), strings.Trim(each, "[]")
		}
	}
	return text, ""
}

// geminiRefusals are the finish reasons Gemini gives when its filters stop
// an answer.
var geminiRefusals = []string{"SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY"}

func geminiRefused(finishReason string) bool {
	return slices.Contains(geminiRefusals, finishReason)
}

// noteRefusal adds the note of a refused answer to it, and shows it as
// the end of the text.
func noteRefusal(message *Message, onEvent func(Event)) {
	message.Text += refusedTextNote
	onEvent(Event{Kind: EventText, Text: refusedTextNote})
}
