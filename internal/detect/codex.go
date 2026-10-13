package detect

import (
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/jg/mo/internal/config"
)

const codexOrigin = "codex"

type codexProvider struct {
	BaseURL string `toml:"base_url"`
	EnvKey  string `toml:"env_key"`
	WireAPI string `toml:"wire_api"`
}

func fromCodex(cfg config.Config) []Finding {
	findings := codexLogins(cfg, config.ReadCodexAuth())
	var codexConfig struct {
		ModelProviders map[string]codexProvider `toml:"model_providers"`
	}
	if _, err := toml.DecodeFile(filepath.Join(config.CodexHome(), "config.toml"), &codexConfig); err == nil {
		for _, name := range slices.Sorted(maps.Keys(codexConfig.ModelProviders)) {
			findings = append(findings, codexCustomProvider(name, codexConfig.ModelProviders[name]))
		}
	}
	return findings
}

func codexLogins(cfg config.Config, auth config.CodexAuth) []Finding {
	var findings []Finding
	switch {
	case auth.UsableKey() != "":
		settings := withoutOwnKey(cfg.Providers["openai"])
		settings.APIKeyFrom = config.KeyFromCodex
		findings = append(findings, Finding{Provider: "openai-codex", Settings: &settings, Origin: codexOrigin + " API key", Usable: true})
	case auth.OpenAIAPIKey != "":
		findings = append(findings, Finding{Provider: "openai-codex", Origin: codexOrigin + " API key", Note: "stored with the ChatGPT login: not usable"})
	}
	if auth.HasChatGPTLogin() {
		findings = append(findings, Finding{Provider: "chatgpt", Origin: codexOrigin + " ChatGPT login", Note: "subscription login: not supported"})
	}
	return findings
}

func codexCustomProvider(name string, provider codexProvider) Finding {
	finding := Finding{Provider: "codex-" + name, Origin: codexOrigin + " config.toml"}
	if provider.WireAPI == "responses" {
		finding.Note = "uses the Responses API: not supported"
		return finding
	}
	if provider.BaseURL == "" {
		finding.Note = "no base_url"
		return finding
	}
	finding.Settings = &config.ProviderConfig{Protocol: "openai", BaseURL: provider.BaseURL, APIKeyEnv: provider.EnvKey}
	finding.Usable = provider.EnvKey == "" || os.Getenv(provider.EnvKey) != ""
	if !finding.Usable {
		finding.Note = provider.EnvKey + " is not set"
	}
	return finding
}

// withoutOwnKey keeps a preset's address but none of mo's own ways to get
// its key, so the imported key is the one used.
func withoutOwnKey(settings config.ProviderConfig) config.ProviderConfig {
	settings.APIKey = ""
	settings.APIKeyEnv = ""
	settings.APIKeyFrom = ""
	return settings
}
