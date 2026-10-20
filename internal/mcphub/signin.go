package mcphub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/netx"
)

// ErrNeedsLogin is a server that asks for a sign-in mo does not have.
var ErrNeedsLogin = errors.New("needs a sign-in")

// signIn puts the token of a server mo signed in to on its requests, and
// notes when the server refuses for want of one.
type signIn struct {
	source  *auth.Source
	client  *http.Client
	refused atomic.Bool
	// expired is the reason the token could not be given, when it could not.
	expired atomic.Value
}

func newSignIn(name string, settings config.MCPServer, base http.RoundTripper) *signIn {
	return &signIn{source: auth.MCPSource(name, settings.URL, settings.OAuth), client: &http.Client{Transport: base}}
}

// authorize sets the bearer token, renewed when it expired. A request that
// already carries an Authorization header, set in the config, keeps it.
func (s *signIn) authorize(req *http.Request) {
	if s == nil || s.source == nil || req.Header.Get("Authorization") != "" {
		return
	}
	ctx := context.WithValue(req.Context(), oauth2.HTTPClient, s.client)
	token, err := s.source.Token(ctx)
	if err != nil {
		s.expired.Store(err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

func (s *signIn) saw(response *http.Response) {
	if s != nil && response.StatusCode == http.StatusUnauthorized {
		s.refused.Store(true)
	}
}

// explain turns a failed connection to a server that refused for want of a
// sign-in into ErrNeedsLogin, saying how to sign in.
func (s *signIn) explain(name string, err error) error {
	if s == nil || !s.refused.Load() {
		return err
	}
	if reason, ok := s.expired.Load().(string); ok && reason != "" {
		return fmt.Errorf("%w: %s", ErrNeedsLogin, reason)
	}
	return fmt.Errorf("%w: /mcp login %s", ErrNeedsLogin, name)
}

// NeedsLogin tells whether a server's error is the want of a sign-in.
func NeedsLogin(err error) bool {
	return errors.Is(err, ErrNeedsLogin)
}

// Login signs in to a server reached by URL, as the MCP standard says,
// through its own certificates; show tells the user the page to open.
func Login(ctx context.Context, name string, settings config.MCPServer, show func(auth.Prompt)) error {
	if settings.URL == "" {
		return fmt.Errorf("%s is started by mo, not reached by URL: it takes no sign-in", name)
	}
	base, err := netx.Transport(settings.CA, settings.Insecure)
	if err != nil {
		return err
	}
	return auth.MCPLogin(ctx, name, settings.URL, settings.OAuth, &http.Client{Transport: base}, show)
}

// Logout forgets the sign-in to a server, telling whether there was one.
func Logout(name string) (bool, error) {
	return auth.MCPLogout(name)
}
