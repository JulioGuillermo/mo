package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	KeyFromCodex          = "codex"
	keyFromOpencodePrefix = "opencode:"
	codexAPIKeyMode       = "apikey"
)

// KeyFrom reads an API key that another coding tool stored, at the moment it
// is needed. source is "codex" or "opencode:<provider>". Keys that come from
// a subscription login are never returned.
func KeyFrom(source string) string {
	if source == KeyFromCodex {
		return ReadCodexAuth().UsableKey()
	}
	if provider, ok := strings.CutPrefix(source, keyFromOpencodePrefix); ok {
		return OpencodeAuth()[provider].Key
	}
	return ""
}

func OpencodeKeyFrom(provider string) string {
	return keyFromOpencodePrefix + provider
}

func CodexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

type CodexAuth struct {
	AuthMode     string          `json:"auth_mode"`
	OpenAIAPIKey string          `json:"OPENAI_API_KEY"`
	Tokens       json.RawMessage `json:"tokens"`
}

func ReadCodexAuth() CodexAuth {
	var auth CodexAuth
	ReadJSON(filepath.Join(CodexHome(), "auth.json"), &auth)
	return auth
}

func (a CodexAuth) HasChatGPTLogin() bool {
	tokens := strings.TrimSpace(string(a.Tokens))
	return tokens != "" && tokens != "null"
}

// UsableKey is the stored API key, unless it may have been issued for the
// ChatGPT login stored next to it.
func (a CodexAuth) UsableKey() string {
	if a.HasChatGPTLogin() && a.AuthMode != codexAPIKeyMode {
		return ""
	}
	return a.OpenAIAPIKey
}

type OpencodeEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// OpencodeAuth returns opencode's stored logins by provider. Only entries of
// type "api" keep their key.
func OpencodeAuth() map[string]OpencodeEntry {
	auth := map[string]OpencodeEntry{}
	ReadJSON(filepath.Join(dataHome(), "opencode", "auth.json"), &auth)
	for name, entry := range auth {
		if entry.Type != "api" {
			entry.Key = ""
			auth[name] = entry
		}
	}
	return auth
}

func ConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// ReadJSON decodes the file into into and reports whether it could.
func ReadJSON(path string, into any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, into) == nil
}
