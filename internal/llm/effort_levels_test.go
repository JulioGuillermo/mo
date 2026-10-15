package llm

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | EL-01 | Happy    | llama-server's chat template says the efforts its model takes            | P0       |
// | EL-02 | Happy    | On llama-server, none turns thinking off, a level goes to the template  | P1       |
// | EL-03 | Error    | A server that refuses the effort gets the request again without it, and none after | P0 |
// | EL-04 | Edge     | Nothing said and no family known: every effort, not known                | P1       |

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestEffortLevels(t *testing.T) {
	props := func(template string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"chat_template": template})
		}))
	}

	t.Run("EL-01 llama-server's chat template says the efforts its model takes", func(t *testing.T) {
		for template, want := range map[string]string{
			"{% if reasoning_effort %}": "low,medium,high",
			"{% if enable_thinking %}":  "none",
			"{{ messages }}":            "",
		} {
			server := props(template)
			levels, known := (&OpenAI{BaseURL: server.URL + "/v1", PromptProgress: true}).Efforts(context.Background(), "m")
			server.Close()
			if strings.Join(levels, ",") != want || !known {
				t.Errorf("%s: levels = %v known = %v", template, levels, known)
			}
		}
	})

	t.Run("EL-02 on llama-server, none turns thinking off, a level goes to the template", func(t *testing.T) {
		llama := &OpenAI{PromptProgress: true}
		off := llama.buildRequest(Request{Model: "m", Effort: "none"})
		high := llama.buildRequest(Request{Model: "m", Effort: "high"})
		plain := (&OpenAI{}).buildRequest(Request{Model: "m", Effort: "high"})

		if off.ReasoningEffort != "" || off.ChatTemplateKwargs["enable_thinking"] != false {
			t.Errorf("off = %+v", off)
		}
		if high.ReasoningEffort != "high" || high.ChatTemplateKwargs["reasoning_effort"] != "high" {
			t.Errorf("high = %+v", high)
		}
		if plain.ChatTemplateKwargs != nil || plain.ReasoningEffort != "high" {
			t.Errorf("another server gets reasoning_effort alone: %+v", plain)
		}
	})

	t.Run("EL-03 a server that refuses the effort gets the request again without it, and none after", func(t *testing.T) {
		var bodies []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(body))
			if strings.Contains(string(body), "reasoning_effort") {
				http.Error(w, `{"error":{"message":"Unrecognized request argument supplied: reasoning_effort"}}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
		}))
		defer server.Close()
		provider := &OpenAI{BaseURL: server.URL + "/v1"}

		response, err := provider.Stream(context.Background(), Request{Model: "m", Effort: "high", Messages: []Message{{Role: RoleUser, Text: "hi"}}}, func(Event) {})
		levels, _ := provider.Efforts(context.Background(), "m")

		if err != nil || response.Message.Text != "ok" || len(bodies) != 2 || strings.Contains(bodies[1], "reasoning_effort") {
			t.Fatalf("err = %v text = %q bodies = %d", err, response.Message.Text, len(bodies))
		}
		if levels != nil {
			t.Errorf("a model that refused should take none: %v", levels)
		}
	})

	t.Run("EL-04 nothing said and no family known: every effort, not known", func(t *testing.T) {
		levels, known := (&OpenAI{BaseURL: "http://127.0.0.1:1/v1"}).Efforts(context.Background(), "some-model")
		gpt, gptKnown := (&OpenAI{}).Efforts(context.Background(), "gpt-5.2")

		if known || !slices.Equal(levels, AllEfforts) {
			t.Errorf("levels = %v known = %v", levels, known)
		}
		if !gptKnown || !slices.Contains(gpt, "minimal") {
			t.Errorf("gpt-5 = %v", gpt)
		}
	})
}
