package llm

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | RF-01 | Happy    | Anthropic's stop_reason refusal ends the text with the refusal note, shown too | P0 |
// | RF-02 | Happy    | OpenAI's content_filter adds the note, and its refusal text is the answer | P0       |
// | RF-03 | Happy    | The Responses API's content_filter and refusal deltas do the same         | P1       |
// | RF-04 | Happy    | Gemini's SAFETY finish and a blocked prompt add the note                  | P1       |
// | RF-05 | Edge     | An answer that ends normally gets no note                                 | P0       |

import (
	"context"
	"strings"
	"testing"
)

func shownText(events *[]Event) func(Event) {
	return func(event Event) {
		if event.Kind == EventText {
			*events = append(*events, event)
		}
	}
}

func joinedText(events []Event) string {
	var out strings.Builder
	for _, event := range events {
		out.WriteString(event.Text)
	}
	return out.String()
}

func TestRefusals(t *testing.T) {
	t.Run("RF-01 Anthropic's stop_reason refusal ends the text with the refusal note, shown too", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Sure, "}}`,
			`{"type":"message_delta","delta":{"stop_reason":"refusal"},"usage":{"output_tokens":3}}`,
			`{"type":"message_stop"}`,
		}, nil)
		var events []Event

		response, err := (&Anthropic{BaseURL: server.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "m"}, shownText(&events))

		if err != nil || response.Message.Text != "Sure, "+refusedTextNote || joinedText(events) != response.Message.Text {
			t.Errorf("text = %q shown = %q err = %v", response.Message.Text, joinedText(events), err)
		}
	})

	t.Run("RF-02 OpenAI's content_filter adds the note, and its refusal text is the answer", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"refusal":"I can't help with that."}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`,
			`[DONE]`,
		}, nil)

		response, err := (&OpenAI{BaseURL: server.URL}).Stream(context.Background(), Request{}, func(Event) {})

		if err != nil || response.Message.Text != "I can't help with that."+refusedTextNote {
			t.Errorf("text = %q err = %v", response.Message.Text, err)
		}
	})

	t.Run("RF-03 the Responses API's content_filter and refusal deltas do the same", func(t *testing.T) {
		var got recorded
		provider := responsesServer(t, []string{
			`{"type":"response.refusal.delta","delta":"No."}`,
			`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"content_filter"}}}`,
		}, &got)

		response, err := provider.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "x"}}}, func(Event) {})

		if err != nil || response.Message.Text != "No."+refusedTextNote {
			t.Errorf("text = %q err = %v", response.Message.Text, err)
		}
	})

	t.Run("RF-04 Gemini's SAFETY finish and a blocked prompt add the note", func(t *testing.T) {
		safety := cloudCodeServer(t, []string{`{"response":{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}}`}, nil)
		blocked := cloudCodeServer(t, []string{`{"response":{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}}`}, nil)
		request := Request{Model: "gemini-3-flash", Messages: []Message{{Role: RoleUser, Text: "x"}}}

		first, firstErr := safety.Stream(context.Background(), request, func(Event) {})
		second, secondErr := blocked.Stream(context.Background(), request, func(Event) {})

		if firstErr != nil || secondErr != nil || first.Message.Text != refusedTextNote || second.Message.Text != refusedTextNote {
			t.Errorf("first = %q %v second = %q %v", first.Message.Text, firstErr, second.Message.Text, secondErr)
		}
	})

	t.Run("RF-05 an answer that ends normally gets no note", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		}, nil)

		response, err := (&Anthropic{BaseURL: server.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "m"}, func(Event) {})

		if err != nil || response.Message.Text != "done" {
			t.Errorf("text = %q err = %v", response.Message.Text, err)
		}
	})
}
