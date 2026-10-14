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
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/jg/mo/internal/config"
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

func oauthConfig(settings config.OAuth, redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: settings.ClientID, ClientSecret: settings.Secret(), Scopes: settings.Scopes, RedirectURL: redirect,
		Endpoint: oauth2.Endpoint{AuthURL: settings.AuthURL, TokenURL: settings.TokenURL, DeviceAuthURL: settings.DeviceURL},
	}
}

func params(settings config.OAuth) []oauth2.AuthCodeOption {
	var options []oauth2.AuthCodeOption
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
	settings config.OAuth
	mu       sync.Mutex
	token    *oauth2.Token
}

func NewSource(provider string, settings config.OAuth) *Source {
	return &Source{provider: provider, settings: settings}
}

// Token is a valid access token, or ErrNeedsLogin saying how to get one.
func (s *Source) Token(ctx context.Context) (string, error) {
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
		return "", fmt.Errorf("%s: %w: /login %s", s.provider, ErrNeedsLogin, s.provider)
	case s.token.Valid():
		return s.token.AccessToken, nil
	case s.token.RefreshToken == "":
		return "", fmt.Errorf("%s: the token expired: %w again: /login %s", s.provider, ErrNeedsLogin, s.provider)
	}
	renewed, err := oauthConfig(s.settings, "").TokenSource(ctx, s.token).Token()
	if err != nil {
		return "", fmt.Errorf("%s: renewing the token: %v: /login %s", s.provider, err, s.provider)
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
		redirect = fmt.Sprintf("http://%s%s", listener.Addr(), path)
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
	url := conf.AuthCodeURL(state, append(params(settings), oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))...)
	show(Prompt{URL: url})
	openBrowser(url)
	select {
	case code := <-codes:
		return conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	case err := <-failures:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("sign-in not finished: %w", ctx.Err())
	}
}

// openBrowser opens a page in the user's browser; tests replace it.
var openBrowser = func(url string) {
	command := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "windows":
		command = "rundll32"
		_ = exec.Command(command, "url.dll,FileProtocolHandler", url).Start()
		return
	}
	_ = exec.Command(command, url).Start()
}
