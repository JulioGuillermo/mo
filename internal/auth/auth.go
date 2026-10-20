// Package auth signs in to providers with OAuth 2 and keeps their tokens:
// the device flow, the browser with PKCE, or client credentials. Tokens are
// stored by provider, readable by the user only, and renewed with their
// refresh token when they expire.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/sysopen"
)

const loginTimeout = 10 * time.Minute

// ErrNeedsLogin is a provider with no token, or one that cannot be renewed.
var ErrNeedsLogin = errors.New("not signed in")

// Dir is where tokens are kept.
func Dir() string {
	return filepath.Join(config.DataHome(), "mo", "oauth")
}

func tokenPath(provider string) string {
	return filepath.Join(Dir(), provider+".json")
}

// oauthConfig is the client of settings. One with no secret, a public
// client, names itself in the request's body, as the standard says; so does
// an MCP server's client, registered to send its secret there.
func oauthConfig(settings config.OAuth, redirect string) *oauth2.Config {
	endpoint := oauth2.Endpoint{AuthURL: settings.AuthURL, TokenURL: settings.TokenURL, DeviceAuthURL: settings.DeviceURL}
	if settings.Secret() == "" || settings.Resource != "" {
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	return &oauth2.Config{
		ClientID: settings.ClientID, ClientSecret: settings.Secret(), Scopes: settings.Scopes, RedirectURL: redirect,
		Endpoint: endpoint,
	}
}

func params(settings config.OAuth) []oauth2.AuthCodeOption {
	var options []oauth2.AuthCodeOption
	if settings.Resource != "" {
		options = append(options, oauth2.SetAuthURLParam("resource", settings.Resource))
	}
	for key, value := range settings.Params {
		options = append(options, oauth2.SetAuthURLParam(key, value))
	}
	return options
}

// Load reads the token kept for a provider.
func Load(provider string) (*oauth2.Token, error) {
	data, err := os.ReadFile(tokenPath(provider))
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// Save keeps a provider's token, for the user only.
func Save(provider string, token *oauth2.Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	temporary := tokenPath(provider) + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, tokenPath(provider))
}

// Logout forgets a provider's token.
func Logout(provider string) error {
	err := os.Remove(tokenPath(provider))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Source gives a provider's token at each request: the one kept while it
// is valid, renewed and kept again when it expires; client credentials
// get a new one without the user.
type Source struct {
	provider string
	// login is the command that signs in again, told when it is needed.
	login    string
	settings config.OAuth
	headers  map[string]string
	mu       sync.Mutex
	token    *oauth2.Token

	exchangeMu     sync.Mutex
	exchanged      string
	exchangedUntil time.Time
}

func NewSource(provider string, settings config.OAuth) *Source {
	return &Source{provider: provider, settings: settings, login: "/login " + provider}
}

// WithHeaders sets the headers the exchange for the bearer is asked with.
func (s *Source) WithHeaders(headers map[string]string) *Source {
	s.headers = headers
	return s
}

// Token is the bearer to send: a valid access token, or what the provider
// gives in exchange for it. ErrNeedsLogin says how to get one.
func (s *Source) Token(ctx context.Context) (string, error) {
	access, err := s.accessToken(ctx)
	if err != nil || s.settings.ExchangeURL == "" {
		return access, err
	}
	return s.exchange(ctx, access)
}

func (s *Source) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token.Valid() {
		return s.token.AccessToken, nil
	}
	if s.settings.Flow == config.OAuthClient {
		token, err := (&clientcredentials.Config{
			ClientID: s.settings.ClientID, ClientSecret: s.settings.Secret(), TokenURL: s.settings.TokenURL, Scopes: s.settings.Scopes,
		}).Token(ctx)
		if err != nil {
			return "", fmt.Errorf("%s: client credentials: %w", s.provider, err)
		}
		s.token = token
		return token.AccessToken, nil
	}
	if stored, err := Load(s.provider); err == nil {
		s.token = stored
	}
	switch {
	case s.token == nil:
		return "", fmt.Errorf("%s: %w: %s", s.provider, ErrNeedsLogin, s.login)
	case s.token.Valid():
		return s.token.AccessToken, nil
	case s.token.RefreshToken == "":
		return "", fmt.Errorf("%s: the token expired: %w again: %s", s.provider, ErrNeedsLogin, s.login)
	}
	renewed, err := renew(ctx, s.settings, s.token)
	if err != nil {
		return "", fmt.Errorf("%s: renewing the token: %w: %v: %s", s.provider, ErrNeedsLogin, err, s.login)
	}
	if renewed.RefreshToken == "" {
		renewed.RefreshToken = s.token.RefreshToken
	}
	s.token = renewed
	if err := Save(s.provider, renewed); err != nil {
		return "", err
	}
	return renewed.AccessToken, nil
}

// renew trades a token's refresh token for a new token.
func renew(ctx context.Context, settings config.OAuth, token *oauth2.Token) (*oauth2.Token, error) {
	if settings.JSON {
		return jsonRefresh(ctx, settings, token.RefreshToken)
	}
	return oauthConfig(settings, "").TokenSource(ctx, token).Token()
}

// Prompt is what the user must do to sign in: open a page, and with the
// device flow enter a code there.
type Prompt struct {
	URL  string
	Code string
}

// Login signs in to a provider with its flow, telling the user what to do
// through show, and keeps the token.
func Login(ctx context.Context, provider string, settings config.OAuth, show func(Prompt)) error {
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	var token *oauth2.Token
	var err error
	switch settings.Flow {
	case config.OAuthClient:
		_, err = NewSource(provider, settings).Token(ctx)
		return err
	case config.OAuthBrowser:
		token, err = browserLogin(ctx, settings, show)
	case config.OAuthDevice, "":
		token, err = deviceLogin(ctx, settings, show)
	default:
		return fmt.Errorf("oauth flow %q: use device, browser or client_credentials", settings.Flow)
	}
	if err != nil {
		return err
	}
	return Save(provider, token)
}

func deviceLogin(ctx context.Context, settings config.OAuth, show func(Prompt)) (*oauth2.Token, error) {
	conf := oauthConfig(settings, "")
	response, err := conf.DeviceAuth(ctx, params(settings)...)
	if err != nil {
		return nil, fmt.Errorf("asking for a device code: %w", err)
	}
	url := response.VerificationURIComplete
	if url == "" {
		url = response.VerificationURI
	}
	show(Prompt{URL: url, Code: response.UserCode})
	return conf.DeviceAccessToken(ctx, response)
}

// browserLogin opens the authorization page and takes the code back on a
// port of the loopback, as the redirect, with PKCE.
func browserLogin(ctx context.Context, settings config.OAuth, show func(Prompt)) (*oauth2.Token, error) {
	address, path, redirect := "127.0.0.1:0", "/callback", ""
	if settings.RedirectURL != "" {
		fixed, err := neturl.Parse(settings.RedirectURL)
		if err != nil || fixed.Port() == "" {
			return nil, fmt.Errorf("oauth redirect_url %q: use http://localhost:<port>/<path>", settings.RedirectURL)
		}
		address, path, redirect = "127.0.0.1:"+fixed.Port(), fixed.Path, settings.RedirectURL
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listening for the sign-in answer: %w", err)
	}
	if redirect == "" {
		redirect = fmt.Sprintf("http://localhost:%d%s", listener.Addr().(*net.TCPAddr).Port, path)
	}
	conf := oauthConfig(settings, redirect)
	verifier := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier()
	codes := make(chan string, 1)
	failures := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		switch {
		case r.URL.Path != path:
			http.NotFound(w, r)
			return
		case query.Get("state") != state:
			http.Error(w, "unexpected state", http.StatusBadRequest)
			return
		case query.Get("error") != "":
			failures <- fmt.Errorf("sign-in refused: %s %s", query.Get("error"), query.Get("error_description"))
		default:
			codes <- query.Get("code")
		}
		fmt.Fprintln(w, "mo: you can close this page and go back to the terminal.")
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	options := append(params(settings), oauth2.S256ChallengeOption(verifier))
	if settings.Resource == "" {
		// Google's way to ask for a refresh token; an MCP server asks for
		// the standard offline_access scope instead, and may refuse it.
		options = append(options, oauth2.AccessTypeOffline)
	}
	url := conf.AuthCodeURL(state, options...)
	show(Prompt{URL: url})
	openBrowser(url)
	select {
	case code := <-codes:
		if settings.JSON {
			return jsonExchange(ctx, settings, map[string]string{
				"grant_type": "authorization_code", "code": code, "redirect_uri": redirect, "client_id": settings.ClientID,
				"code_verifier": verifier, "state": state,
			})
		}
		exchange := []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)}
		if settings.Resource != "" {
			exchange = append(exchange, oauth2.SetAuthURLParam("resource", settings.Resource))
		}
		return conf.Exchange(ctx, code, exchange...)
	case err := <-failures:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("sign-in not finished: %w", ctx.Err())
	}
}

// openBrowser opens a page in the user's browser; tests replace it.
var openBrowser = func(url string) {
	_ = sysopen.Open(url)
}
