package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | OL-01 | Happy    | The provider form sets a sign-in: flow, client, URLs and scopes go to the file | P0     |
// | OL-02 | Error    | A sign-in without a client id or a token URL is not kept                    | P1       |
// | OL-03 | Edge     | /login on a provider with a key says it takes a key                          | P2       |

import (
	"slices"
	"strings"
	"testing"

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
