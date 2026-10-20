package mcphub

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | MP-01 | Happy    | A plugin's server gets the headers its function gives on every request, over the static ones | P0 |
// | MP-02 | Error    | A header function that fails fails the connection with its error            | P1       |
// | MP-03 | Security | A server of the settings never asks the plugins for headers                 | P0       |
// | MP-04 | Happy    | WithoutPluginServers keeps only the servers of the settings                 | P1       |

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jg/mo/internal/config"
)

// headerServer is an MCP server by URL that keeps the Authorization and
// X-Static headers of each request.
func headerServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return demoServer(t) }, nil)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization")+"|"+r.Header.Get("X-Static"))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(web.Close)
	return web, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestPluginServers(t *testing.T) {
	t.Run("MP-01 a plugin's server gets the headers its function gives on every request, over the static ones", func(t *testing.T) {
		web, seen := headerServer(t)
		var calls atomic.Int32
		headers := func(_ context.Context, plugin, server, method, url string) (map[string]string, error) {
			calls.Add(1)
			if plugin != "jira" || server != "board" || method == "" || !strings.HasPrefix(url, web.URL) {
				return nil, errors.New("asked for the wrong server")
			}
			return map[string]string{"Authorization": "Bearer fresh", "X-Static": "overridden"}, nil
		}
		settings := config.MCPServer{URL: web.URL, Plugin: "jira", Headers: map[string]string{"X-Static": "kept", "Authorization": "Bearer stale"}}

		hub := Connect(context.Background(), map[string]config.MCPServer{"board": settings}, t.TempDir(), headers)
		t.Cleanup(hub.Close)

		status := hub.Status()[0]
		if status.Err != nil || status.Tools == 0 || calls.Load() == 0 {
			t.Fatalf("status = %+v calls = %d", status, calls.Load())
		}
		for _, each := range seen() {
			if each != "Bearer fresh|overridden" {
				t.Errorf("a request carried %q", each)
			}
		}
	})

	t.Run("MP-02 a header function that fails fails the connection with its error", func(t *testing.T) {
		web, _ := headerServer(t)
		headers := func(context.Context, string, string, string, string) (map[string]string, error) {
			return nil, errors.New("token expired")
		}

		hub := Connect(context.Background(), map[string]config.MCPServer{"board": {URL: web.URL, Plugin: "jira"}}, t.TempDir(), headers)
		t.Cleanup(hub.Close)

		if err := hub.Status()[0].Err; err == nil || !strings.Contains(err.Error(), "token expired") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("MP-03 a server of the settings never asks the plugins for headers", func(t *testing.T) {
		web, _ := headerServer(t)
		var asked atomic.Bool
		headers := func(context.Context, string, string, string, string) (map[string]string, error) {
			asked.Store(true)
			return nil, nil
		}

		hub := Connect(context.Background(), map[string]config.MCPServer{"own": {URL: web.URL}}, t.TempDir(), headers)
		t.Cleanup(hub.Close)

		if asked.Load() || hub.Status()[0].Err != nil {
			t.Errorf("asked = %v err = %v", asked.Load(), hub.Status()[0].Err)
		}
	})

	t.Run("MP-04 WithoutPluginServers keeps only the servers of the settings", func(t *testing.T) {
		hub := &Hub{servers: []*server{{name: "own"}, {name: "board", settings: config.MCPServer{Plugin: "jira"}}}}

		kept := hub.WithoutPluginServers()

		if len(kept.servers) != 1 || kept.servers[0].name != "own" || len(hub.servers) != 2 {
			t.Errorf("kept = %+v", kept.servers)
		}
	})
}
