package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | PV-01 | Happy    | A new provider is saved whole and, when asked, used from then on     | P0       |
// | PV-02 | Happy    | Editing a preset saves only what differs from it                     | P1       |
// | PV-03 | Security | An empty key keeps the saved one; forgetting it clears it            | P0       |
// | PV-04 | Edge     | Deleting the provider in use goes back to auto; a preset is reset    | P1       |
// | PV-05 | Error    | An Anthropic provider without a model is refused                     | P1       |
// | PV-06 | Security | Editing a detected provider keeps where its key comes from, not a key | P0      |
// | MC-01 | Happy    | A started server keeps its arguments and environment line by line    | P0       |
// | MC-02 | Happy    | A server reached by URL keeps its headers                            | P1       |
// | MC-03 | Error    | A server needs exactly one of command and URL                        | P1       |
// | MC-04 | Error    | A line without its separator is refused with the field name          | P2       |
// | MC-05 | Happy    | A deleted server leaves the file                                     | P1       |
// | MC-06 | Security | Editing shows the saved server as lines, literal values hidden and kept | P0    |
// | SU-01 | Happy    | Setup with a known provider saves the typed key and model on it      | P0       |
// | SU-02 | Happy    | Setup with a new provider saves it and uses it                       | P0       |
// | SU-03 | Happy    | The ways out are offered: go on only when the provider was built     | P1       |
// | SD-19 | Happy    | Untouched web, display, prompts and token fields stay out of the file | P0      |
// | SD-20 | Happy    | Changed web, display, prompts and token fields are saved             | P1       |
// | SD-21 | Happy    | Descriptions of added tools are edited; saved ones of absent tools stay | P1    |
// | SD-22 | Security | A saved search key can be forgotten; an empty field keeps it         | P1       |
// | PV-07 | Security | Keys are offered only from API logins, never subscription logins     | P0       |
// | PV-08 | Edge     | The provider in use takes the top-level model as its own             | P1       |
// | SU-04 | Happy    | Setup asks the key of a provider that needs one, also when one is set | P1      |
// | SU-05 | Edge     | Setup leaves a top-level model behind when it switches provider      | P1       |

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
)

func TestProviderForm(t *testing.T) {
	cfg := config.UserSettings(config.Config{Provider: "llama-server"})

	t.Run("PV-01 a new provider is saved whole and, when asked, used from then on", func(t *testing.T) {
		draft := newProviderDraft(newProviderChoice, cfg, nil)
		draft.name, draft.baseURL, draft.keyEnv = "my-vllm", "http://gpu:8000/v1", "VLLM_KEY"

		layer, err := draft.applyTo(config.Config{Provider: "llama-server"})

		saved := layer.Providers["my-vllm"]
		if err != nil || layer.Provider != "my-vllm" || saved.Protocol != "openai" || saved.BaseURL != "http://gpu:8000/v1" || saved.APIKeyEnv != "VLLM_KEY" || saved.APIKey != "" {
			t.Errorf("layer = %+v err = %v", layer, err)
		}
	})

	t.Run("PV-02 editing a preset saves only what differs from it", func(t *testing.T) {
		draft := newProviderDraft("ollama", cfg, nil)
		draft.baseURL = "http://box:11434/v1"

		layer, _ := draft.applyTo(config.Config{Provider: "llama-server"})

		if saved := layer.Providers["ollama"]; !reflect.DeepEqual(saved, config.ProviderConfig{BaseURL: "http://box:11434/v1"}) || layer.Provider != "llama-server" {
			t.Errorf("saved = %+v provider = %q", saved, layer.Provider)
		}
	})

	t.Run("PV-03 an empty key keeps the saved one; forgetting it clears it", func(t *testing.T) {
		file := config.Config{Providers: map[string]config.ProviderConfig{"openai": {APIKey: "sk-old"}}}
		withKey := config.UserSettings(file)
		kept, _ := newProviderDraft("openai", withKey, nil).applyTo(file)
		forget := newProviderDraft("openai", withKey, nil)
		forget.forgetKey = true
		forgotten, _ := forget.applyTo(file)

		if kept.Providers["openai"].APIKey != "sk-old" || forgotten.Providers["openai"].APIKey != "" || !forget.savedKey {
			t.Errorf("kept = %+v forgotten = %+v", kept.Providers["openai"], forgotten.Providers["openai"])
		}
	})

	t.Run("PV-04 deleting the provider in use goes back to auto; a preset is reset", func(t *testing.T) {
		file := config.Config{Provider: "mine", Providers: map[string]config.ProviderConfig{
			"mine":   {Protocol: "openai", BaseURL: "http://x/v1"},
			"ollama": {BaseURL: "http://box:11434/v1"},
		}}
		mine := newProviderDraft("mine", config.UserSettings(file), nil)
		mine.remove = true
		withoutMine, _ := mine.applyTo(file)
		ollama := newProviderDraft("ollama", config.UserSettings(file), nil)
		ollama.remove = true
		reset, _ := ollama.applyTo(file)

		if _, left := withoutMine.Providers["mine"]; left || withoutMine.Provider != autoProvider {
			t.Errorf("without mine = %+v", withoutMine)
		}
		if _, left := reset.Providers["ollama"]; left || reset.Provider != "mine" {
			t.Errorf("reset = %+v", reset)
		}
	})

	t.Run("PV-05 an Anthropic provider without a model is refused", func(t *testing.T) {
		draft := newProviderDraft(newProviderChoice, cfg, nil)
		draft.name, draft.protocol = "claude", "anthropic"

		_, err := draft.applyTo(config.Config{})

		if err == nil || !strings.Contains(err.Error(), "needs a model") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("PV-06 editing a detected provider keeps where its key comes from, not a key", func(t *testing.T) {
		settings := config.ProviderConfig{Protocol: "openai", BaseURL: "https://api.openai.com/v1", APIKeyFrom: config.KeyFromCodex}
		findings := []detect.Finding{{Provider: "openai-codex", Settings: &settings, Usable: true}}
		draft := newProviderDraft("openai-codex", cfg, findings)
		draft.model = "gpt-5"

		layer, _ := draft.applyTo(config.Config{})

		saved := layer.Providers["openai-codex"]
		if saved.APIKeyFrom != config.KeyFromCodex || saved.APIKey != "" || saved.Model != "gpt-5" || saved.BaseURL == "" {
			t.Errorf("saved = %+v", saved)
		}
	})
}

func TestMCPForm(t *testing.T) {
	t.Run("MC-01 a started server keeps its arguments and environment line by line", func(t *testing.T) {
		draft := newMCPDraft(newServerChoice, config.MCPServer{})
		draft.name, draft.command = "files", "npx"
		draft.args = "-y\n@modelcontextprotocol/server-filesystem\n\n/tmp\n"
		draft.env = "TOKEN=${MY_TOKEN}\nMODE = fast"

		layer, err := draft.applyTo(config.Config{})

		server := layer.MCP["files"]
		if err != nil || server.Command != "npx" || len(server.Args) != 3 || server.Args[2] != "/tmp" || server.Env["TOKEN"] != "${MY_TOKEN}" || server.Env["MODE"] != "fast" {
			t.Errorf("server = %+v err = %v", server, err)
		}
	})

	t.Run("MC-02 a server reached by URL keeps its headers", func(t *testing.T) {
		draft := newMCPDraft(newServerChoice, config.MCPServer{})
		draft.name, draft.url, draft.trust = "remote", "http://localhost:3000/mcp", true
		draft.headers = "Authorization: Bearer ${TOKEN}"

		layer, err := draft.applyTo(config.Config{})

		server := layer.MCP["remote"]
		if err != nil || server.URL == "" || server.Headers["Authorization"] != "Bearer ${TOKEN}" || !server.Trust || server.Env != nil {
			t.Errorf("server = %+v err = %v", server, err)
		}
	})

	t.Run("MC-03 a server needs exactly one of command and URL", func(t *testing.T) {
		both := &mcpDraft{name: "x", command: "npx", url: "http://a"}
		neither := &mcpDraft{name: "x"}

		_, bothErr := both.applyTo(config.Config{})
		_, neitherErr := neither.applyTo(config.Config{})

		if bothErr == nil || neitherErr == nil {
			t.Errorf("both = %v neither = %v", bothErr, neitherErr)
		}
	})

	t.Run("MC-04 a line without its separator is refused with the field name", func(t *testing.T) {
		draft := &mcpDraft{name: "x", command: "npx", env: "TOKEN"}

		_, err := draft.applyTo(config.Config{})

		if err == nil || !strings.Contains(err.Error(), "environment") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("MC-05 a deleted server leaves the file", func(t *testing.T) {
		file := config.Config{MCP: map[string]config.MCPServer{"files": {Command: "npx"}, "other": {URL: "http://a"}}}
		draft := newMCPDraft("files", file.MCP["files"])
		draft.remove = true

		layer, _ := draft.applyTo(file)

		if _, left := layer.MCP["files"]; left || len(layer.MCP) != 1 || len(file.MCP) != 2 {
			t.Errorf("layer = %+v file = %+v", layer.MCP, file.MCP)
		}
	})

	t.Run("MC-06 editing shows the saved server as lines, literal values hidden and kept", func(t *testing.T) {
		saved := config.MCPServer{Command: "npx", Args: []string{"-y", "pkg"}, Env: map[string]string{"B": "${B_TOKEN}", "A": "sk-secret"}}
		draft := newMCPDraft("files", saved)
		layer, err := draft.applyTo(config.Config{})

		if draft.args != "-y\npkg" || draft.env != "A="+hiddenValue+"\nB=${B_TOKEN}" || draft.isNew || strings.Contains(draft.env, "sk-secret") {
			t.Errorf("draft = %+v", draft)
		}
		if err != nil || layer.MCP["files"].Env["A"] != "sk-secret" {
			t.Errorf("saved = %+v err = %v", layer.MCP["files"], err)
		}
	})
}

func TestSetupScreen(t *testing.T) {
	cfg := config.UserSettings(config.Config{})

	t.Run("SU-01 setup with a known provider saves the typed key and model on it", func(t *testing.T) {
		setup := &providerSetup{choice: "openai", key: "sk-new", model: "gpt-5", provider: newProviderDraft(newProviderChoice, cfg, nil)}

		layer, err := setup.applyTo(config.Config{}, nil)

		saved := layer.Providers["openai"]
		if err != nil || layer.Provider != "openai" || saved.APIKey != "sk-new" || saved.Model != "gpt-5" {
			t.Errorf("layer = %+v err = %v", layer, err)
		}
	})

	t.Run("SU-02 setup with a new provider saves it and uses it", func(t *testing.T) {
		setup := &providerSetup{choice: newProviderChoice, provider: newSetupProvider(cfg)}
		setup.provider.name, setup.provider.baseURL = "lab", "http://10.0.0.1:8081/v1"

		layer, err := setup.applyTo(config.Config{}, nil)

		if err != nil || layer.Provider != "lab" || layer.Providers["lab"].BaseURL != "http://10.0.0.1:8081/v1" {
			t.Errorf("layer = %+v err = %v", layer, err)
		}
	})

	t.Run("SU-03 the ways out are offered: go on only when the provider was built", func(t *testing.T) {
		values := func(problem ProviderProblem) []string {
			var out []string
			for _, option := range setupOptions(problem) {
				out = append(out, option.Value)
			}
			return out
		}
		built := strings.Join(values(ProviderProblem{Config: cfg, CanContinue: true}), "|")
		broken := strings.Join(values(ProviderProblem{Config: cfg}), "|")

		if !strings.Contains(built, continueChoice) || strings.Contains(broken, continueChoice) || !strings.HasSuffix(broken, quitChoice) || !strings.Contains(broken, newProviderChoice) {
			t.Errorf("built = %q broken = %q", built, broken)
		}
	})
}

func TestMoreSettings(t *testing.T) {
	t.Run("SD-19 untouched web, display, prompts and token fields stay out of the file", func(t *testing.T) {
		layer, err := defaultDraft().applyTo(config.Config{Provider: "llama-server"}, nil)

		if err != nil || layer.Web != (config.Web{}) || layer.Render != (config.Render{}) || layer.MaxTokens != 0 || layer.Context.CompactPrompt != "" {
			t.Errorf("layer = %+v err = %v", layer, err)
		}
	})

	t.Run("SD-20 changed web, display, prompts and token fields are saved", func(t *testing.T) {
		draft := defaultDraft()
		draft.search, draft.searchKey, draft.images = "brave", "bk-1", "blocks"
		draft.maxTokens, draft.compactPrompt = "4096", "Summarize briefly."

		layer, err := draft.applyTo(config.Config{Provider: "llama-server"}, nil)

		if err != nil || layer.Web.Search != "brave" || layer.Web.SearchKey != "bk-1" || layer.Render.Images != "blocks" || layer.MaxTokens != 4096 || layer.Context.CompactPrompt != "Summarize briefly." {
			t.Errorf("layer = %+v err = %v", layer, err)
		}
		if agent.DefaultCompactPrompt == "" {
			t.Error("no default summary prompt")
		}
	})

	t.Run("SD-21 descriptions of added tools are edited; saved ones of absent tools stay", func(t *testing.T) {
		file := config.Config{Provider: "llama-server", Tools: config.Tools{Descriptions: map[string]string{"mcp__gone__x": "kept"}}}
		draft := newSettingsDraft(file)
		draft.describeTools(map[string]string{"read": "Read a file.", "mcp__demo__greet": "Greet someone."})
		*draft.descriptions["mcp__demo__greet"] = "Say hi."

		layer, _ := draft.applyTo(file, nil)

		saved := layer.Tools.Descriptions
		if len(saved) != 2 || saved["mcp__demo__greet"] != "Say hi." || saved["mcp__gone__x"] != "kept" {
			t.Errorf("descriptions = %v", saved)
		}
	})
}

func TestReviewFixes(t *testing.T) {
	t.Run("SD-22 a saved search key can be forgotten; an empty field keeps it", func(t *testing.T) {
		file := config.Config{Provider: "llama-server", Web: config.Web{Search: "brave", SearchKey: "bk-1"}}
		kept, _ := newSettingsDraft(file).applyTo(file, nil)
		forget := newSettingsDraft(file)
		forget.forgetKey = true
		forgotten, _ := forget.applyTo(file, nil)

		if !forget.savedKey || kept.Web.SearchKey != "bk-1" || forgotten.Web.SearchKey != "" {
			t.Errorf("kept = %+v forgotten = %+v", kept.Web, forgotten.Web)
		}
	})

	t.Run("PV-07 keys are offered only from API logins, never subscription logins", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
		write := func(path, content string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(home, "codex", "auth.json"), `{"OPENAI_API_KEY":"sk-x","tokens":{"access_token":"a"},"auth_mode":"chatgpt"}`)
		write(filepath.Join(home, "data", "opencode", "auth.json"), `{"anthropic":{"type":"oauth","access":"a"},"groq":{"type":"api","key":"gk"}}`)

		var values []string
		for _, option := range keySources("") {
			values = append(values, option.Value)
		}

		if strings.Join(values, "|") != "|"+config.OpencodeKeyFrom("groq") {
			t.Errorf("sources = %q", values)
		}
	})

	t.Run("PV-08 the provider in use takes the top-level model as its own", func(t *testing.T) {
		file := config.Config{Provider: "llama-server", Model: "qwen3"}
		draft := newProviderDraft("llama-server", config.UserSettings(file), nil)

		layer, _ := draft.applyTo(file)

		if draft.model != "qwen3" || layer.Model != "" || layer.Providers["llama-server"].Model != "qwen3" {
			t.Errorf("draft model = %q layer = %+v", draft.model, layer)
		}
	})

	t.Run("SU-04 setup asks the key of a provider that needs one, also when one is set", func(t *testing.T) {
		cfg := config.UserSettings(config.Config{Providers: map[string]config.ProviderConfig{"openai": {APIKey: "sk-wrong"}}})

		keyed := (&providerSetup{choice: "openai"}).needsKey(cfg)
		local := (&providerSetup{choice: "llama-server"}).needsKey(cfg)

		if !keyed || local {
			t.Errorf("openai = %v llama-server = %v", keyed, local)
		}
	})

	t.Run("SU-05 setup leaves a top-level model behind when it switches provider", func(t *testing.T) {
		file := config.Config{Provider: "llama-server", Model: "qwen3"}
		switched, _ := (&providerSetup{choice: "ollama"}).applyTo(file, nil)
		same, _ := (&providerSetup{choice: "llama-server"}).applyTo(file, nil)

		if switched.Provider != "ollama" || switched.Model != "" || same.Model != "qwen3" {
			t.Errorf("switched = %+v same = %+v", switched, same)
		}
	})
}
