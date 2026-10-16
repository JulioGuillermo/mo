package detect

import (
	"errors"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/login"
)

// Suffix is what a provider that borrows another app's login is called
// after: antigravity-detected, claude-code-detected, codex-detected.
const Suffix = "-detected"

// fromLogins finds the sign-ins of the apps mo can borrow from. The
// provider is the app's own preset, taking the token from the app instead
// of signing in again.
func fromLogins() []Finding {
	var findings []Finding
	for _, from := range login.Sources() {
		app := login.Name(from)
		token, err := login.Read(from)
		switch {
		case errors.Is(err, login.ErrNeedsSQLite):
			findings = append(findings, Finding{Provider: from + Suffix, Origin: app + " login", Note: err.Error()})
			continue
		case err != nil:
			continue
		}
		settings := config.Defaults().Providers[from]
		settings.TokenFrom = from
		finding := Finding{Provider: from + Suffix, Settings: &settings, Origin: app + " login", Usable: true}
		if !token.Valid() {
			finding.Note = "expired: mo renews it"
			if login.Rotates(from) {
				finding.Note = "expired: open " + app + " to renew it, or let mo renew it in the provider's settings"
			}
		}
		findings = append(findings, finding)
	}
	return findings
}
