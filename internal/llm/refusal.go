package llm

import "slices"

const refusedTextNote = "\n\n[the provider's safety filter stopped this answer. Rephrasing the request, or a new conversation, can help.]"

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
