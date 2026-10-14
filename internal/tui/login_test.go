package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | OL-01 | Happy    | The provider form sets a sign-in: flow, client, URLs and scopes go to the file | P0     |
// | OL-02 | Error    | A sign-in without a client id or a token URL is not kept                    | P1       |
// | OL-03 | Edge     | /login on a provider with a key says it takes a key                          | P2       |
// | OL-10 | Happy    | A browser sign-in provider gets Log in, and Log out once signed in           | P1       |
// | OL-11 | Edge     | A provider with a key or client credentials has no sign-in button            | P2       |
// | OL-12 | Happy    | /login and a space list the providers that sign in                           | P1       |
// | OL-13 | Happy    | Tab on /login asks for the provider; tab on one writes it ready to run       | P1       |
// | OL-14 | Edge     | /logout lists only the providers that are signed in                          | P2       |

import (
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
)

func TestProviderSignIn(t *testing.T) {
	t.Run("OL-01 the provider form sets a sign-in: flow, client, URLs and scopes go to the file", func(t *testing.T) {
		cfg := config.UserSettings(config.Config{})
		draft := newProviderDraft(newProviderChoice, cfg, nil)
		draft.name, draft.baseURL = "corp", "https://llm.corp.example/v1"
		draft.oauth = config.OAuth{Flow: config.OAuthDevice, ClientID: "mo", TokenURL: "https://sso.corp.example/token", DeviceURL: "https://sso.corp.example/device"}
		draft.oauthScopes = "models  chat"

		layer, err := draft.applyTo(config.Config{})

		oauth := layer.Providers["corp"].OAuth
		if err != nil || oauth == nil || oauth.Flow != "device" || !slices.Equal(oauth.Scopes, []string{"models", "chat"}) || oauth.DeviceURL == "" {
			t.Fatalf("oauth %+v err %v", oauth, err)
		}
		titles := []string{}
		for _, field := range draft.entries(cfg) {
			titles = append(titles, field.title)
		}
		if !slices.Contains(titles, "Device URL") || slices.Contains(titles, "Authorization URL") {
			t.Errorf("fields %v", titles)
		}
		back := newProviderDraft("corp", config.UserSettings(layer), nil)
		if back.oauth.ClientID != "mo" || back.oauthScopes != "models chat" {
			t.Errorf("read back %+v %q", back.oauth, back.oauthScopes)
		}
	})

	t.Run("OL-02 a sign-in without a client id or a token URL is not kept", func(t *testing.T) {
		draft := newProviderDraft(newProviderChoice, config.UserSettings(config.Config{}), nil)
		draft.name, draft.baseURL = "corp", "https://llm.corp.example/v1"
		draft.oauth.Flow = config.OAuthBrowser

		_, err := draft.applyTo(config.Config{})

		if err == nil || !strings.Contains(err.Error(), "client id") {
			t.Errorf("err %v", err)
		}
	})

	t.Run("OL-03 /login on a provider with a key says it takes a key", func(t *testing.T) {
		m := conversationModel()
		m.session.Config = config.Defaults()
		m.session.Provider = "openai"

		updated, _ := m.runCommand("/login")

		if !strings.Contains(shown(updated.(model)), "openai takes a key, not a sign-in") {
			t.Errorf("shown: %q", shown(updated.(model)))
		}
	})
}

func TestSignInButtons(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	labels := func(actions []editorAction) []string {
		var out []string
		for _, action := range actions {
			out = append(out, action.label)
		}
		return out
	}
	cfg := config.Defaults()

	t.Run("OL-10 a provider that signs in with the browser gets Log in, and Log out once signed in", func(t *testing.T) {
		draft := newProviderDraft("antigravity", cfg, nil)
		if got := labels(draft.signInActions(cfg)); !slices.Equal(got, []string{"Log in"}) {
			t.Fatalf("buttons %v", got)
		}
		_ = auth.Save("antigravity", &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)})
		if got := labels(draft.signInActions(cfg)); !slices.Equal(got, []string{"Log in again", "Log out"}) {
			t.Fatalf("buttons %v", got)
		}
	})

	t.Run("OL-11 a provider with a key or with client credentials has no sign-in button", func(t *testing.T) {
		if actions := newProviderDraft("openai", cfg, nil).signInActions(cfg); len(actions) != 0 {
			t.Errorf("key: %v", labels(actions))
		}
		draft := newProviderDraft("antigravity", cfg, nil)
		draft.oauth.Flow = config.OAuthClient
		if actions := draft.signInActions(cfg); len(actions) != 0 {
			t.Errorf("client credentials: %v", labels(actions))
		}
	})
}

func TestLoginSuggestions(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	names := func(m model) []string {
		var out []string
		for _, match := range m.suggestions() {
			out = append(out, match.name)
		}
		return out
	}
	typing := func(text string) model {
		m := conversationModel()
		m.session.Config = config.Defaults()
		m.input.SetValue(text)
		return m
	}

	t.Run("OL-12 /login and a space list the providers that sign in", func(t *testing.T) {
		got := names(typing("/login "))
		if !slices.Equal(got, []string{"login antigravity", "login claude-code", "login codex"}) {
			t.Fatalf("suggestions %v", got)
		}
		if got := names(typing("/login co")); !slices.Equal(got, []string{"login codex"}) {
			t.Fatalf("filtered %v", got)
		}
		if got := names(typing("/title x")); got != nil {
			t.Fatalf("other commands %v", got)
		}
	})

	t.Run("OL-13 tab on /login asks for the provider, tab on one writes it ready to run", func(t *testing.T) {
		m := typing("/login")
		if matches := m.suggestions(); len(matches) != 1 || m.completeCommand(matches[0]) || m.input.Value() != "/login " {
			t.Fatalf("after tab: %q", m.input.Value())
		}
		matches := m.suggestions()
		if len(matches) == 0 || !m.completeCommand(matches[0]) || m.input.Value() != "/login antigravity" {
			t.Fatalf("after tab on a provider: %q", m.input.Value())
		}
	})

	t.Run("OL-14 /logout lists only the providers that are signed in", func(t *testing.T) {
		if got := names(typing("/logout ")); len(got) != 0 {
			t.Fatalf("nobody signed in: %v", got)
		}
		_ = auth.Save("codex", &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)})
		if got := names(typing("/logout ")); !slices.Equal(got, []string{"logout codex"}) {
			t.Fatalf("suggestions %v", got)
		}
	})
}
