package llm

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | WN-01 | Happy    | The model's entry in /models gives its context, in the names servers use      | P0       |
// | WN-02 | Happy    | Ollama gives the context the model is loaded with, else its num_ctx, else its most | P0  |
// | WN-03 | Happy    | LM Studio gives the loaded context, else the most                            | P1       |
// | WN-04 | Happy    | Gemini's own model API gives the input limit, with the key as key=            | P0       |
// | WN-05 | Happy    | Anthropic's Models API gives max_input_tokens                                 | P1       |
// | WN-06 | Edge     | A server that says nothing gives 0                                            | P1       |

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// windowServer answers the paths given with their JSON, and 404 the rest.
func windowServer(t *testing.T, answers map[string]string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		answer, ok := answers[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, answer)
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func TestModelWindow(t *testing.T) {
	t.Run("WN-01 the model's entry in /models gives its context, in the names servers use", func(t *testing.T) {
		for field, model := range map[string]string{"context_length": "a/b", "max_model_len": "qwen", "context_window": "llama-3"} {
			list, _ := json.Marshal(map[string]any{"data": []map[string]any{{"id": "other", field: 1}, {"id": model, field: 131072}}})
			server, _ := windowServer(t, map[string]string{"GET /v1/models": string(list)})

			size, from := (&OpenAI{BaseURL: server.URL + "/v1"}).ModelWindow(context.Background(), model)

			if size != 131072 || from != "the server's model list" {
				t.Errorf("%s: size %d from %q", field, size, from)
			}
		}
	})

	t.Run("WN-02 Ollama gives the context the model is loaded with, else its num_ctx, else its most", func(t *testing.T) {
		show := `{"parameters":"stop \"<|im_end|>\"\nnum_ctx 16384","model_info":{"qwen3.context_length":40960}}`
		loaded, _ := windowServer(t, map[string]string{"GET /api/ps": `{"models":[{"name":"qwen3:8b","context_length":8192}]}`, "POST /api/show": show})
		set, _ := windowServer(t, map[string]string{"GET /api/ps": `{"models":[]}`, "POST /api/show": show})
		bare, _ := windowServer(t, map[string]string{"POST /api/show": `{"model_info":{"qwen3.context_length":40960}}`})

		a, _ := (&OpenAI{BaseURL: loaded.URL + "/v1"}).ModelWindow(context.Background(), "qwen3:8b")
		b, _ := (&OpenAI{BaseURL: set.URL + "/v1"}).ModelWindow(context.Background(), "qwen3:8b")
		c, from := (&OpenAI{BaseURL: bare.URL + "/v1"}).ModelWindow(context.Background(), "qwen3:8b")

		if a != 8192 || b != 16384 || c != 40960 || from != "Ollama" {
			t.Errorf("loaded %d set %d bare %d from %q", a, b, c, from)
		}
	})

	t.Run("WN-03 LM Studio gives the loaded context, else the most", func(t *testing.T) {
		loaded, _ := windowServer(t, map[string]string{"GET /api/v0/models/qwen": `{"loaded_context_length":32768,"max_context_length":131072}`})
		idle, _ := windowServer(t, map[string]string{"GET /api/v0/models/qwen": `{"max_context_length":131072}`})

		a, from := (&OpenAI{BaseURL: loaded.URL + "/v1"}).ModelWindow(context.Background(), "qwen")
		b, _ := (&OpenAI{BaseURL: idle.URL + "/v1"}).ModelWindow(context.Background(), "qwen")

		if a != 32768 || b != 131072 || from != "LM Studio" {
			t.Errorf("loaded %d idle %d from %q", a, b, from)
		}
	})

	t.Run("WN-04 Gemini's own model API gives the input limit, with the key as key=", func(t *testing.T) {
		server, seen := windowServer(t, map[string]string{"GET /v1beta/models/gemini-3.5-flash-lite": `{"inputTokenLimit":1048576}`})
		gemini := &OpenAI{BaseURL: server.URL + "/v1beta/openai", APIKey: "AIza-test"}

		size := gemini.geminiWindow(context.Background(), "models/gemini-3.5-flash-lite")

		request := (*seen)[0]
		if size != 1048576 || request.URL.Query().Get("key") != "AIza-test" || request.Header.Get("Authorization") != "" {
			t.Errorf("size %d key %q auth %q", size, request.URL.Query().Get("key"), request.Header.Get("Authorization"))
		}
	})

	t.Run("WN-05 Anthropic's Models API gives max_input_tokens", func(t *testing.T) {
		server, _ := windowServer(t, map[string]string{"GET /v1/models/claude-opus-5-5": `{"id":"claude-opus-5-5","max_input_tokens":1000000}`})

		size := (&Anthropic{BaseURL: server.URL, APIKey: "sk-test"}).ModelWindow(context.Background(), "claude-opus-5-5")

		if size != 1000000 {
			t.Errorf("size %d", size)
		}
	})

	t.Run("WN-06 a server that says nothing gives 0", func(t *testing.T) {
		server, _ := windowServer(t, map[string]string{"GET /v1/models": `{"data":[{"id":"gpt-5"}]}`})

		size, from := (&OpenAI{BaseURL: server.URL + "/v1"}).ModelWindow(context.Background(), "gpt-5")

		if size != 0 || from != "" {
			t.Errorf("size %d from %q", size, from)
		}
	})
}
