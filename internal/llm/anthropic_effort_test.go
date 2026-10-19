package llm

// Test Traceability Table
// | ID    | Category | Description                                                                   | Priority |
// | AE-01 | Happy    | Effort goes as Claude Code sends it: the set level, the model's default, or high | P0     |
// | AE-02 | Edge     | A model without effort gets none; one not known gets what is set, as it is     | P0       |
// | AE-03 | Happy    | Model names with dates, prefixes and suffixes are found                         | P1       |
// | AE-04 | Happy    | The menu offers each model its levels, xhigh where it has it                    | P1       |
// | AE-05 | Happy    | The effort reaches the wire in output_config, as a word                         | P0       |
// | AE-06 | Happy    | max_tokens and thinking go as Claude Code sends them, per model                 | P0       |
// | AE-07 | Happy    | Thinking blocks come back with their signature and go back unchanged            | P0       |
// | AE-08 | Error    | Thinking blocks refused are left out and the request sent again                 | P1       |
// | AE-09 | Error    | Thinking refused is left out, the effort kept, and none asked after             | P1       |

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestAnthropicEffort(t *testing.T) {
	t.Run("AE-01 effort goes as Claude Code sends it", func(t *testing.T) {
		cases := []struct{ model, set, want string }{
			{"claude-opus-5", "low", "low"},
			{"claude-opus-5", "max", "max"},
			{"claude-opus-5", "", "high"},
			{"claude-opus-4-7", "", "xhigh"},
			{"claude-opus-5-5", "", "medium"},
			{"claude-sonnet-5-5", "", "medium"},
			{"claude-haiku-5-5", "", "medium"},
			{"claude-sonnet-4-6", "xhigh", "high"},
			{"claude-opus-4-6", "max", "max"},
			{"claude-opus-4-5", "max", "high"},
			{"claude-opus-5", "none", "high"},
		}
		for _, c := range cases {
			if got := anthropicSentEffort(c.model, c.set); got != c.want {
				t.Errorf("%s with %q sent %q, want %q", c.model, c.set, got, c.want)
			}
		}
	})

	t.Run("AE-02 a model without effort gets none; one not known gets what is set", func(t *testing.T) {
		for _, model := range []string{"claude-haiku-4-5", "claude-sonnet-4-5", "claude-3-7-sonnet-20250219", "claude-opus-4-1"} {
			if got := anthropicSentEffort(model, "high"); got != "" {
				t.Errorf("%s sent %q", model, got)
			}
		}
		if got := anthropicSentEffort("claude-future-9", "xhigh"); got != "xhigh" {
			t.Errorf("unknown model sent %q", got)
		}
		if got := anthropicSentEffort("claude-future-9", ""); got != "" {
			t.Errorf("unknown model with nothing set sent %q", got)
		}
	})

	t.Run("AE-03 model names with dates, prefixes and suffixes are found", func(t *testing.T) {
		names := map[string]string{
			"claude-sonnet-4-20250514":          "claude-sonnet-4-0",
			"claude-haiku-4-5-20251001":         "claude-haiku-4-5",
			"us.anthropic.claude-opus-4-8-v1:0": "claude-opus-4-8",
			"claude-opus-4-6@20260101":          "claude-opus-4-6",
			"claude-opus-5[1m]":                 "claude-opus-5",
			"anthropic/claude-sonnet-5-5":       "claude-sonnet-5-5",
		}
		for in, want := range names {
			if got := canonicalAnthropicModel(in); got != want {
				t.Errorf("%s is %s, want %s", in, got, want)
			}
		}
	})

	t.Run("AE-04 the menu offers each model its levels", func(t *testing.T) {
		provider := &Anthropic{}
		opus, opusKnown := provider.Efforts(context.Background(), "claude-opus-5")
		sonnet, _ := provider.Efforts(context.Background(), "claude-sonnet-4-6")
		haiku, haikuKnown := provider.Efforts(context.Background(), "claude-haiku-4-5")
		other, otherKnown := provider.Efforts(context.Background(), "claude-future-9")

		if !opusKnown || !slices.Equal(opus, anthropicAllLevels) {
			t.Errorf("opus 5 = %v", opus)
		}
		if slices.Contains(sonnet, "xhigh") || !slices.Contains(sonnet, "max") {
			t.Errorf("sonnet 4.6 = %v", sonnet)
		}
		if !haikuKnown || len(haiku) != 0 {
			t.Errorf("haiku 4.5 = %v", haiku)
		}
		if otherKnown || !slices.Equal(other, anthropicAllLevels) {
			t.Errorf("unknown = %v %v", other, otherKnown)
		}
	})

	t.Run("AE-05 the effort reaches the wire in output_config, as a word", func(t *testing.T) {
		var bodies []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
		}))
		defer server.Close()
		provider := &Anthropic{BaseURL: server.URL, APIKey: "k"}
		hi := []Message{{Role: RoleUser, Text: "hi"}}

		for _, req := range []Request{
			{Model: "claude-opus-5-5", Messages: hi},
			{Model: "claude-sonnet-4-6", Effort: "xhigh", Messages: hi},
			{Model: "claude-haiku-4-5", Effort: "high", Messages: hi},
		} {
			if _, err := provider.Stream(context.Background(), req, func(Event) {}); err != nil {
				t.Fatal(err)
			}
		}

		if len(bodies) != 3 || !strings.Contains(bodies[0], `"output_config":{"effort":"medium"}`) ||
			!strings.Contains(bodies[1], `"output_config":{"effort":"high"}`) || strings.Contains(bodies[2], "output_config") {
			t.Errorf("bodies %v", bodies)
		}
	})

	t.Run("AE-06 max_tokens and thinking go as Claude Code sends them", func(t *testing.T) {
		cases := []struct {
			model, want string
			tokens      int
		}{
			{"claude-opus-5", `"thinking":{"type":"adaptive","display":"summarized"}`, 64000},
			{"claude-sonnet-4-6", `"thinking":{"type":"adaptive","display":"summarized"}`, 32000},
			{"claude-opus-5-5", `"thinking":{"type":"adaptive","display":"summarized"}`, 128000},
			{"claude-haiku-4-5", `"thinking":{"type":"enabled","budget_tokens":31999,"display":"summarized"}`, 32000},
		}
		for _, c := range cases {
			built := buildAnthropicRequest(Request{Model: c.model})
			if body := encoded(t, built); built.MaxTokens != c.tokens || !strings.Contains(body, c.want) {
				t.Errorf("%s: %s", c.model, body)
			}
		}
		if body := encoded(t, buildAnthropicRequest(Request{Model: "claude-opus-5", NoThinking: true})); strings.Contains(body, "thinking") {
			t.Errorf("no thinking: %s", body)
		}
		if built := buildAnthropicRequest(Request{Model: "claude-future-9"}); built.Thinking != nil || built.MaxTokens != anthropicDefaultMaxTokens {
			t.Errorf("unknown: %+v", built)
		}
		if built := buildAnthropicRequest(Request{Model: "claude-opus-5", MaxTokens: 5000}); built.MaxTokens != 5000 {
			t.Errorf("set max tokens: %d", built.MaxTokens)
		}
	})

	t.Run("AE-07 thinking blocks come back with their signature and go back unchanged", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me see"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"c2ln"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"ZGF0YQ=="}}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t1","name":"ls"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`{"type":"message_stop"}`,
		}, nil)

		response, err := (&Anthropic{BaseURL: server.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "claude-opus-5"}, func(Event) {})
		if err != nil {
			t.Fatal(err)
		}
		sent := encoded(t, buildAnthropicRequest(Request{Model: "claude-opus-5", Messages: []Message{{Role: RoleUser, Text: "hi"}, response.Message}}))

		if response.Message.Reasoning != "let me see" {
			t.Errorf("reasoning %q", response.Message.Reasoning)
		}
		want := `"content":[{"type":"thinking","thinking":"let me see","signature":"c2ln"},{"type":"redacted_thinking","data":"ZGF0YQ=="},{"type":"tool_use"`
		if !strings.Contains(sent, want) {
			t.Errorf("sent %s", sent)
		}
	})

	t.Run("AE-08 thinking blocks refused are left out and the request sent again", func(t *testing.T) {
		var bodies []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			if strings.Contains(string(body), `"signature"`) {
				http.Error(w, "{\"error\":{\"message\":\"messages.1.content.0: Invalid `signature` in `thinking` block\"}}", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
		}))
		defer server.Close()
		history := []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Text: "ok", Thinking: []byte(`[{"type":"thinking","thinking":"x","signature":"bad"}]`)},
			{Role: RoleUser, Text: "again"},
		}

		_, err := (&Anthropic{BaseURL: server.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "claude-opus-5", Effort: "high", Messages: history}, func(Event) {})

		if err != nil || len(bodies) != 2 || strings.Contains(bodies[1], "signature") || !strings.Contains(bodies[1], `"effort":"high"`) {
			t.Errorf("err %v bodies %v", err, bodies)
		}
	})

	t.Run("AE-09 thinking refused is left out, the effort kept, and none asked after", func(t *testing.T) {
		var bodies []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			if strings.Contains(string(body), `"thinking":{`) {
				http.Error(w, `{"error":{"message":"thinking: Extra inputs are not permitted"}}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
		}))
		defer server.Close()
		provider := &Anthropic{BaseURL: server.URL, APIKey: "k"}
		req := Request{Model: "claude-opus-5", Effort: "high", Messages: []Message{{Role: RoleUser, Text: "hi"}}}

		_, err := provider.Stream(context.Background(), req, func(Event) {})
		_, again := provider.Stream(context.Background(), req, func(Event) {})

		if err != nil || again != nil || len(bodies) != 3 || strings.Contains(bodies[2], `"thinking":{`) || !strings.Contains(bodies[1], `"effort":"high"`) {
			t.Errorf("err %v again %v bodies %v", err, again, bodies)
		}
	})
}
