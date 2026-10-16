package login

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | LG-01 | Happy    | Claude Code's login is read with its expiry                           | P0       |
// | LG-02 | Happy    | Codex's login is read, its expiry from the access token               | P0       |
// | LG-03 | Happy    | Antigravity's nested protobuf token is read                           | P0       |
// | LG-04 | Edge     | a missing or malformed login is os.ErrNotExist                        | P1       |
// | LG-05 | Happy    | writing a renewed Claude Code token keeps the rest of its file        | P0       |
// | LG-06 | Happy    | writing a renewed Codex token keeps the rest of its file              | P0       |
// | LG-07 | Security | writing keeps the file private to the user                            | P0       |

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func homesFor(t *testing.T) (claude, codex string) {
	t.Helper()
	home := t.TempDir()
	claude, codex = filepath.Join(home, "claude"), filepath.Join(home, "codex")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return claude, codex
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jwt(exp int64) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp})
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

func field(number int, value []byte) []byte {
	out := []byte{byte(number<<3 | 2), byte(len(value))}
	if len(value) >= 128 {
		out = []byte{byte(number<<3 | 2), byte(len(value)&0x7f | 0x80), byte(len(value) >> 7)}
	}
	return append(out, value...)
}

func varintField(number int, value uint64) []byte {
	out := []byte{byte(number << 3)}
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}

func antigravityValue(access, refresh string, expiry uint64) string {
	inner := append(field(1, []byte(access)), field(2, []byte("Bearer"))...)
	inner = append(inner, field(3, []byte(refresh))...)
	inner = append(inner, field(4, varintField(1, expiry))...)
	holder := field(1, []byte(base64.StdEncoding.EncodeToString(inner)))
	wrapper := append(field(1, []byte("oauthTokenInfoSentinelKey")), field(2, holder)...)
	return base64.StdEncoding.EncodeToString(field(1, wrapper))
}

func TestLogin(t *testing.T) {
	t.Run("LG-01 Claude Code's login is read with its expiry", func(t *testing.T) {
		claude, _ := homesFor(t)
		put(t, filepath.Join(claude, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"a","refreshToken":"r","expiresAt":4102444800000}}`)

		token, err := Read(Claude)

		if err != nil || token.AccessToken != "a" || token.RefreshToken != "r" || token.Expiry.Year() != 2100 {
			t.Errorf("token = %+v, err = %v", token, err)
		}
	})

	t.Run("LG-02 Codex's login is read, its expiry from the access token", func(t *testing.T) {
		_, codex := homesFor(t)
		put(t, filepath.Join(codex, "auth.json"), `{"tokens":{"access_token":"`+jwt(4102444800)+`","refresh_token":"r"}}`)

		token, err := Read(Codex)

		if err != nil || token.RefreshToken != "r" || token.Expiry.Year() != 2100 {
			t.Errorf("token = %+v, err = %v", token, err)
		}
	})

	t.Run("LG-03 Antigravity's nested protobuf token is read", func(t *testing.T) {
		token, err := parseAntigravity(antigravityValue("ya29.access", "1//refresh", 4102444800))

		if err != nil || token.AccessToken != "ya29.access" || token.RefreshToken != "1//refresh" || token.TokenType != "Bearer" || token.Expiry.Unix() != 4102444800 {
			t.Errorf("token = %+v, err = %v", token, err)
		}
	})

	t.Run("LG-04 a missing or malformed login is os.ErrNotExist", func(t *testing.T) {
		claude, _ := homesFor(t)
		put(t, filepath.Join(claude, ".credentials.json"), `not json`)

		for _, from := range []string{Claude, Codex} {
			if _, err := Read(from); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s: err = %v", from, err)
			}
		}
		if _, err := parseAntigravity("%%%"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("antigravity: err = %v", err)
		}
		if _, err := parseAntigravity(base64.StdEncoding.EncodeToString([]byte{0x0a, 0xff})); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("truncated: err = %v", err)
		}
	})

	t.Run("LG-05 writing a renewed Claude Code token keeps the rest of its file", func(t *testing.T) {
		claude, _ := homesFor(t)
		path := filepath.Join(claude, ".credentials.json")
		put(t, path, `{"mcpOAuth":{"x":1},"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r","expiresAt":1,"scopes":["a"]}}`)

		err := Write(Claude, &oauth2.Token{AccessToken: "new", RefreshToken: "new-r", Expiry: time.UnixMilli(4102444800000)})

		var file map[string]map[string]any
		data, _ := os.ReadFile(path)
		_ = json.Unmarshal(data, &file)
		entry := file["claudeAiOauth"]
		if err != nil || entry["accessToken"] != "new" || entry["refreshToken"] != "new-r" || entry["scopes"] == nil || file["mcpOAuth"]["x"] != float64(1) {
			t.Errorf("file = %s, err = %v", data, err)
		}
	})

	t.Run("LG-06 writing a renewed Codex token keeps the rest of its file", func(t *testing.T) {
		_, codex := homesFor(t)
		path := filepath.Join(codex, "auth.json")
		put(t, path, `{"auth_mode":"chatgpt","tokens":{"access_token":"old","refresh_token":"old-r","id_token":"i","account_id":"acc"}}`)

		err := Write(Codex, &oauth2.Token{AccessToken: "new", RefreshToken: "new-r"})

		var file struct {
			AuthMode string            `json:"auth_mode"`
			Tokens   map[string]string `json:"tokens"`
		}
		data, _ := os.ReadFile(path)
		_ = json.Unmarshal(data, &file)
		if err != nil || file.AuthMode != "chatgpt" || file.Tokens["access_token"] != "new" || file.Tokens["refresh_token"] != "new-r" || file.Tokens["account_id"] != "acc" || file.Tokens["id_token"] != "i" {
			t.Errorf("file = %s, err = %v", data, err)
		}
	})

	t.Run("LG-07 writing keeps the file private to the user", func(t *testing.T) {
		claude, _ := homesFor(t)
		path := filepath.Join(claude, ".credentials.json")
		put(t, path, `{"claudeAiOauth":{"accessToken":"old"}}`)

		if err := Write(Claude, &oauth2.Token{AccessToken: "new"}); err != nil {
			t.Fatal(err)
		}

		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", info.Mode().Perm())
		}
		if _, err := os.Stat(path + ".mo.tmp"); err == nil {
			t.Error("the temporary file was left behind")
		}
	})
}
