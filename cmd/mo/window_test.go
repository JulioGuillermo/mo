package main

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | CW-01 | Happy    | The window set wins; else llama-server's, else the model's as its server says, else the default | P0 |
// | CW-02 | Happy    | chooseProvider uses detected opencode login when environment key is absent   | P0       |

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jg/mo/internal/config"
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

	t.Run("CW-02 chooseProvider uses detected opencode login when environment key is absent", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("OPENCODE_API_KEY", "")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
		}))
		t.Cleanup(server.Close)
		authPath := fmt.Sprintf("%s/opencode/auth.json", t.TempDir())
		t.Setenv("XDG_DATA_HOME", authPath[:len(authPath)-len("/opencode/auth.json")])
		_ = os.MkdirAll(fmt.Sprintf("%s/opencode", t.TempDir()), 0o755)
		writeFile := func(dir string) {
			_ = os.MkdirAll(dir+"/opencode", 0o755)
			_ = os.WriteFile(dir+"/opencode/auth.json", []byte(`{"opencode":{"type":"api","key":"sk-opencode-detected"}}`), 0o600)
		}
		dataHome := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dataHome)
		writeFile(dataHome)

		cfg := config.Defaults()
		provider := cfg.Providers["opencode"]
		provider.BaseURL = server.URL + "/v1"
		cfg.Providers["opencode"] = provider

		chosen, err := chooseProvider(context.Background(), cfg, "opencode", "m1")
		if err != nil {
			t.Fatal(err)
		}
		if chosen.name != "opencode" {
			t.Errorf("chosen.name = %q", chosen.name)
		}
	})
}
