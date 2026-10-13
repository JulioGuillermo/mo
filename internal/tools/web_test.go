package tools

// Test Traceability Table
// | ID    | Category | Description                                                     | Priority |
// | WT-01 | Happy    | A long page is served 8 KB at a time with the offset to go on   | P0       |
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
	"github.com/jg/mo/internal/web"
)

func TestFetchTool(t *testing.T) {
	t.Run("WT-01 a long page is served 8 KB at a time with the offset to go on", func(t *testing.T) {
		page := web.Page{URL: "https://x", Title: "T", Markdown: strings.Repeat("a", fetchPageBytes+100)}

		first := pageWindow(page, 0)
		rest := pageWindow(page, fetchPageBytes)

		if !strings.HasPrefix(first, "# T\nhttps://x\n\n") || !strings.HasSuffix(first, fmt.Sprintf("[continues: offset=%d of %d]", fetchPageBytes, fetchPageBytes+100)) {
			t.Errorf("first ends with %q", first[len(first)-60:])
		}
		if strings.Contains(rest, "continues") || strings.Count(rest, "a") != 100 {
			t.Errorf("rest = %q", rest)
		}
	})

	t.Run("WT-02 an offset past the end says so", func(t *testing.T) {
		if text := pageWindow(web.Page{Markdown: "short"}, 99); !strings.Contains(text, "past the end") {
			t.Errorf("text = %q", text)
		}
	})

	t.Run("WT-03 reading on with an offset does not download the page again", func(t *testing.T) {
		downloads := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downloads++
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, strings.Repeat("b", fetchPageBytes*2))
		}))
		defer server.Close()
		tool := fetchTool()

		for _, offset := range []int{0, fetchPageBytes} {
			args, _ := json.Marshal(map[string]any{"url": server.URL, "offset": offset})
			if _, err := tool.Run(context.Background(), Env{}, args); err != nil {
				t.Fatal(err)
			}
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
