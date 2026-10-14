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
		// Sign in with a GitHub account that has Copilot, as VS Code does.
		"copilot": {Protocol: "openai", BaseURL: "https://api.githubcopilot.com", Model: "gpt-4.1", Headers: map[string]string{
			"Editor-Version":         "vscode/1.99.3",
			"Editor-Plugin-Version":  "copilot-chat/0.26.7",
			"Copilot-Integration-Id": "vscode-chat",
			"User-Agent":             "GitHubCopilotChat/0.26.7",
		}, OAuth: &OAuth{
			Flow:        OAuthDevice,
			ClientID:    "Iv1.b507a08c87ecfe98",
			DeviceURL:   "https://github.com/login/device/code",
			TokenURL:    "https://github.com/login/oauth/access_token",
			ExchangeURL: "https://api.github.com/copilot_internal/v2/token",
			Scopes:      []string{"read:user"},
		}},
		// Microsoft's and GitHub's hosted models, with a GitHub token that may use them.
		"github-models": {Protocol: "openai", BaseURL: "https://models.github.ai/inference", APIKeyEnv: "GITHUB_TOKEN"},
		// OpenCode's own models.
		"opencode":     {Protocol: "openai", BaseURL: "https://opencode.ai/zen/v1", APIKeyEnv: "OPENCODE_API_KEY"},
		"opencode-zen": {Protocol: "openai", BaseURL: "https://opencode.ai/zen/v1", APIKeyEnv: "OPENCODE_API_KEY"},
		"xai":          {Protocol: "openai", BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY"},
		"together":     {Protocol: "openai", BaseURL: "https://api.together.xyz/v1", APIKeyEnv: "TOGETHER_API_KEY"},
		"fireworks":    {Protocol: "openai", BaseURL: "https://api.fireworks.ai/inference/v1", APIKeyEnv: "FIREWORKS_API_KEY"},
		"cerebras":     {Protocol: "openai", BaseURL: "https://api.cerebras.ai/v1", APIKeyEnv: "CEREBRAS_API_KEY"},
		"moonshot":     {Protocol: "openai", BaseURL: "https://api.moonshot.ai/v1", APIKeyEnv: "MOONSHOT_API_KEY"},
		"zai":          {Protocol: "openai", BaseURL: "https://api.z.ai/api/paas/v4", APIKeyEnv: "ZAI_API_KEY"},
		// Alibaba's DashScope, which serves Qwen.
		"qwen":        {Protocol: "openai", BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", APIKeyEnv: "DASHSCOPE_API_KEY"},
		"nvidia":      {Protocol: "openai", BaseURL: "https://integrate.api.nvidia.com/v1", APIKeyEnv: "NVIDIA_API_KEY"},
		"huggingface": {Protocol: "openai", BaseURL: "https://router.huggingface.co/v1", APIKeyEnv: "HF_TOKEN"},
	}
}
