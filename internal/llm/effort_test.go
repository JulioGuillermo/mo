package llm

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | EF-01 | Happy    | Anthropic gets the effort in output_config; without one, nothing      | P0       |
// | EF-02 | Happy    | OpenAI-compatible servers get it as reasoning_effort; without one, nothing | P0  |
// | PP-01 | Happy    | llama-server is asked for prompt progress, and each chunk of it is an event | P1  |
// | PP-02 | Edge     | Other servers are never sent return_progress                             | P0       |
// | PP-03 | Happy    | /props tells llama-server apart from servers without it                   | P1       |
// | LM-01 | Happy    | Anthropic's models are listed with the key, from the Models API          | P1       |
// | GS-01 | Happy    | Gemini's thought signature on a call is kept and sent back with it       | P0       |
// | OA-01 | Happy    | A sign-in's token is the bearer at each request, over the key; its error stops the request | P0 |

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func encoded(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEffort(t *testing.T) {
	t.Run("EF-01 Anthropic gets the effort in output_config; without one, nothing", func(t *testing.T) {
		with := encoded(t, buildAnthropicRequest(Request{Model: "m", Effort: "xhigh"}))
		without := encoded(t, buildAnthropicRequest(Request{Model: "m"}))

		if !strings.Contains(with, `"output_config":{"effort":"xhigh"}`) || strings.Contains(without, "output_config") {
			t.Errorf("with %s\nwithout %s", with, without)
		}
	})

	t.Run("EF-02 OpenAI-compatible servers get it as reasoning_effort; without one, nothing", func(t *testing.T) {
		provider := &OpenAI{}
		with := encoded(t, provider.buildRequest(Request{Model: "m", Effort: "low"}))
		without := encoded(t, provider.buildRequest(Request{Model: "m"}))

		if !strings.Contains(with, `"reasoning_effort":"low"`) || strings.Contains(without, "reasoning_effort") {
			t.Errorf("with %s\nwithout %s", with, without)
		}
	})

	t.Run("PP-01 llama-server is asked for prompt progress, and each chunk of it is an event", func(t *testing.T) {
		var body string
		server := sseServer(t, []string{
			`{"choices":[],"prompt_progress":{"total":900,"cache":100,"processed":500,"time_ms":10}}`,
			`{"choices":[],"prompt_progress":{"total":900,"cache":100,"processed":900,"time_ms":20}}`,
			`{"choices":[{"delta":{"content":"Hi"}}]}`,
			`[DONE]`,
		}, func(raw []byte) { body = string(raw) })
		provider := &OpenAI{BaseURL: server.URL, PromptProgress: true}
		var progress []PromptProgress

		response, err := provider.Stream(context.Background(), Request{Model: "m"}, func(event Event) {
			if event.Kind == EventPromptProgress {
				progress = append(progress, event.Progress)
			}
		})

		if err != nil || response.Message.Text != "Hi" || !strings.Contains(body, `"return_progress":true`) {
			t.Fatalf("text %q err %v body %s", response.Message.Text, err, body)
		}
		if len(progress) != 2 || progress[0] != (PromptProgress{Done: 500, Total: 900, Cached: 100}) || progress[0].Share() != 0.5 || progress[1].Share() != 1 {
			t.Errorf("progress = %+v", progress)
		}
	})

	t.Run("PP-02 other servers are never sent return_progress", func(t *testing.T) {
		if body := encoded(t, (&OpenAI{}).buildRequest(Request{Model: "m"})); strings.Contains(body, "return_progress") {
			t.Errorf("body %s", body)
		}
	})

	t.Run("PP-03 /props tells llama-server apart from servers without it", func(t *testing.T) {
		llama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":32768}}`)
		}))
		defer llama.Close()
		other := httptest.NewServer(http.NotFoundHandler())
		defer other.Close()

		size, isLlama := (&OpenAI{BaseURL: llama.URL + "/v1"}).Props(context.Background())
		_, isOther := (&OpenAI{BaseURL: other.URL + "/v1"}).Props(context.Background())

		if size != 32768 || !isLlama || isOther {
			t.Errorf("size %d llama %v other %v", size, isLlama, isOther)
		}
	})

	t.Run("LM-01 Anthropic's models are listed with the key, from the Models API", func(t *testing.T) {
		var key, version string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, version = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
			fmt.Fprint(w, `{"data":[{"id":"claude-opus-5-5"},{"id":"claude-sonnet-5-5"}],"has_more":false}`)
		}))
		defer server.Close()

		models, err := (&Anthropic{BaseURL: server.URL, APIKey: "sk-test"}).ListModels(context.Background())

		if err != nil || strings.Join(models, ",") != "claude-opus-5-5,claude-sonnet-5-5" || key != "sk-test" || version == "" {
			t.Errorf("models %v err %v key %q", models, err, key)
		}
	})

	t.Run("GS-01 Gemini's thought signature on a call is kept and sent back with it", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"ls","arguments":"{}"},"extra_content":{"google":{"thought_signature":"c2lnbmF0dXJl"}}}]}}]}`,
			`[DONE]`,
		}, nil)

		response, err := (&OpenAI{BaseURL: server.URL}).Stream(context.Background(), Request{Model: "gemini-3"}, func(Event) {})
		sent := encoded(t, (&OpenAI{}).buildRequest(Request{Model: "gemini-3", Messages: []Message{response.Message}}))

		if err != nil || len(response.Message.ToolCalls) != 1 || !strings.Contains(string(response.Message.ToolCalls[0].Extra), "c2lnbmF0dXJl") {
			t.Fatalf("calls %+v err %v", response.Message.ToolCalls, err)
		}
		if !strings.Contains(sent, `"extra_content":{"google":{"thought_signature":"c2lnbmF0dXJl"}}`) {
			t.Errorf("sent %s", sent)
		}
	})

	t.Run("OA-01 a sign-in's token is the bearer at each request, over the key; its error stops the request", func(t *testing.T) {
		var bearer string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer = r.Header.Get("Authorization")
			fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
		}))
		defer server.Close()
		signedIn := &OpenAI{BaseURL: server.URL, APIKey: "sk-key", Token: func(context.Context) (string, error) { return "tok-1", nil }}
		signedOut := &OpenAI{BaseURL: server.URL, Token: func(context.Context) (string, error) { return "", fmt.Errorf("not signed in: /login corp") }}

		_, err := signedIn.ListModels(context.Background())
		_, outErr := signedOut.ListModels(context.Background())

		if err != nil || bearer != "Bearer tok-1" || outErr == nil || !strings.Contains(outErr.Error(), "/login corp") {
			t.Errorf("bearer %q err %v outErr %v", bearer, err, outErr)
		}
	})
}
