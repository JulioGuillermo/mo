package llm

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | TK-01 | Happy    | <think> at the start goes to reasoning, also with tags split in deltas | P0     |
// | TK-02 | Edge     | <think> later in the answer stays in the answer                      | P1       |
// | TK-03 | Edge     | An answer cut inside <think> keeps what came as reasoning            | P2       |
// | TK-04 | Happy    | The reasoning streamed is kept in the message and sent back as reasoning_content | P0 |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func split(deltas ...string) (string, string) {
	var s thinkSplitter
	var answer, reasoning strings.Builder
	for _, delta := range deltas {
		a, r := s.feed(delta)
		answer.WriteString(a)
		reasoning.WriteString(r)
	}
	a, r := s.flush()
	answer.WriteString(a)
	reasoning.WriteString(r)
	return answer.String(), reasoning.String()
}

func TestThinkSplitter(t *testing.T) {
	t.Run("TK-01 <think> at the start goes to reasoning, also with tags split in deltas", func(t *testing.T) {
		answer, reasoning := split("\n<thi", "nk>let me ", "see</th", "ink>\n\nHello")

		if answer != "Hello" || reasoning != "let me see" {
			t.Errorf("answer = %q reasoning = %q", answer, reasoning)
		}
	})

	t.Run("TK-02 <think> later in the answer stays in the answer", func(t *testing.T) {
		answer, reasoning := split("Use the ", "<think> tag")

		if answer != "Use the <think> tag" || reasoning != "" {
			t.Errorf("answer = %q reasoning = %q", answer, reasoning)
		}
	})

	t.Run("TK-03 an answer cut inside <think> keeps what came as reasoning", func(t *testing.T) {
		answer, reasoning := split("<think>half a thou", "ght</")

		if answer != "" || reasoning != "half a thought</" {
			t.Errorf("answer = %q reasoning = %q", answer, reasoning)
		}
	})

	t.Run("TK-04 the reasoning streamed is kept in the message and sent back as reasoning_content", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"content":"<think>hmm"}}]}`,
			`{"choices":[{"delta":{"content":"</think>Hi"}}]}`,
			`[DONE]`,
		}, nil)
		response, err := (&OpenAI{BaseURL: server.URL}).Stream(context.Background(), Request{}, func(Event) {})
		wire, _ := json.Marshal(toOpenAIMessage(response.Message))

		if err != nil || response.Message.Text != "Hi" || response.Message.Reasoning != "hmm" || !strings.Contains(string(wire), `"reasoning_content":"hmm"`) {
			t.Errorf("message = %+v wire = %s err = %v", response.Message, wire, err)
		}
	})
}
