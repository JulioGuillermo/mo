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
		"gemini":       {Protocol: "openai", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKeyEnv: "GEMINI_API_KEY"},
		"groq":         {Protocol: "openai", BaseURL: "https://api.groq.com/openai/v1", APIKeyEnv: "GROQ_API_KEY"},
		"deepseek":     {Protocol: "openai", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY"},
		"mistral":      {Protocol: "openai", BaseURL: "https://api.mistral.ai/v1", APIKeyEnv: "MISTRAL_API_KEY"},
		// Signs in as Antigravity does, with its own OAuth client.
		"antigravity": {Protocol: "cloudcode", BaseURL: "https://daily-cloudcode-pa.sandbox.googleapis.com", Model: "gemini-3-flash", OAuth: &OAuth{
			Flow:     OAuthBrowser,
			ClientID: "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
			// Public: it ships in Antigravity's app, as for any installed app.
			ClientSecret: "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",
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			RedirectURL:  "http://localhost:51121/oauth-callback",
			Params:       map[string]string{"prompt": "consent"},
			Scopes: []string{
				"https://www.googleapis.com/auth/cloud-platform",
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
				"https://www.googleapis.com/auth/cclog",
				"https://www.googleapis.com/auth/experimentsandconfigs",
			},
		}},
		// Sign in as Codex does, with a ChatGPT account.
		"codex": {Protocol: "responses", BaseURL: "https://chatgpt.com/backend-api/codex", Model: "gpt-5.6", OAuth: &OAuth{
			Flow:        OAuthBrowser,
			ClientID:    "app_EMoamEEZ73f0CkXaXp7hrann",
			AuthURL:     "https://auth.openai.com/oauth/authorize",
			TokenURL:    "https://auth.openai.com/oauth/token",
			RedirectURL: "http://localhost:1455/auth/callback",
			Scopes:      []string{"openid", "profile", "email", "offline_access", "api.connectors.read", "api.connectors.invoke"},
			Params:      map[string]string{"id_token_add_organizations": "true", "codex_cli_simplified_flow": "true", "originator": "codex_cli_rs"},
		}},
		// Sign in as Claude Code does, with a Claude account.
		"claude-code": {Protocol: "claude-code", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5-5", OAuth: &OAuth{
			Flow:     OAuthBrowser,
			JSON:     true,
			ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
			AuthURL:  "https://claude.com/cai/oauth/authorize",
			TokenURL: "https://platform.claude.com/v1/oauth/token",
			Scopes:   []string{"user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"},
			Params:   map[string]string{"code": "true"},
		}},
	}
}
