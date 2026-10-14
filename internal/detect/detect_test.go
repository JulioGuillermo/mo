package detect

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | DT-01 | Happy    | A provider key in the environment is a usable finding                | P0       |
// | DT-02 | Security | opencode OAuth logins are listed as not usable and give no key       | P0       |
// | DT-03 | Happy    | An opencode API login becomes a provider whose key is read from it   | P0       |
// | DT-04 | Happy    | codex's API key becomes openai-codex; its ChatGPT login is not usable| P0       |
// | DT-05 | Happy    | codex providers on chat completions are imported; responses are not  | P1       |
// | DT-06 | Security | an opencode provider with a literal key is not imported with it      | P0       |
// | DT-07 | Happy    | an opencode provider with {env:NAME} uses that variable              | P1       |
// | DT-08 | Happy    | a keyless openai provider that answers is a usable local server      | P1       |
// | DT-09 | Security | a codex key stored with a ChatGPT login is not usable                 | P0       |
// | DT-10 | Security | an imported provider never keeps mo's own literal api_key            | P0       |
// | DT-11 | Happy    | an opencode API login for opencode becomes the opencode provider      | P0       |

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jg/mo/internal/config"
)

type homes struct {
	home, codex, data string
}

func isolatedHomes(t *testing.T) homes {
	t.Helper()
	h := homes{home: t.TempDir(), codex: t.TempDir(), data: t.TempDir()}
	t.Setenv("HOME", h.home)
	t.Setenv("CODEX_HOME", h.codex)
	t.Setenv("XDG_DATA_HOME", h.data)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h.home, ".config"))
	for _, name := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY", "DEEPSEEK_API_KEY", "MISTRAL_API_KEY"} {
		t.Setenv(name, "")
	}
	return h
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func remoteOnly() config.Config {
	cfg := config.Defaults()
	for _, name := range config.LocalPresets {
		delete(cfg.Providers, name)
	}
	return cfg
}

func find(findings []Finding, provider string) *Finding {
	for i := range findings {
		if findings[i].Provider == provider {
			return &findings[i]
		}
	}
	return nil
}

func TestDetect(t *testing.T) {
	ctx := context.Background()

	t.Run("DT-01 a provider key in the environment is a usable finding", func(t *testing.T) {
		isolatedHomes(t)
		t.Setenv("GROQ_API_KEY", "gsk_test")

		found := find(Run(ctx, remoteOnly()), "groq")

		if found == nil || !found.Usable || found.Origin != "env GROQ_API_KEY" {
			t.Errorf("found = %+v", found)
		}
	})

	t.Run("DT-02 opencode OAuth logins are listed as not usable and give no key", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.data, "opencode", "auth.json"), `{"anthropic":{"type":"oauth","access":"secret-token","refresh":"r"}}`)

		found := find(Run(ctx, remoteOnly()), "anthropic-opencode")

		if found == nil || found.Usable || found.Settings != nil {
			t.Errorf("found = %+v", found)
		}
		if key := config.KeyFrom(config.OpencodeKeyFrom("anthropic")); key != "" {
			t.Errorf("an OAuth token was handed out as a key: %q", key)
		}
	})

	t.Run("DT-03 an opencode API login becomes a provider whose key is read from it", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.data, "opencode", "auth.json"), `{"openrouter":{"type":"api","key":"sk-or-test"}}`)

		found := find(Run(ctx, remoteOnly()), "openrouter-opencode")

		if found == nil || !found.Usable || found.Settings == nil {
			t.Fatalf("found = %+v", found)
		}
		if found.Settings.APIKey != "" || found.Settings.Key() != "sk-or-test" || found.Settings.BaseURL != "https://openrouter.ai/api/v1" {
			t.Errorf("settings = %+v", found.Settings)
		}
	})

	t.Run("DT-04 codex's API key becomes openai-codex; its ChatGPT login is not usable", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.codex, "auth.json"), `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-codex","tokens":{"access_token":"x"}}`)

		findings := Run(ctx, remoteOnly())

		codex := find(findings, "openai-codex")
		if codex == nil || !codex.Usable || codex.Settings.APIKeyFrom != config.KeyFromCodex || codex.Settings.Key() != "sk-codex" {
			t.Errorf("codex = %+v", codex)
		}
		if chatgpt := find(findings, "chatgpt"); chatgpt == nil || chatgpt.Usable {
			t.Errorf("chatgpt = %+v", chatgpt)
		}
	})

	t.Run("DT-05 codex providers on chat completions are imported; responses are not", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.codex, "config.toml"), `
[model_providers.box]
base_url = "http://box:8000/v1"
wire_api = "chat"

[model_providers.azure]
base_url = "https://x.openai.azure.com/openai"
wire_api = "responses"
`)

		findings := Run(ctx, remoteOnly())

		if box := find(findings, "codex-box"); box == nil || !box.Usable || box.Settings.BaseURL != "http://box:8000/v1" {
			t.Errorf("box = %+v", box)
		}
		if azure := find(findings, "codex-azure"); azure == nil || azure.Usable {
			t.Errorf("azure = %+v", azure)
		}
	})

	t.Run("DT-06 an opencode provider with a literal key is not imported with it", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.home, ".config", "opencode", "opencode.json"), `{"provider":{"vllm":{"options":{"baseURL":"http://gpu:8000/v1","apiKey":"literal-secret"}}}}`)

		found := find(Run(ctx, remoteOnly()), "opencode-vllm")

		if found == nil || found.Usable || found.Settings.APIKey != "" || found.Settings.Key() != "" {
			t.Errorf("found = %+v settings = %+v", found, found.Settings)
		}
	})

	t.Run("DT-07 an opencode provider with {env:NAME} uses that variable", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.home, ".config", "opencode", "opencode.json"), `{"provider":{"vllm":{"options":{"baseURL":"http://gpu:8000/v1","apiKey":"{env:VLLM_KEY}"}}}}`)

		found := find(Run(ctx, remoteOnly()), "opencode-vllm")

		if found == nil || !found.Usable || found.Settings.APIKeyEnv != "VLLM_KEY" {
			t.Errorf("found = %+v", found)
		}
	})

	t.Run("DT-08 a keyless openai provider that answers is a usable local server", func(t *testing.T) {
		isolatedHomes(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[{"id":"qwen"},{"id":"llama"}]}`)
		}))
		defer server.Close()
		cfg := remoteOnly()
		cfg.Providers["box"] = config.ProviderConfig{Protocol: "openai", BaseURL: server.URL}

		found := find(Run(ctx, cfg), "box")

		if found == nil || !found.Usable || found.Note != "2 models" {
			t.Errorf("found = %+v", found)
		}
	})
}

func TestImportedKeys(t *testing.T) {
	ctx := context.Background()

	t.Run("DT-09 a codex key stored with a ChatGPT login is not usable", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.codex, "auth.json"), `{"auth_mode":"chatgpt","OPENAI_API_KEY":"sk-minted","tokens":{"access_token":"x"}}`)

		found := find(Run(ctx, remoteOnly()), "openai-codex")

		if found == nil || found.Usable || found.Settings != nil {
			t.Errorf("found = %+v", found)
		}
		if key := config.KeyFrom(config.KeyFromCodex); key != "" {
			t.Errorf("KeyFrom returned %q", key)
		}
	})

	t.Run("DT-10 an imported provider never keeps mo's own literal api_key", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.codex, "auth.json"), `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-codex"}`)
		cfg := remoteOnly()
		openai := cfg.Providers["openai"]
		openai.APIKey = "sk-mo-own"
		cfg.Providers["openai"] = openai

		found := find(Run(ctx, cfg), "openai-codex")

		if found == nil || found.Settings.APIKey != "" || found.Settings.Key() != "sk-codex" {
			t.Errorf("settings = %+v", found.Settings)
		}
	})

	t.Run("DT-11 an opencode API login for opencode becomes the opencode provider", func(t *testing.T) {
		h := isolatedHomes(t)
		writeFile(t, filepath.Join(h.data, "opencode", "auth.json"), `{"opencode":{"type":"api","key":"sk-opencode-test"}}`)

		found := find(Run(ctx, remoteOnly()), "opencode")

		if found == nil || !found.Usable || found.Settings == nil {
			t.Fatalf("found = %+v", found)
		}
		if found.Settings.APIKey != "" || found.Settings.Key() != "sk-opencode-test" || found.Settings.BaseURL != "https://opencode.ai/zen/v1" {
			t.Errorf("settings = %+v", found.Settings)
		}
		if key := config.KeyFrom("opencode"); key != "sk-opencode-test" {
			t.Errorf("KeyFrom(opencode) = %q", key)
		}
	})
}
