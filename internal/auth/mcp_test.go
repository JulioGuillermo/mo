package auth

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AM-01 | Happy    | An MCP server's sign-in is found from its challenge, mo registers, and the token comes with PKCE and the resource, no access_type | P0 |
// | AM-02 | Happy    | The client registered is kept and reused at the next sign-in, and the token renews | P0 |
// | AM-03 | Happy    | A server without metadata signs in at its own origin                    | P1       |
// | AM-04 | Happy    | A client the config names is used instead of registering, with its scopes | P1     |
// | AM-05 | Edge     | The scopes are the challenge's, else the metadata's, with offline_access when offered | P1 |
// | AM-06 | Happy    | Logout forgets the token and the client; without them there is no source | P1      |

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/config"
)

// fakeMCPAuth is an MCP server that wants a sign-in, its authorization
// server on another path as Atlassian's is, and what each was asked.
type fakeMCPAuth struct {
	*httptest.Server
	mu            sync.Mutex
	registrations []map[string]any
	authorized    url.Values
	grants        []url.Values
	challenges    []string
	metadata      bool
}

func newFakeMCPAuth(t *testing.T, metadata bool) *fakeMCPAuth {
	t.Helper()
	fake := &fakeMCPAuth{metadata: metadata}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.Close)
	return fake
}

func (f *fakeMCPAuth) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := f.URL
	issuer := base + "/tenant-1"
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v2/mcp":
		challenge := `Bearer realm="mcp"`
		if f.metadata {
			challenge += `, resource_metadata="` + base + `/.well-known/oauth-protected-resource/v2/mcp", scope="read:jira write:jira"`
		}
		f.challenges = append(f.challenges, r.Header.Get("Authorization"))
		w.Header().Set("WWW-Authenticate", challenge)
		w.WriteHeader(http.StatusUnauthorized)
	case r.URL.Path == "/.well-known/oauth-protected-resource/v2/mcp" && f.metadata:
		fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q],"scopes_supported":["read:jira","write:jira","offline_access","read:goals"]}`, base+"/v2/mcp", issuer)
	case r.URL.Path == "/tenant-1/.well-known/oauth-authorization-server" && f.metadata:
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q}`, issuer, base+"/authorize", base+"/token", base+"/register")
	case r.URL.Path == "/register":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.registrations = append(f.registrations, body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"client_id":"client-%d","client_secret":"unused","token_endpoint_auth_method":"none"}`, len(f.registrations))
	case r.URL.Path == "/token":
		_ = r.ParseForm()
		f.grants = append(f.grants, r.PostForm)
		if r.PostForm.Get("grant_type") == "authorization_code" {
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != f.authorized.Get("code_challenge") {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant","error_description":"PKCE does not match"}`)
				return
			}
		}
		fmt.Fprint(w, `{"access_token":"access-1","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-1"}`)
	default:
		http.NotFound(w, r)
	}
}

// browser answers the sign-in page as a user who accepts would, and keeps
// what the page was asked.
func (f *fakeMCPAuth) browser(t *testing.T) {
	t.Helper()
	openBrowser = func(page string) {
		parsed, _ := url.Parse(page)
		f.mu.Lock()
		f.authorized = parsed.Query()
		f.mu.Unlock()
		query := parsed.Query()
		go http.Get(query.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(query.Get("state")))
	}
	t.Cleanup(func() { openBrowser = func(string) {} })
}

func TestMCPLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("AM-01 an MCP server's sign-in is found from its challenge, mo registers, and the token comes with PKCE and the resource, no access_type", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		fake := newFakeMCPAuth(t, true)
		fake.browser(t)
		resource := fake.URL + "/v2/mcp"

		err := MCPLogin(ctx, "jira", resource, nil, http.DefaultClient, func(Prompt) {})

		token, tokenErr := MCPSource("jira", resource, nil).Token(ctx)
		if err != nil || tokenErr != nil || token != "access-1" {
			t.Fatalf("err = %v token = %q (%v)", err, token, tokenErr)
		}
		registration := fake.registrations[0]
		redirect := registration["redirect_uris"].([]any)[0].(string)
		if !strings.HasPrefix(redirect, "http://127.0.0.1:") || registration["token_endpoint_auth_method"] != "none" {
			t.Errorf("registration = %v", registration)
		}
		asked := fake.authorized
		if asked.Get("client_id") != "client-1" || asked.Get("resource") != resource || asked.Get("code_challenge_method") != "S256" || asked.Has("access_type") ||
			asked.Get("scope") != "read:jira write:jira offline_access" || asked.Get("redirect_uri") != redirect {
			t.Errorf("authorize = %v", asked)
		}
		if grant := fake.grants[0]; grant.Get("resource") != resource || grant.Get("client_id") != "client-1" || grant.Get("client_secret") != "" {
			t.Errorf("grant = %v", grant)
		}
	})

	t.Run("AM-02 the client registered is kept and reused at the next sign-in, and the token renews", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		fake := newFakeMCPAuth(t, true)
		fake.browser(t)
		resource := fake.URL + "/v2/mcp"

		MCPLogin(ctx, "jira", resource, nil, http.DefaultClient, func(Prompt) {})
		err := MCPLogin(ctx, "jira", resource, nil, http.DefaultClient, func(Prompt) {})
		Save(MCPKey("jira"), &oauth2.Token{AccessToken: "old", RefreshToken: "refresh-1", Expiry: time.Now().Add(-time.Hour)})
		renewed, renewErr := MCPSource("jira", resource, nil).Token(ctx)

		if err != nil || len(fake.registrations) != 1 || renewErr != nil || renewed != "access-1" {
			t.Errorf("err = %v registrations = %d renewed = %q (%v)", err, len(fake.registrations), renewed, renewErr)
		}
		if last := fake.grants[len(fake.grants)-1]; last.Get("grant_type") != "refresh_token" || last.Get("client_id") != "client-1" {
			t.Errorf("refresh = %v", last)
		}
	})

	t.Run("AM-03 a server without metadata signs in at its own origin", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		fake := newFakeMCPAuth(t, false)
		fake.browser(t)

		err := MCPLogin(ctx, "old", fake.URL+"/v2/mcp", nil, http.DefaultClient, func(Prompt) {})

		if err != nil || len(fake.registrations) != 1 || fake.grants[0].Get("code") != "the-code" {
			t.Errorf("err = %v registrations = %d", err, len(fake.registrations))
		}
	})

	t.Run("AM-04 a client the config names is used instead of registering, with its scopes", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		fake := newFakeMCPAuth(t, true)
		fake.browser(t)
		overrides := &config.OAuth{ClientID: "mine", Scopes: []string{"read:jira"}, Params: map[string]string{"prompt": "consent"}}

		err := MCPLogin(ctx, "jira", fake.URL+"/v2/mcp", overrides, http.DefaultClient, func(Prompt) {})

		asked := fake.authorized
		if err != nil || len(fake.registrations) != 0 || asked.Get("client_id") != "mine" || asked.Get("scope") != "read:jira" || asked.Get("prompt") != "consent" {
			t.Errorf("err = %v registrations = %d authorize = %v", err, len(fake.registrations), asked)
		}
	})

	t.Run("AM-05 the scopes are the challenge's, else the metadata's, with offline_access when offered", func(t *testing.T) {
		challenged := chosenScopes("read write", []string{"read", "write", "admin", "offline_access"}, nil)
		offered := chosenScopes("", []string{"read", "admin"}, []string{"offline_access"})
		neither := chosenScopes("", nil, nil)

		if !slices.Equal(challenged, []string{"read", "write", "offline_access"}) || !slices.Equal(offered, []string{"read", "admin", "offline_access"}) || len(neither) != 0 {
			t.Errorf("challenged = %v offered = %v neither = %v", challenged, offered, neither)
		}
	})

	t.Run("AM-06 logout forgets the token and the client; without them there is no source", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		fake := newFakeMCPAuth(t, true)
		fake.browser(t)
		resource := fake.URL + "/v2/mcp"
		MCPLogin(ctx, "jira", resource, nil, http.DefaultClient, func(Prompt) {})

		had, err := MCPLogout("jira")
		again, _ := MCPLogout("jira")

		if !had || err != nil || again || MCPSource("jira", resource, nil) != nil {
			t.Errorf("had = %v err = %v again = %v", had, err, again)
		}
	})
}
