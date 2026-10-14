package auth

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AU-01 | Happy    | The device flow shows the code and the page, and keeps the token          | P0       |
// | AU-02 | Happy    | The browser flow takes the code on the loopback with PKCE and keeps the token | P0   |
// | AU-03 | Happy    | An expired token is renewed with its refresh token and kept again         | P0       |
// | AU-04 | Error    | Without a token the provider says to /login; logout forgets it            | P1       |
// | AU-05 | Happy    | Client credentials get a token without the user                           | P1       |

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/config"
)

// fakeServer is an OAuth server: device codes, authorization codes, client
// credentials and refresh tokens, each giving its own access token.
func fakeServer(t *testing.T) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var grants []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			fmt.Fprintf(w, `{"device_code":"dev-1","user_code":"ABCD-EFGH","verification_uri":"%s/activate","expires_in":60,"interval":1}`, "http://"+r.Host)
		case "/token":
			grants = append(grants, r.PostForm)
			token := map[string]string{
				"urn:ietf:params:oauth:grant-type:device_code": "from-device",
				"authorization_code":                           "from-browser",
				"client_credentials":                           "from-client",
				"refresh_token":                                "renewed",
			}[r.PostForm.Get("grant_type")]
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-1"}`, token)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &grants
}

func settingsFor(server *httptest.Server, flow string) config.OAuth {
	return config.OAuth{Flow: flow, ClientID: "mo", AuthURL: server.URL + "/authorize", TokenURL: server.URL + "/token", DeviceURL: server.URL + "/device", Scopes: []string{"models"}}
}

func TestLogin(t *testing.T) {
	t.Run("AU-01 the device flow shows the code and the page, and keeps the token", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		server, _ := fakeServer(t)
		var shown Prompt

		err := Login(context.Background(), "corp", settingsFor(server, config.OAuthDevice), func(prompt Prompt) { shown = prompt })

		token, _ := NewSource("corp", settingsFor(server, config.OAuthDevice)).Token(context.Background())
		info, _ := os.Stat(tokenPath("corp"))
		if err != nil || shown.Code != "ABCD-EFGH" || !strings.HasSuffix(shown.URL, "/activate") || token != "from-device" || info.Mode().Perm() != 0o600 {
			t.Errorf("err %v shown %+v token %q", err, shown, token)
		}
	})

	t.Run("AU-02 the browser flow takes the code on the loopback with PKCE and keeps the token", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		server, grants := fakeServer(t)
		openBrowser = func(page string) {
			parsed, _ := url.Parse(page)
			query := parsed.Query()
			callback := query.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(query.Get("state"))
			go http.Get(callback)
		}
		t.Cleanup(func() { openBrowser = func(string) {} })

		err := Login(context.Background(), "corp", settingsFor(server, config.OAuthBrowser), func(Prompt) {})

		token, _ := Load("corp")
		if err != nil || token == nil || token.AccessToken != "from-browser" {
			t.Fatalf("err %v token %+v", err, token)
		}
		if grant := (*grants)[0]; grant.Get("code") != "the-code" || grant.Get("code_verifier") == "" {
			t.Errorf("grant %v", grant)
		}
	})

	t.Run("AU-03 an expired token is renewed with its refresh token and kept again", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		server, _ := fakeServer(t)
		_ = Save("corp", &oauth2.Token{AccessToken: "old", RefreshToken: "refresh-0", Expiry: time.Now().Add(-time.Hour)})

		token, err := NewSource("corp", settingsFor(server, config.OAuthDevice)).Token(context.Background())

		kept, _ := Load("corp")
		if err != nil || token != "renewed" || kept.AccessToken != "renewed" || kept.RefreshToken == "" {
			t.Errorf("token %q err %v kept %+v", token, err, kept)
		}
	})

	t.Run("AU-04 without a token the provider says to /login; logout forgets it", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		server, _ := fakeServer(t)
		_ = Save("corp", &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)})

		before, _ := NewSource("corp", settingsFor(server, config.OAuthDevice)).Token(context.Background())
		_ = Logout("corp")
		_, err := NewSource("corp", settingsFor(server, config.OAuthDevice)).Token(context.Background())

		if before != "x" || !errors.Is(err, ErrNeedsLogin) || !strings.Contains(err.Error(), "/login corp") {
			t.Errorf("before %q err %v", before, err)
		}
	})

	t.Run("AU-05 client credentials get a token without the user", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		server, grants := fakeServer(t)
		settings := settingsFor(server, config.OAuthClient)
		settings.ClientSecret = "s3cret"

		token, err := NewSource("corp", settings).Token(context.Background())

		if err != nil || token != "from-client" || len(*grants) != 1 {
			t.Errorf("token %q err %v", token, err)
		}
	})

	t.Run("AU-06 the browser flow listens on the fixed redirect address the client has registered", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		server, _ := fakeServer(t)
		free, _ := net.Listen("tcp", "127.0.0.1:0")
		port := free.Addr().(*net.TCPAddr).Port
		free.Close()
		redirect := fmt.Sprintf("http://localhost:%d/oauth-callback", port)
		var asked string
		openBrowser = func(page string) {
			parsed, _ := url.Parse(page)
			query := parsed.Query()
			asked = query.Get("redirect_uri")
			go http.Get(redirect + "?code=c&state=" + url.QueryEscape(query.Get("state")))
		}
		t.Cleanup(func() { openBrowser = func(string) {} })
		settings := settingsFor(server, config.OAuthBrowser)
		settings.RedirectURL = redirect

		err := Login(context.Background(), "corp", settings, func(Prompt) {})

		token, _ := Load("corp")
		if err != nil || asked != redirect || token == nil {
			t.Errorf("err %v asked %q token %+v", err, asked, token)
		}
	})
}
