package auth

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | BW-01 | Happy    | a valid borrowed token is used as it is, and mo keeps none               | P0       |
// | BW-02 | Security | an expired Claude Code token is not renewed unless asked                 | P0       |
// | BW-03 | Happy    | with renewing on, the new token is used and written back for the app     | P0       |
// | BW-04 | Edge     | no login in the app is ErrNeedsLogin                                     | P1       |

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/login"
)

func claudeHome(t *testing.T, credentials string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func jsonServer(t *testing.T, calls *int) config.OAuth {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "renewed", "refresh_token": "rotated", "expires_in": 3600})
	}))
	t.Cleanup(server.Close)
	return config.OAuth{ClientID: "c", TokenURL: server.URL, JSON: true}
}

const validClaude = `{"claudeAiOauth":{"accessToken":"live","refreshToken":"r","expiresAt":4102444800000}}`
const expiredClaude = `{"other":true,"claudeAiOauth":{"accessToken":"stale","refreshToken":"old-r","expiresAt":1000}}`

func TestBorrowed(t *testing.T) {
	ctx := context.Background()

	t.Run("BW-01 a valid borrowed token is used as it is, and mo keeps none", func(t *testing.T) {
		claudeHome(t, validClaude)
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		calls := 0

		token, err := NewBorrowed(login.Claude, jsonServer(t, &calls), false).Token(ctx)

		if err != nil || token != "live" || calls != 0 {
			t.Errorf("token = %q, err = %v, calls = %d", token, err, calls)
		}
		if _, err := Load("claude-code-detected"); err == nil {
			t.Error("mo kept a token of its own")
		}
	})

	t.Run("BW-02 an expired Claude Code token is not renewed unless asked", func(t *testing.T) {
		path := claudeHome(t, expiredClaude)
		calls := 0

		_, err := NewBorrowed(login.Claude, jsonServer(t, &calls), false).Token(ctx)

		data, _ := os.ReadFile(path)
		if err == nil || !strings.Contains(err.Error(), "open Claude Code") || calls != 0 || string(data) != expiredClaude {
			t.Errorf("err = %v, calls = %d, file = %s", err, calls, data)
		}
	})

	t.Run("BW-03 with renewing on, the new token is used and written back for the app", func(t *testing.T) {
		path := claudeHome(t, expiredClaude)
		calls := 0
		source := NewBorrowed(login.Claude, jsonServer(t, &calls), true)

		token, err := source.Token(ctx)
		again, _ := source.Token(ctx)

		data, _ := os.ReadFile(path)
		if err != nil || token != "renewed" || again != "renewed" || calls != 1 {
			t.Errorf("token = %q, again = %q, err = %v, calls = %d", token, again, err, calls)
		}
		if !strings.Contains(string(data), `"rotated"`) || !strings.Contains(string(data), `"other"`) {
			t.Errorf("file = %s", data)
		}
	})

	t.Run("BW-04 no login in the app is ErrNeedsLogin", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

		_, err := NewBorrowed(login.Claude, config.OAuth{}, false).Token(ctx)

		if !errors.Is(err, ErrNeedsLogin) {
			t.Errorf("err = %v", err)
		}
	})
}
