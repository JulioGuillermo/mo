package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const exchangeMargin = time.Minute

// exchange asks the provider for its own short-lived token in exchange for
// the access token, as GitHub Copilot does, and keeps it until it is about
// to expire.
func (s *Source) exchange(ctx context.Context, access string) (string, error) {
	s.exchangeMu.Lock()
	defer s.exchangeMu.Unlock()
	if s.exchanged != "" && time.Now().Add(exchangeMargin).Before(s.exchangedUntil) {
		return s.exchanged, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.settings.ExchangeURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	for name, value := range s.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Authorization", "token "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: asking for its token: %w", s.provider, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s: its token was refused (%s): %s; does the account have access? /login %s again", s.provider, resp.Status, strings.TrimSpace(string(data)), s.provider)
	}
	var answer struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &answer); err != nil || answer.Token == "" {
		return "", fmt.Errorf("%s: no token in the answer", s.provider)
	}
	s.exchanged = answer.Token
	s.exchangedUntil = time.Unix(answer.ExpiresAt, 0)
	if answer.ExpiresAt == 0 {
		s.exchangedUntil = time.Now().Add(10 * time.Minute)
	}
	return s.exchanged, nil
}
