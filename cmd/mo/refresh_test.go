package main

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | RF-01 | Happy    | llama-server unchanged: the same model and window                           | P0       |
// | RF-02 | Happy    | llama-server restarted with another model and context: both are followed    | P0       |
// | RF-03 | Edge     | A name the user chose is kept while llama-server stays                      | P0       |
// | RF-04 | Security | A window set is never more than the server has                              | P0       |
// | RF-05 | Happy    | A provider that no longer offers the model says so once                     | P0       |
// | RF-06 | Edge     | A stable provider is asked only for a new conversation or after a failure   | P0       |
// | RF-07 | Edge     | models/ before a name and :latest after it are the same model               | P1       |
// | RF-08 | Edge     | A server that does not list its models says nothing                         | P1       |
// | RF-09 | Happy    | The window is read again where the server says it                           | P1       |

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

// fakeServer answers /props and /v1/models with what its fields hold now.
type fakeServer struct {
	mu       sync.Mutex
	models   []string
	ctx      int
	noList   bool
	requests atomic.Int32
	server   *httptest.Server
}

func newFakeServer(t *testing.T, models []string, ctxSize int) *fakeServer {
	t.Helper()
	f := &fakeServer{models: models, ctx: ctxSize}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/props":
			fmt.Fprintf(w, `{"default_generation_settings":{"n_ctx":%d}}`, f.ctx)
		case "/v1/models":
			if f.noList {
				http.NotFound(w, r)
				return
			}
			var entries []string
			for _, id := range f.models {
				entries = append(entries, fmt.Sprintf(`{"id":%q}`, id))
			}
			fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(entries, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeServer) restart(models []string, ctxSize int) {
	f.mu.Lock()
	f.models, f.ctx = models, ctxSize
	f.mu.Unlock()
}

func (f *fakeServer) provider(llama bool) *llm.OpenAI {
	return &llm.OpenAI{BaseURL: f.server.URL + "/v1", PromptProgress: llama}
}

func TestModelRefresher(t *testing.T) {
	ctx := context.Background()

	t.Run("RF-01 llama-server unchanged: the same model and window", func(t *testing.T) {
		server := newFakeServer(t, []string{"gemma.gguf"}, 32768)
		refresh := modelRefresher(chosenProvider{name: "llama-server", provider: server.provider(true)}, 0)

		check := refresh(ctx, agent.CheckTurn, "gemma.gguf", 32768)

		if check.Model != "gemma.gguf" || check.Window != 32768 || check.Notice != "" {
			t.Errorf("check = %+v", check)
		}
	})

	t.Run("RF-02 llama-server restarted with another model and context: both are followed", func(t *testing.T) {
		server := newFakeServer(t, []string{"gemma.gguf"}, 32768)
		refresh := modelRefresher(chosenProvider{name: "llama-server", provider: server.provider(true)}, 0)
		server.restart([]string{"qwen.gguf"}, 131072)

		check := refresh(ctx, agent.CheckTurn, "gemma.gguf", 32768)
		again := refresh(ctx, agent.CheckTurn, check.Model, check.Window)

		if check.Model != "qwen.gguf" || check.Window != 131072 || again.Model != "qwen.gguf" || again.Window != 131072 {
			t.Errorf("check = %+v again = %+v", check, again)
		}
	})

	t.Run("RF-03 a name the user chose is kept while llama-server stays", func(t *testing.T) {
		server := newFakeServer(t, []string{"gemma.gguf"}, 32768)
		refresh := modelRefresher(chosenProvider{name: "llama-server", provider: server.provider(true)}, 0)

		check := refresh(ctx, agent.CheckTurn, "my-alias", 32768)

		if check.Model != "my-alias" {
			t.Errorf("check = %+v", check)
		}
	})

	t.Run("RF-04 a window set is never more than the server has", func(t *testing.T) {
		server := newFakeServer(t, []string{"gemma.gguf"}, 32768)
		tooMuch := modelRefresher(chosenProvider{name: "llama-server", provider: server.provider(true)}, 65536)
		fits := modelRefresher(chosenProvider{name: "llama-server", provider: server.provider(true)}, 16384)

		if got := tooMuch(ctx, agent.CheckTurn, "gemma.gguf", 65536).Window; got != 32768 {
			t.Errorf("set above the server: %d", got)
		}
		if got := fits(ctx, agent.CheckTurn, "gemma.gguf", 16384).Window; got != 16384 {
			t.Errorf("set below the server: %d", got)
		}
	})

	t.Run("RF-05 a provider that no longer offers the model says so once", func(t *testing.T) {
		server := newFakeServer(t, []string{"a", "b"}, 0)
		refresh := modelRefresher(chosenProvider{name: "ollama", provider: server.provider(false)}, 0)

		first := refresh(ctx, agent.CheckNew, "gone", 8192)
		second := refresh(ctx, agent.CheckFailure, "gone", 8192)
		back := refresh(ctx, agent.CheckNew, "a", 8192)
		goneAgain := refresh(ctx, agent.CheckFailure, "gone", 8192)

		if !strings.Contains(first.Notice, "gone no longer appears") || !strings.Contains(first.Notice, "ollama") || second.Notice != "" || back.Notice != "" || goneAgain.Notice == "" {
			t.Errorf("first %q second %q back %q again %q", first.Notice, second.Notice, back.Notice, goneAgain.Notice)
		}
	})

	t.Run("RF-06 a stable provider is asked only for a new conversation or after a failure", func(t *testing.T) {
		server := newFakeServer(t, []string{"a"}, 0)
		refresh := modelRefresher(chosenProvider{name: "ollama", provider: server.provider(false)}, 0)

		refresh(ctx, agent.CheckTurn, "a", 8192)
		refresh(ctx, agent.CheckCompact, "a", 8192)
		quiet := server.requests.Load()
		refresh(ctx, agent.CheckNew, "a", 8192)
		afterNew := server.requests.Load()
		refresh(ctx, agent.CheckFailure, "a", 8192)

		if quiet != 0 || afterNew == 0 || server.requests.Load() == afterNew {
			t.Errorf("requests: %d, %d, %d", quiet, afterNew, server.requests.Load())
		}
	})

	t.Run("RF-07 models/ before a name and :latest after it are the same model", func(t *testing.T) {
		for _, id := range []string{"models/gemini-pro", "gemini-pro:latest", "gemini-pro"} {
			if !sameModel(id, "gemini-pro") {
				t.Errorf("%q is not gemini-pro", id)
			}
		}
		if sameModel("gemini-pro-2", "gemini-pro") {
			t.Error("another model matched")
		}
	})

	t.Run("RF-08 a server that does not list its models says nothing", func(t *testing.T) {
		server := newFakeServer(t, nil, 0)
		server.noList = true
		refresh := modelRefresher(chosenProvider{name: "custom", provider: server.provider(false)}, 0)

		if check := refresh(ctx, agent.CheckNew, "m", 8192); check.Notice != "" || check.Model != "m" || check.Window != 8192 {
			t.Errorf("check = %+v", check)
		}
	})

	t.Run("RF-09 the window is read again where the server says it", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				fmt.Fprint(w, `{"data":[{"id":"m","context_length":200000}]}`)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(server.Close)
		provider := &llm.OpenAI{BaseURL: server.URL + "/v1"}
		refresh := modelRefresher(chosenProvider{name: "router", provider: provider}, 0)
		set := modelRefresher(chosenProvider{name: "router", provider: provider}, 4096)

		if got := refresh(ctx, agent.CheckNew, "m", 8192).Window; got != 200000 {
			t.Errorf("window = %d", got)
		}
		if got := set(ctx, agent.CheckNew, "m", 4096).Window; got != 4096 {
			t.Errorf("set window = %d", got)
		}
	})
}
