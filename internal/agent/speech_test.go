package agent

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | SH-01 | Happy    | The voice mode reaches the provider, and a reply said is saved and heard whole once | P0 |
// | SH-02 | Happy    | The speech of a reply that only calls tools is dropped, unless SpeakCalls keeps it | P0  |
// | SH-03 | Edge     | A reply said without text reads as answered aloud, not as a silent model    | P1       |

import (
	"context"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func spoken(text string, calls ...llm.ToolCall) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Text: text, ToolCalls: calls, Speech: &llm.Speech{PCM: []byte{1, 2}, Rate: 24000}}
}

// heardReplies runs a prompt and returns the replies OnReplyEnd heard and
// the speeches SaveSpeech was given.
func heardReplies(t *testing.T, agent *Agent) ([]llm.Message, int) {
	t.Helper()
	var heard []llm.Message
	saved := 0
	hooks := Hooks{
		OnReplyEnd: func(reply llm.Message) { heard = append(heard, reply) },
		SaveSpeech: func(llm.Speech) string {
			saved++
			return "said.wav"
		},
	}
	if err := agent.Run(context.Background(), "go", hooks); err != nil {
		t.Fatal(err)
	}
	return heard, saved
}

func TestSpeech(t *testing.T) {
	t.Run("SH-01 the voice mode reaches the provider, and a reply said is saved and heard whole once", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{spoken("hello")}}
		agent := newAgent(t, provider)
		agent.Voice = "audio"

		heard, saved := heardReplies(t, agent)

		if provider.requests[0].Voice != "audio" || saved != 1 || len(heard) != 1 || heard[0].SpeechFile != "said.wav" || heard[0].Speech == nil {
			t.Errorf("voice %q saved %d heard %+v", provider.requests[0].Voice, saved, heard)
		}
		if agent.History[1].SpeechFile != "said.wav" {
			t.Errorf("history keeps %+v", agent.History[1])
		}
	})

	t.Run("SH-02 the speech of a reply that only calls tools is dropped, unless SpeakCalls keeps it", func(t *testing.T) {
		for _, speakCalls := range []bool{false, true} {
			provider := &scriptedProvider{replies: []llm.Message{spoken("", toolCall("c1", "read", `{"path":"a.txt"}`)), spoken("done")}}
			agent := newAgent(t, provider)
			agent.SpeakCalls = speakCalls

			heard, saved := heardReplies(t, agent)

			if len(heard) != 2 || (heard[0].Speech != nil) != speakCalls || saved != map[bool]int{false: 1, true: 2}[speakCalls] {
				t.Errorf("speak calls %v: first said %v, saved %d", speakCalls, heard[0].Speech != nil, saved)
			}
		}
	})

	t.Run("SH-03 a reply said without text reads as answered aloud, not as a silent model", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{spoken("")}})

		heardReplies(t, agent)

		if got := agent.History[1].Text; got != spokenNote {
			t.Errorf("text = %q", got)
		}
	})
}
