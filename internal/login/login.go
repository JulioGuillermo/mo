// Package login reads the sign-ins other apps keep on this machine, Claude
// Code's, Codex's and Antigravity's, so mo can use them without a second
// sign-in. Reading changes nothing; only Write, for the apps whose refresh
// token changes at each renewal, puts a renewed token back.
package login

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/jg/mo/internal/config"
)

// The apps whose sign-in can be borrowed.
const (
	Claude      = config.TokenFromClaudeCode
	Codex       = config.TokenFromCodex
	Antigravity = config.TokenFromAntigravity
)

// ErrNeedsSQLite is Antigravity's login, kept in a database mo reads with
// the sqlite3 command, which is not installed.
var ErrNeedsSQLite = errors.New("the sqlite3 command is needed to read it")

// Sources are the apps mo looks in, in the order it lists them.
func Sources() []string { return []string{Antigravity, Claude, Codex} }

// Name is how the app is called.
func Name(from string) string {
	switch from {
	case Claude:
		return "Claude Code"
	case Codex:
		return "Codex"
	case Antigravity:
		return "Antigravity"
	}
	return from
}

// Rotates says whether the app's refresh token changes when it is used, so
// that renewing it for mo leaves the app with one that no longer works
// unless the new one is written back.
func Rotates(from string) bool { return from == Claude || from == Codex }

// Read is the token the app holds now; os.ErrNotExist when it has none.
func Read(from string) (*oauth2.Token, error) {
	switch from {
	case Claude:
		return readClaude()
	case Codex:
		return readCodex()
	case Antigravity:
		return readAntigravity()
	}
	return nil, fmt.Errorf("unknown login source %q", from)
}

// Write keeps a renewed token for the app, leaving the rest of its file as
// it was. Only Claude Code and Codex take one: Antigravity's refresh token
// does not change.
func Write(from string, token *oauth2.Token) error {
	switch from {
	case Claude:
		return writeClaude(token)
	case Codex:
		return writeCodex(token)
	}
	return fmt.Errorf("%s keeps its login in a place mo does not write", Name(from))
}

func claudePath() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".credentials.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", ".credentials.json")
}

func codexPath() string { return filepath.Join(config.CodexHome(), "auth.json") }

func readClaude() (*oauth2.Token, error) {
	var file struct {
		OAuth struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if !config.ReadJSON(claudePath(), &file) || file.OAuth.AccessToken == "" {
		return nil, os.ErrNotExist
	}
	token := &oauth2.Token{AccessToken: file.OAuth.AccessToken, RefreshToken: file.OAuth.RefreshToken, TokenType: "Bearer"}
	if file.OAuth.ExpiresAt > 0 {
		token.Expiry = time.UnixMilli(file.OAuth.ExpiresAt)
	}
	return token, nil
}

func readCodex() (*oauth2.Token, error) {
	auth := config.ReadCodexAuth()
	if !auth.HasChatGPTLogin() {
		return nil, os.ErrNotExist
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(auth.Tokens, &tokens) != nil || tokens.AccessToken == "" {
		return nil, os.ErrNotExist
	}
	return &oauth2.Token{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, TokenType: "Bearer", Expiry: jwtExpiry(tokens.AccessToken)}, nil
}

// jwtExpiry is the exp claim of a JWT, or the zero time, which never
// expires, when it has none.
func jwtExpiry(jwt string) time.Time {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

func writeClaude(token *oauth2.Token) error {
	return rewriteJSON(claudePath(), func(file map[string]any) error {
		entry, ok := file["claudeAiOauth"].(map[string]any)
		if !ok {
			return errors.New("no Claude Code login in the file")
		}
		entry["accessToken"] = token.AccessToken
		if token.RefreshToken != "" {
			entry["refreshToken"] = token.RefreshToken
		}
		if !token.Expiry.IsZero() {
			entry["expiresAt"] = token.Expiry.UnixMilli()
		}
		return nil
	})
}

func writeCodex(token *oauth2.Token) error {
	return rewriteJSON(codexPath(), func(file map[string]any) error {
		tokens, ok := file["tokens"].(map[string]any)
		if !ok {
			return errors.New("no ChatGPT login in the file")
		}
		tokens["access_token"] = token.AccessToken
		if token.RefreshToken != "" {
			tokens["refresh_token"] = token.RefreshToken
		}
		if id, ok := token.Extra("id_token").(string); ok && id != "" {
			tokens["id_token"] = id
		}
		file["last_refresh"] = time.Now().UTC().Format(time.RFC3339Nano)
		return nil
	})
}

// rewriteJSON changes a JSON file the way change says and replaces it at
// once, keeping its permissions and every field mo does not know.
func rewriteJSON(path string, change func(map[string]any) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	if err := change(file); err != nil {
		return err
	}
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temporary := path + ".mo.tmp"
	if err := os.WriteFile(temporary, out, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

const antigravityKey = "antigravityUnifiedStateSync.oauthToken"

// antigravityDatabase is the file Antigravity, a VS Code fork, keeps its
// state in.
func antigravityDatabase() string {
	dir := config.ConfigHome()
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support")
	}
	return filepath.Join(dir, "Antigravity", "User", "globalStorage", "state.vscdb")
}

func readAntigravity() (*oauth2.Token, error) {
	path := antigravityDatabase()
	if _, err := os.Stat(path); err != nil {
		return nil, os.ErrNotExist
	}
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, ErrNeedsSQLite
	}
	query := "select value from ItemTable where key='" + antigravityKey + "';"
	out, err := exec.Command(sqlite, "-readonly", path, query).Output()
	if err != nil {
		return nil, os.ErrNotExist
	}
	return parseAntigravity(strings.TrimSpace(string(out)))
}

// parseAntigravity reads Antigravity's token: protocol buffers inside
// base64, inside protocol buffers. The innermost message has the access
// token (1), its type (2), the refresh token (3) and a message whose first
// field is the expiry in seconds (4).
func parseAntigravity(stored string) (*oauth2.Token, error) {
	outer, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return nil, os.ErrNotExist
	}
	wrapper, _ := fieldBytes(outer, 1)
	holder, _ := fieldBytes(wrapper, 2)
	encoded, _ := fieldBytes(holder, 1)
	inner, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return nil, os.ErrNotExist
	}
	access, _ := fieldBytes(inner, 1)
	if len(access) == 0 {
		return nil, os.ErrNotExist
	}
	kind, _ := fieldBytes(inner, 2)
	refresh, _ := fieldBytes(inner, 3)
	token := &oauth2.Token{AccessToken: string(access), RefreshToken: string(refresh), TokenType: string(kind)}
	if expiry, ok := fieldBytes(inner, 4); ok {
		if seconds, ok := fieldVarint(expiry, 1); ok && seconds > 0 {
			token.Expiry = time.Unix(int64(seconds), 0)
		}
	}
	return token, nil
}

// fieldBytes is the first length-delimited field with that number.
func fieldBytes(message []byte, number uint64) ([]byte, bool) {
	var found []byte
	var ok bool
	walk(message, func(field, wire uint64, value []byte, _ uint64) bool {
		if field == number && wire == 2 {
			found, ok = value, true
			return false
		}
		return true
	})
	return found, ok
}

// fieldVarint is the first varint field with that number.
func fieldVarint(message []byte, number uint64) (uint64, bool) {
	var found uint64
	var ok bool
	walk(message, func(field, wire uint64, _ []byte, value uint64) bool {
		if field == number && wire == 0 {
			found, ok = value, true
			return false
		}
		return true
	})
	return found, ok
}

// walk calls visit for each field of a message until it says to stop, and
// gives up at the first thing that is not well formed.
func walk(message []byte, visit func(field, wire uint64, value []byte, number uint64) bool) {
	for len(message) > 0 {
		key, n := varint(message)
		if n == 0 {
			return
		}
		message = message[n:]
		field, wire := key>>3, key&7
		switch wire {
		case 0:
			value, n := varint(message)
			if n == 0 || !visit(field, wire, nil, value) {
				return
			}
			message = message[n:]
		case 2:
			size, n := varint(message)
			if n == 0 || uint64(len(message)-n) < size {
				return
			}
			if !visit(field, wire, message[n:n+int(size)], 0) {
				return
			}
			message = message[n+int(size):]
		case 1:
			if len(message) < 8 {
				return
			}
			message = message[8:]
		case 5:
			if len(message) < 4 {
				return
			}
			message = message[4:]
		default:
			return
		}
	}
}

func varint(data []byte) (uint64, int) {
	var value uint64
	for i := 0; i < len(data) && i < 10; i++ {
		value |= uint64(data[i]&0x7f) << (7 * i)
		if data[i] < 0x80 {
			return value, i + 1
		}
	}
	return 0, 0
}
