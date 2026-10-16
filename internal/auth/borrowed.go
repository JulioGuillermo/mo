package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/login"
)

// Borrowed gives the token of a sign-in another app keeps, read from that
// app at each request so a renewal by the app is seen at once. mo keeps no
// copy of its own.
//
// An expired token is renewed only when it is safe or asked for: Google
// does not change Antigravity's refresh token, so mo renews it in memory
// and writes nothing; Claude Code's and Codex's change at each renewal, so
// renewing them takes renew, and the new token is written back for the app.
type Borrowed struct {
	from     string
	settings config.OAuth
	renew    bool
	mu       sync.Mutex
	renewed  *oauth2.Token
}

func NewBorrowed(from string, settings config.OAuth, renew bool) *Borrowed {
	return &Borrowed{from: from, settings: settings, renew: renew}
}

// TokenFor is the bearer source of a provider: a borrowed sign-in, or its
// own; nil when it signs in with neither.
func TokenFor(name string, settings config.ProviderConfig) func(context.Context) (string, error) {
	switch {
	case settings.TokenFrom != "":
		oauth := settings.OAuth
		if oauth == nil {
			oauth = config.Defaults().Providers[settings.TokenFrom].OAuth
		}
		if oauth == nil {
			return func(context.Context) (string, error) {
				return "", fmt.Errorf("%s: no way to renew the login of %s", name, login.Name(settings.TokenFrom))
			}
		}
		return NewBorrowed(settings.TokenFrom, *oauth, settings.TokenRenew).Token
	case settings.OAuth != nil:
		return NewSource(name, *settings.OAuth).WithHeaders(settings.Headers).Token
	}
	return nil
}

func (b *Borrowed) Token(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	app := login.Name(b.from)
	token, err := login.Read(b.from)
	switch {
	case errors.Is(err, login.ErrNeedsSQLite):
		return "", fmt.Errorf("%s: %w", app, err)
	case err != nil:
		return "", fmt.Errorf("%s: %w: sign in to %s", app, ErrNeedsLogin, app)
	case token.Valid():
		return token.AccessToken, nil
	case b.renewed.Valid():
		return b.renewed.AccessToken, nil
	case token.RefreshToken == "":
		return "", fmt.Errorf("%s: the login expired: %w again in %s", app, ErrNeedsLogin, app)
	case login.Rotates(b.from) && !b.renew:
		return "", fmt.Errorf("%s: the login expired: open %s to renew it, or let mo renew it in the provider's settings", app, app)
	}
	renewed, err := renew(ctx, b.settings, token)
	if err != nil {
		return "", fmt.Errorf("%s: renewing the login: %v", app, err)
	}
	if renewed.RefreshToken == "" {
		renewed.RefreshToken = token.RefreshToken
	}
	b.renewed = renewed
	if login.Rotates(b.from) {
		if err := login.Write(b.from, renewed); err != nil {
			return "", fmt.Errorf("%s: renewed, but could not keep it for %s: %w", app, app, err)
		}
	}
	return renewed.AccessToken, nil
}
