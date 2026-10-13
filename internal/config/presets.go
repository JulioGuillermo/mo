package config

// LocalPresets are tried in this order when no provider is configured.
var LocalPresets = []string{"llama-server", "ollama", "lmstudio"}

func presets() map[string]ProviderConfig {
	return map[string]ProviderConfig{
		"llama-server": {Protocol: "openai", BaseURL: "http://localhost:8080/v1"},
		"ollama":       {Protocol: "openai", BaseURL: "http://localhost:11434/v1"},
		"lmstudio":     {Protocol: "openai", BaseURL: "http://localhost:1234/v1"},
		"openai":       {Protocol: "openai", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
		"openrouter":   {Protocol: "openai", BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY"},
		"anthropic":    {Protocol: "anthropic", BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY"},
	}
}
