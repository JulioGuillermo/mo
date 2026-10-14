package tools

// Test Traceability Table
// | ID    | Category | Description                                                     | Priority |
// | WT-01 | Happy    | A long page comes 200 lines at a time with the offset to go on  | P0       |
// | WT-02 | Edge     | An offset past the end says so                                  | P2       |
// | WT-03 | Happy    | Reading on with an offset does not download the page again      | P1       |
// | WT-04 | Error    | web_search without a searcher says it is not configured         | P2       |
// | WT-05 | Happy    | Reading on in a fetched page does not ask again; a new URL does | P1       |

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestFetchTool(t *testing.T) {
	page := func(downloads *int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*downloads++
			w.Header().Set("Content-Type", "text/plain")
			for i := range 300 {
				fmt.Fprintf(w, "line %d\n", i+1)
			}
		}))
	}

	t.Run("WT-01 a long page comes 200 lines at a time with the offset to go on", func(t *testing.T) {
		downloads := 0
		server := page(&downloads)
		defer server.Close()

		first, _ := call(t, Env{}, "fetch", map[string]any{"url": server.URL})
		rest, _ := call(t, Env{}, "fetch", map[string]any{"url": server.URL, "offset": 200})

		if !strings.HasPrefix(first, server.URL+"\n\nline 1\n") || !strings.HasSuffix(first, "[lines 1-200 of 302; offset=200 continues]\n") {
			t.Errorf("first = %q", first[len(first)-80:])
		}
		if !strings.HasPrefix(rest, "line 199\n") || !strings.Contains(rest, "line 300\n[lines 201-302 of 302]") {
			t.Errorf("rest = %q", rest)
		}
	})

	t.Run("WT-02 an offset past the end says so", func(t *testing.T) {
		downloads := 0
		server := page(&downloads)
		defer server.Close()

		text, _ := call(t, Env{}, "fetch", map[string]any{"url": server.URL, "offset": 999})

		if !strings.Contains(text, "past the end") {
			t.Errorf("text = %q", text)
		}
	})

	t.Run("WT-03 reading on with an offset does not download the page again", func(t *testing.T) {
		downloads := 0
		server := page(&downloads)
		defer server.Close()
		registry := NewRegistry(Env{}, Options{})

		for _, offset := range []int{0, 200} {
			registry.Run(context.Background(), llm.ToolCall{Name: "fetch", Args: args(t, map[string]any{"url": server.URL, "offset": offset})})
		}

		if downloads != 1 {
			t.Errorf("downloads = %d", downloads)
		}
	})

	t.Run("WT-04 web_search without a searcher says it is not configured", func(t *testing.T) {
		_, err := webSearchTool(nil).Run(context.Background(), Env{}, json.RawMessage(`{"query":"x"}`))

		if err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestFetchConfirmation(t *testing.T) {
	t.Run("WT-05 reading on in a fetched page does not ask again; a new URL does", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "page")
		}))
		defer server.Close()
		registry := NewRegistry(Env{}, Options{})
		first, _ := json.Marshal(map[string]any{"url": server.URL})
		other, _ := json.Marshal(map[string]any{"url": server.URL + "/other"})

		before := registry.AlreadyConfirmed(llm.ToolCall{Name: "fetch", Args: first})
		registry.Run(context.Background(), llm.ToolCall{Name: "fetch", Args: first})
		after := registry.AlreadyConfirmed(llm.ToolCall{Name: "fetch", Args: first})
		elsewhere := registry.AlreadyConfirmed(llm.ToolCall{Name: "fetch", Args: other})

		if before || !after || elsewhere {
			t.Errorf("before = %v after = %v elsewhere = %v", before, after, elsewhere)
		}
	})
}
