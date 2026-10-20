package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/config"
)

const (
	mcpDiscoveryTimeout = 20 * time.Second
	mcpClientName       = "mo"
	offlineAccess       = "offline_access"
)

var challengeField = regexp.MustCompile(`(\w+)="([^"]*)"`)

// MCPKey is the name an MCP server's token is kept under, apart from the
// providers'.
func MCPKey(server string) string {
	return "mcp-" + server
}

func mcpLoginCommand(server string) string {
	return "/mcp login " + server
}

// mcpClient is how mo signs in to an MCP server: the endpoints found and the
// client registered with them, kept beside the token.
type mcpClient struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes,omitempty"`
}

func mcpClientPath(server string) string {
	return filepath.Join(Dir(), MCPKey(server)+".client.json")
}

func loadMCPClient(server string) (mcpClient, bool) {
	data, err := os.ReadFile(mcpClientPath(server))
	if err != nil {
		return mcpClient{}, false
	}
	var client mcpClient
	if json.Unmarshal(data, &client) != nil || client.ClientID == "" || client.TokenURL == "" {
		return mcpClient{}, false
	}
	return client, true
}

func saveMCPClient(server string, client mcpClient) error {
	data, err := json.MarshalIndent(client, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(mcpClientPath(server), data, 0o600)
}

// settings are the OAuth settings of a client, what the config sets
// winning: its scopes and parameters.
func (c mcpClient) settings(resource string, overrides *config.OAuth) config.OAuth {
	settings := config.OAuth{
		Flow: config.OAuthBrowser, ClientID: c.ClientID, ClientSecret: c.ClientSecret,
		AuthURL: c.AuthURL, TokenURL: c.TokenURL, RedirectURL: c.RedirectURL, Scopes: c.Scopes, Resource: resource,
	}
	if overrides == nil {
		return settings
	}
	if len(overrides.Scopes) > 0 {
		settings.Scopes = overrides.Scopes
	}
	settings.Params = overrides.Params
	if overrides.Resource != "" {
		settings.Resource = overrides.Resource
	}
	return settings
}

// MCPSource gives the token of an MCP server mo signed in to, renewed when
// it expires; nil when it never signed in.
func MCPSource(server, resource string, overrides *config.OAuth) *Source {
	client, ok := loadMCPClient(server)
	if !ok {
		return nil
	}
	source := NewSource(MCPKey(server), client.settings(resource, overrides))
	source.login = mcpLoginCommand(server)
	return source
}

// MCPLogout forgets an MCP server's token and the client registered with
// it, telling whether there was one.
func MCPLogout(server string) (bool, error) {
	_, had := loadMCPClient(server)
	if err := Logout(MCPKey(server)); err != nil {
		return had, err
	}
	err := os.Remove(mcpClientPath(server))
	if errors.Is(err, os.ErrNotExist) {
		return had, nil
	}
	return true, err
}

// MCPLogin signs in to the MCP server at resource as the MCP standard says:
// it finds where the server signs in, registers mo there unless overrides
// names a client, and opens the browser with PKCE. client is how to reach
// those addresses, with the server's own certificates.
func MCPLogin(ctx context.Context, server, resource string, overrides *config.OAuth, client *http.Client, show func(Prompt)) error {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	registered, err := mcpClientFor(ctx, server, resource, overrides, client)
	if err != nil {
		return err
	}
	err = Login(ctx, MCPKey(server), registered.settings(resource, overrides), show)
	if err == nil || overrides != nil && overrides.ClientID != "" || !strings.Contains(err.Error(), "listening for the sign-in answer") {
		return err
	}
	// The port the client was registered with is taken: register again on
	// a free one.
	os.Remove(mcpClientPath(server))
	registered, err = mcpClientFor(ctx, server, resource, overrides, client)
	if err != nil {
		return err
	}
	return Login(ctx, MCPKey(server), registered.settings(resource, overrides), show)
}

// mcpClientFor is the client to sign in with: the one kept, the one the
// config names, or one registered now.
func mcpClientFor(ctx context.Context, server, resource string, overrides *config.OAuth, httpClient *http.Client) (mcpClient, error) {
	if kept, ok := loadMCPClient(server); ok && (overrides == nil || overrides.ClientID == "" || overrides.ClientID == kept.ClientID) {
		return kept, nil
	}
	found, err := discoverMCP(ctx, resource, httpClient)
	if err != nil {
		return mcpClient{}, err
	}
	client := mcpClient{AuthURL: found.AuthURL, TokenURL: found.TokenURL, Scopes: found.Scopes}
	if overrides != nil {
		client.AuthURL = orDefault(overrides.AuthURL, client.AuthURL)
		client.TokenURL = orDefault(overrides.TokenURL, client.TokenURL)
		client.RedirectURL = overrides.RedirectURL
	}
	if overrides != nil && overrides.ClientID != "" {
		// A client_id that is an https address is a client metadata
		// document: the server reads it, nothing is registered, and the
		// browser comes back to any port of localhost.
		client.ClientID, client.ClientSecret = overrides.ClientID, overrides.Secret()
	} else {
		if found.RegistrationURL == "" {
			return mcpClient{}, fmt.Errorf("%s does not register clients: set [mcp.%s.oauth] client_id", resource, server)
		}
		if client.RedirectURL == "" {
			client.RedirectURL, err = freeRedirect()
			if err != nil {
				return mcpClient{}, err
			}
		}
		client.ClientID, client.ClientSecret, err = register(ctx, httpClient, found.RegistrationURL, client.RedirectURL, authMethod(found.AuthMethods))
		if err != nil {
			return mcpClient{}, err
		}
	}
	return client, saveMCPClient(server, client)
}

func orDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// freeRedirect is an address on this computer whose port is free now, to
// register as the place the browser comes back to; localhost, as servers
// such as Atlassian's take it.
func freeRedirect() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return fmt.Sprintf("http://localhost:%d/callback", listener.Addr().(*net.TCPAddr).Port), nil
}

// discovered is where an MCP server signs in, the scopes it offers and
// how its clients may prove who they are at the token endpoint.
type discovered struct {
	AuthURL, TokenURL, RegistrationURL string
	Scopes, AuthMethods                []string
}

type protectedResource struct {
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authorizationServer struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
	AuthMethods           []string `json:"token_endpoint_auth_methods_supported"`
}

// discoverMCP finds where the server at resource signs in: the server's
// answer without a token names its metadata, which names the authorization
// server, whose metadata names the endpoints. An older server without
// metadata signs in at its own origin.
func discoverMCP(ctx context.Context, resource string, client *http.Client) (discovered, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpDiscoveryTimeout)
	defer cancel()
	address, err := neturl.Parse(resource)
	if err != nil || address.Host == "" {
		return discovered{}, fmt.Errorf("MCP url %q: %v", resource, err)
	}
	origin := address.Scheme + "://" + address.Host
	path := strings.TrimSuffix(address.Path, "/")
	challenge := askChallenge(ctx, client, resource)

	var metadata protectedResource
	candidates := []string{challenge["resource_metadata"], origin + "/.well-known/oauth-protected-resource" + path, origin + "/.well-known/oauth-protected-resource"}
	found := false
	for _, candidate := range candidates {
		if candidate != "" && getJSON(ctx, client, candidate, &metadata) == nil && len(metadata.AuthorizationServers) > 0 {
			found = true
			break
		}
	}
	issuer := origin
	if found {
		issuer = strings.TrimSuffix(metadata.AuthorizationServers[0], "/")
	}
	server, err := readAuthorizationServer(ctx, client, issuer)
	if err != nil && found {
		return discovered{}, err
	}
	if err != nil {
		server = authorizationServer{AuthorizationEndpoint: origin + "/authorize", TokenEndpoint: origin + "/token", RegistrationEndpoint: origin + "/register"}
	}
	result := discovered{AuthURL: server.AuthorizationEndpoint, TokenURL: server.TokenEndpoint, RegistrationURL: server.RegistrationEndpoint, AuthMethods: server.AuthMethods}
	result.Scopes = chosenScopes(challenge["scope"], metadata.ScopesSupported, server.ScopesSupported)
	return result, nil
}

// chosenScopes are the scopes to ask for, as the MCP standard says: those
// the server's challenge names, else those its metadata offers; with
// offline_access when offered, for a token that renews.
func chosenScopes(challenged string, resourceScopes, serverScopes []string) []string {
	scopes := strings.Fields(challenged)
	if len(scopes) == 0 {
		scopes = slices.Clone(resourceScopes)
	}
	offered := append(slices.Clone(resourceScopes), serverScopes...)
	if slices.Contains(offered, offlineAccess) && !slices.Contains(scopes, offlineAccess) {
		scopes = append(scopes, offlineAccess)
	}
	return scopes
}

// askChallenge sends the server an empty request and reads the fields of
// the WWW-Authenticate it answers with.
func askChallenge(ctx context.Context, client *http.Client, resource string) map[string]string {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, resource, strings.NewReader("{}"))
	if err != nil {
		return nil
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	return ChallengeFields(response.Header.Get("WWW-Authenticate"))
}

// ChallengeFields are the name="value" fields of a WWW-Authenticate header.
func ChallengeFields(header string) map[string]string {
	fields := map[string]string{}
	for _, match := range challengeField.FindAllStringSubmatch(header, -1) {
		fields[match[1]] = match[2]
	}
	return fields
}

// readAuthorizationServer reads an authorization server's metadata where
// the standards put it, and where some servers do.
func readAuthorizationServer(ctx context.Context, client *http.Client, issuer string) (authorizationServer, error) {
	address, err := neturl.Parse(issuer)
	if err != nil {
		return authorizationServer{}, err
	}
	origin := address.Scheme + "://" + address.Host
	path := strings.TrimSuffix(address.Path, "/")
	candidates := []string{
		origin + "/.well-known/oauth-authorization-server" + path,
		issuer + "/.well-known/oauth-authorization-server",
		origin + "/.well-known/openid-configuration" + path,
		issuer + "/.well-known/openid-configuration",
	}
	for _, candidate := range candidates {
		var server authorizationServer
		if getJSON(ctx, client, candidate, &server) == nil && server.AuthorizationEndpoint != "" && server.TokenEndpoint != "" {
			return server, nil
		}
	}
	return authorizationServer{}, fmt.Errorf("cannot read where %s signs in", issuer)
}

func getJSON(ctx context.Context, client *http.Client, url string, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(into)
}

// authMethod is how mo proves who it is at the token endpoint: with the
// secret it is given, in the request's body, when the server offers it, as
// Claude Code signs in to Atlassian; else as a public client, PKCE alone.
func authMethod(offered []string) string {
	if slices.Contains(offered, "client_secret_post") {
		return "client_secret_post"
	}
	return "none"
}

// register signs mo up as a client of the authorization server (RFC 7591),
// with PKCE, coming back to redirect, proving itself as method says.
func register(ctx context.Context, client *http.Client, url, redirect, method string) (string, string, error) {
	body, _ := json.Marshal(map[string]any{
		"client_name":                mcpClientName,
		"redirect_uris":              []string{redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("registering with %s: %w", url, err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 300 {
		return "", "", fmt.Errorf("registering with %s: %s: %s", url, response.Status, strings.TrimSpace(string(data)))
	}
	var registered struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthMethod   string `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(data, &registered); err != nil || registered.ClientID == "" {
		return "", "", fmt.Errorf("registering with %s: no client_id in the answer", url)
	}
	if registered.AuthMethod == "none" {
		registered.ClientSecret = ""
	}
	return registered.ClientID, registered.ClientSecret, nil
}

// MCPSignedIn tells whether mo keeps a sign-in to an MCP server.
func MCPSignedIn(server string) bool {
	_, ok := loadMCPClient(server)
	return ok
}
