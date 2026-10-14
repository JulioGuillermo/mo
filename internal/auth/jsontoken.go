package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/config"
)

// jsonRefresh renews a token at an endpoint that speaks JSON, as Claude's
// does; oauth2 only sends forms.
func jsonRefresh(ctx context.Context, settings config.OAuth, refresh string) (*oauth2.Token, error) {
	body := map[string]string{"grant_type": "refresh_token", "refresh_token": refresh, "client_id": settings.ClientID}
	if len(settings.Scopes) > 0 {
		body["scope"] = strings.Join(settings.Scopes, " ")
	}
	return jsonExchange(ctx, settings, body)
}

// jsonExchange posts a token request as JSON and reads the token from the
// answer.
func jsonExchange(ctx context.Context, settings config.OAuth, body map[string]string) (*oauth2.Token, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.TokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &answer); err != nil || answer.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint: no access token in the answer")
	}
	token := &oauth2.Token{AccessToken: answer.AccessToken, RefreshToken: answer.RefreshToken, TokenType: answer.TokenType}
	if answer.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	}
	return token, nil
}
