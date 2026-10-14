package main

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | CW-01 | Happy    | The window set wins; else llama-server's, else the model's as its server says, else the default | P0 |

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestContextWindow(t *testing.T) {
	t.Run("CW-01 the window set wins; else llama-server's, else the model's as its server says, else the default", func(t *testing.T) {
		serve := func(paths map[string]string) *llm.OpenAI {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if answer, ok := paths[r.URL.Path]; ok {
					fmt.Fprint(w, answer)
					return
				}
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)
			return &llm.OpenAI{BaseURL: server.URL + "/v1"}
		}
		llama := serve(map[string]string{"/props": `{"default_generation_settings":{"n_ctx":65536}}`})
		listed := serve(map[string]string{"/v1/models": `{"data":[{"id":"m","context_length":200000}]}`})
		silent := serve(map[string]string{})

		set, setFrom, _ := openAIWindow(context.Background(), listed, "m", 8192)
		fromLlama, _, isLlama := openAIWindow(context.Background(), llama, "m", 0)
		fromList, listFrom, _ := openAIWindow(context.Background(), listed, "m", 0)
		fallback, fallbackFrom, _ := openAIWindow(context.Background(), silent, "m", 0)

		if set != 8192 || setFrom != windowFromSettings {
			t.Errorf("set %d %q", set, setFrom)
		}
		if fromLlama != 65536 || !isLlama || fromList != 200000 || listFrom != "the server's model list" {
			t.Errorf("llama %d %v list %d %q", fromLlama, isLlama, fromList, listFrom)
		}
		if fallback != defaultLocalWindow || fallbackFrom != windowFromDefault {
			t.Errorf("fallback %d %q", fallback, fallbackFrom)
		}
	})
}
