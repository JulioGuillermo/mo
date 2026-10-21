package mcphub

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | MD-01 | Happy    | What a server says it is for is kept, cut to its length with …       | P0       |
// | MD-02 | Happy    | Unset, the length is 2048; off, or a server that says nothing, is left out | P1  |

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jg/mo/internal/config"
)

func describedServer(t *testing.T, instructions string) string {
	t.Helper()
	server := func(*http.Request) *mcp.Server {
		return mcp.NewServer(&mcp.Implementation{Name: "described", Version: "1"}, &mcp.ServerOptions{Instructions: instructions})
	}
	web := httptest.NewServer(mcp.NewStreamableHTTPHandler(server, nil))
	t.Cleanup(web.Close)
	return web.URL
}

func TestDescriptions(t *testing.T) {
	long := strings.Repeat("x", 3000)

	t.Run("MD-01 what a server says it is for is kept, cut to its length with …", func(t *testing.T) {
		servers := map[string]config.MCPServer{
			"short": {URL: describedServer(t, "  Alerts and incidents.  ")},
			"cut":   {URL: describedServer(t, "Tickets of the team, and their sprints."), DescriptionChars: 10},
		}
		hub := Connect(context.Background(), servers, t.TempDir(), nil)
		defer hub.Close()

		got := hub.Descriptions()

		if got["short"] != "Alerts and incidents." || got["cut"] != "Tickets of…" {
			t.Errorf("descriptions = %q", got)
		}
	})

	t.Run("MD-02 unset, the length is 2048; off, or a server that says nothing, is left out", func(t *testing.T) {
		off := false
		servers := map[string]config.MCPServer{
			"long":   {URL: describedServer(t, long)},
			"off":    {URL: describedServer(t, "Hidden."), Description: &off},
			"silent": {URL: describedServer(t, "")},
		}
		hub := Connect(context.Background(), servers, t.TempDir(), nil)
		defer hub.Close()

		got := hub.Descriptions()

		if len(got) != 1 || len([]rune(got["long"])) != config.DefaultDescriptionChars+1 {
			t.Errorf("descriptions = %d, long is %d characters", len(got), len([]rune(got["long"])))
		}
	})
}
