package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | DL-01 | Happy    | A detected login keeps token_from and leaves the standard sign-in out    | P0       |
// | DL-02 | Security | Renewing a borrowed login is off unless it was turned on                 | P0       |
// | DL-03 | Edge     | Antigravity needs no renew option: its refresh token does not change     | P1       |
// | DL-04 | Happy    | Only the logins found, and the one in use, are offered to take from      | P1       |
// | DL-05 | Happy    | The detected list holds only what is usable                              | P1       |

import (
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
)

func loginFinding(from string) detect.Finding {
	settings := config.Defaults().Providers[from]
	settings.TokenFrom = from
	return detect.Finding{Provider: from + detect.Suffix, Settings: &settings, Usable: true}
}

func TestDetectedLogins(t *testing.T) {
	cfg := config.UserSettings(config.Config{})

	t.Run("DL-01 a detected login keeps token_from and leaves the standard sign-in out", func(t *testing.T) {
		findings := []detect.Finding{loginFinding(config.TokenFromClaudeCode)}
		draft := newProviderDraft("claude-code-detected", cfg, findings)

		layer, err := draft.applyTo(config.Config{})

		saved := layer.Providers["claude-code-detected"]
		if err != nil || saved.TokenFrom != config.TokenFromClaudeCode || saved.OAuth != nil || saved.APIKey != "" || saved.Protocol != "claude-code" {
			t.Errorf("saved = %+v, err = %v", saved, err)
		}
	})

	t.Run("DL-02 renewing a borrowed login is off unless it was turned on", func(t *testing.T) {
		findings := []detect.Finding{loginFinding(config.TokenFromCodex)}
		draft := newProviderDraft("codex-detected", cfg, findings)

		off, _ := draft.applyTo(config.Config{})
		draft.tokenRenew = true
		on, _ := draft.applyTo(config.Config{})

		if off.Providers["codex-detected"].TokenRenew || !on.Providers["codex-detected"].TokenRenew {
			t.Errorf("off = %+v, on = %+v", off.Providers["codex-detected"], on.Providers["codex-detected"])
		}
	})

	t.Run("DL-03 Antigravity needs no renew option: its refresh token does not change", func(t *testing.T) {
		findings := []detect.Finding{loginFinding(config.TokenFromAntigravity)}
		draft := newProviderDraft("antigravity-detected", cfg, findings)
		draft.tokenRenew = true

		layer, _ := draft.applyTo(config.Config{})

		if layer.Providers["antigravity-detected"].TokenRenew {
			t.Errorf("saved = %+v", layer.Providers["antigravity-detected"])
		}
		for _, each := range draft.entries(cfg) {
			if each.title == "Renew its login" {
				t.Error("the renew option is offered for Antigravity")
			}
		}
	})

	t.Run("DL-04 only the logins found, and the one in use, are offered to take from", func(t *testing.T) {
		findings := []detect.Finding{loginFinding(config.TokenFromCodex), {Provider: "claude-code-detected", Usable: false}}

		offered := foundLogins(findings, config.TokenFromAntigravity)

		if len(offered) != 2 || offered[0] != config.TokenFromCodex || offered[1] != config.TokenFromAntigravity {
			t.Errorf("offered = %v", offered)
		}
	})

	t.Run("DL-05 the detected list holds only what is usable", func(t *testing.T) {
		findings := []detect.Finding{loginFinding(config.TokenFromCodex), {Provider: "gemini-opencode", Usable: false}}

		options := detectedOptions(findings)

		if len(options) != 1 || options[0].Value != "codex-detected" {
			t.Errorf("options = %v", options)
		}
	})
}
