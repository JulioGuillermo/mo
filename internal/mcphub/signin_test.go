package mcphub

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | MS-01 | Happy    | A server that refuses without a token needs a sign-in, said with /mcp login | P0     |
// | MS-02 | Happy    | Once signed in, every request carries the token and the tools come      | P0       |
// | MS-03 | Edge     | An Authorization header the config sets wins over the sign-in           | P1       |
// | MS-04 | Edge     | An expired sign-in that cannot renew needs a sign-in again              | P1       |

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
)

// guardedServer is an MCP server that wants the bearer token "good", and
// the Authorization headers it saw.
func guardedServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return demoServer(t) }, nil)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer good" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+"http://"+r.Host+`/.well-known/oauth-protected-resource"`)
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(web.Close)
	return web, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// signedIn keeps a sign-in to server as /mcp login would.
func signedIn(t *testing.T, server, tokenURL string, token *oauth2.Token) {
	t.Helper()
	client := `{"client_id":"c1","auth_url":"` + tokenURL + `","token_url":"` + tokenURL + `","redirect_url":"http://127.0.0.1:1/callback"}`
	writeFile(t, auth.Dir()+"/"+auth.MCPKey(server)+".client.json", client)
	if err := auth.Save(auth.MCPKey(server), token); err != nil {
		t.Fatal(err)
	}
}

func TestSignIn(t *testing.T) {
	ctx := context.Background()

	t.Run("MS-01 a server that refuses without a token needs a sign-in, said with /mcp login", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		web, _ := guardedServer(t)

		_, err := Test(ctx, "jira", config.MCPServer{URL: web.URL}, t.TempDir())

		if !NeedsLogin(err) || !strings.Contains(err.Error(), "/mcp login jira") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("MS-02 once signed in, every request carries the token and the tools come", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		web, seen := guardedServer(t)
		signedIn(t, "jira", web.URL+"/token", &oauth2.Token{AccessToken: "good", Expiry: time.Now().Add(time.Hour)})

		names, err := Test(ctx, "jira", config.MCPServer{URL: web.URL}, t.TempDir())

		if err != nil || !slices.Equal(names, []string{"fail", "greet"}) {
			t.Fatalf("names = %v err = %v", names, err)
		}
		for _, header := range seen() {
			if header != "Bearer good" {
				t.Errorf("a request went with %q", header)
			}
		}
	})

	t.Run("MS-03 an Authorization header the config sets wins over the sign-in", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		web, seen := guardedServer(t)
		signedIn(t, "jira", web.URL+"/token", &oauth2.Token{AccessToken: "stale", Expiry: time.Now().Add(time.Hour)})

		_, err := Test(ctx, "jira", config.MCPServer{URL: web.URL, Headers: map[string]string{"Authorization": "Bearer good"}}, t.TempDir())

		if err != nil || seen()[0] != "Bearer good" {
			t.Errorf("err = %v seen = %v", err, seen())
		}
	})

	t.Run("MS-04 an expired sign-in that cannot renew needs a sign-in again", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		web, _ := guardedServer(t)
		signedIn(t, "jira", web.URL+"/token", &oauth2.Token{AccessToken: "good", Expiry: time.Now().Add(-time.Hour)})

		_, err := Test(ctx, "jira", config.MCPServer{URL: web.URL}, t.TempDir())

		if !NeedsLogin(err) || !strings.Contains(err.Error(), "/mcp login jira") {
			t.Errorf("err = %v", err)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
