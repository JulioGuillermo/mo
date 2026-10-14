package detect

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/jg/mo/internal/config"
)

const opencodeOrigin = "opencode"

var opencodeToPreset = map[string]string{
	"google":       "gemini",
	"zen":          "opencode",
	"opencode-zen": "opencode",
}

var opencodeEnvReference = regexp.MustCompile(`^\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)

type opencodeProvider struct {
	Options struct {
		BaseURL string `json:"baseURL"`
		APIKey  string `json:"apiKey"`
	} `json:"options"`
}

func fromOpencode(cfg config.Config) []Finding {
	var findings []Finding
	auth := config.OpencodeAuth()
	for _, name := range slices.Sorted(maps.Keys(auth)) {
		findings = append(findings, opencodeLogin(cfg, name, auth[name]))
	}
	var opencodeConfig struct {
		Provider map[string]opencodeProvider `json:"provider"`
	}
	if config.ReadJSON(filepath.Join(config.ConfigHome(), "opencode", "opencode.json"), &opencodeConfig) {
		for _, name := range slices.Sorted(maps.Keys(opencodeConfig.Provider)) {
			if found, ok := opencodeCustomProvider(name, opencodeConfig.Provider[name]); ok {
				findings = append(findings, found)
			}
		}
	}
	return findings
}

func opencodeLogin(cfg config.Config, name string, entry config.OpencodeEntry) Finding {
	preset := name
	if mapped, ok := opencodeToPreset[name]; ok {
		preset = mapped
	}
	providerName := preset + "-opencode"
	origin := opencodeOrigin + " " + entry.Type + " login for " + name
	if preset == "opencode" {
		providerName = "opencode"
		origin = opencodeOrigin + " " + entry.Type + " login"
	}
	finding := Finding{Provider: providerName, Origin: origin}
	switch entry.Type {
	case "api":
	case "oauth":
		finding.Note = "subscription login (OAuth): not usable"
		return finding
	default:
		finding.Note = entry.Type + " login: not supported"
		return finding
	}
	settings, known := cfg.Providers[preset]
	if !known {
		finding.Note = "unknown provider: no base URL"
		return finding
	}
	settings = withoutOwnKey(settings)
	settings.APIKeyFrom = config.OpencodeKeyFrom(name)
	finding.Settings = &settings
	finding.Usable = true
	return finding
}

// opencodeCustomProvider imports a provider defined with its own baseURL.
// An apiKey must be an {env:NAME} reference; a literal key is not copied.
func opencodeCustomProvider(name string, provider opencodeProvider) (Finding, bool) {
	if provider.Options.BaseURL == "" {
		return Finding{}, false
	}
	settings := &config.ProviderConfig{Protocol: "openai", BaseURL: provider.Options.BaseURL}
	finding := Finding{Provider: "opencode-" + name, Settings: settings, Origin: opencodeOrigin + " opencode.json", Usable: true}
	if provider.Options.APIKey == "" {
		return finding, true
	}
	match := opencodeEnvReference.FindStringSubmatch(provider.Options.APIKey)
	if match == nil {
		finding.Usable = false
		finding.Note = "its API key is written in opencode.json; set it in mo's config"
		return finding, true
	}
	settings.APIKeyEnv = match[1]
	return finding, true
}
