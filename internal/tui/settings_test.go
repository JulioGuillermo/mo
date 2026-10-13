package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SD-01 | Happy    | An untouched draft leaves the user file as it was                    | P0       |
// | SD-02 | Edge     | Unticking every confirmation saves confirm = [], not the default     | P0       |
// | SD-03 | Happy    | Only edited tool descriptions are saved, an emptied one as ""        | P1       |
// | SD-04 | Happy    | Picking a detected provider saves its settings, never its key        | P0       |
// | SD-05 | Error    | Bad numbers are refused with the field name                          | P1       |
// | SD-06 | Happy    | An empty model keeps following the server                            | P1       |
// | SD-07 | Happy    | Esc closes the settings without saving                               | P1       |
// | SD-08 | Happy    | The budget line adds up every item                                   | P2       |
// | SD-09 | Happy    | A model typed for a provider goes to that provider, or to the top-level model if the file has one | P1 |
// | SD-10 | Happy    | Changing only the provider keeps the models already saved            | P1       |
// | SD-11 | Error    | A run cannot start while settings load or save                       | P0       |
// | SD-12 | Happy    | Agent messages still reach the UI while the form is open             | P1       |
// | SD-13 | Edge     | A saved provider that is not found now stays selected and saved      | P0       |
// | SD-14 | Happy    | With auto, a typed model becomes the top-level model                 | P1       |
// | SD-15 | Edge     | A resize while the form is open refits it and the input              | P2       |
// | SD-16 | Happy    | Choosing to drop old messages is saved; the default is not           | P1       |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/tools"
)

func defaultDraft() *settingsDraft {
	return newSettingsDraft(config.Config{Provider: "llama-server"})
}

func TestSettingsDraft(t *testing.T) {
	t.Run("SD-01 an untouched draft leaves the user file as it was", func(t *testing.T) {
		layer, err := defaultDraft().applyTo(config.Config{Provider: "llama-server"}, nil)

		if err != nil {
			t.Fatal(err)
		}
		if layer.Provider != "llama-server" || layer.Providers != nil || layer.SystemPrompt != "" || layer.Memory.Dir != "" || layer.Context.CompactAt != nil {
			t.Errorf("layer = %+v", layer)
		}
		if layer.Tools.Mode != "" || layer.Tools.Confirm != nil || layer.Tools.AlwaysLoaded != nil || layer.Tools.MaxOutput != 0 || layer.Tools.Descriptions != nil {
			t.Errorf("tools = %+v", layer.Tools)
		}
	})

	t.Run("SD-02 unticking every confirmation saves confirm = [], not the default", func(t *testing.T) {
		draft := defaultDraft()
		draft.confirm = nil

		layer, _ := draft.applyTo(config.Config{}, nil)

		if layer.Tools.Confirm == nil || len(layer.Tools.Confirm) != 0 {
			t.Errorf("confirm = %#v", layer.Tools.Confirm)
		}
	})

	t.Run("SD-03 only edited tool descriptions are saved, an emptied one as empty", func(t *testing.T) {
		draft := defaultDraft()
		*draft.descriptions["bash"] = "Run bash."
		*draft.descriptions["ls"] = ""

		layer, _ := draft.applyTo(config.Config{}, nil)

		descriptions := layer.Tools.Descriptions
		if len(descriptions) != 2 || descriptions["bash"] != "Run bash." || descriptions["ls"] != "" {
			t.Errorf("descriptions = %#v", descriptions)
		}
	})

	t.Run("SD-04 picking a detected provider saves its settings, never its key", func(t *testing.T) {
		draft := defaultDraft()
		draft.provider = "openai-codex"
		settings := config.ProviderConfig{Protocol: "openai", BaseURL: "https://api.openai.com/v1", APIKeyFrom: config.KeyFromCodex}
		findings := []detect.Finding{{Provider: "openai-codex", Settings: &settings, Usable: true}}

		layer, _ := draft.applyTo(config.Config{Provider: "llama-server"}, findings)

		saved := layer.Providers["openai-codex"]
		if layer.Provider != "openai-codex" || saved.APIKeyFrom != config.KeyFromCodex || saved.APIKey != "" || saved.BaseURL == "" {
			t.Errorf("saved = %+v", saved)
		}
	})

	t.Run("SD-05 bad numbers are refused with the field name", func(t *testing.T) {
		for field, set := range map[string]func(*settingsDraft){
			"context window":  func(d *settingsDraft) { d.window = "big" },
			"max tool output": func(d *settingsDraft) { d.maxOutput = "-1" },
			"compact at":      func(d *settingsDraft) { d.compactAt = "1.5" },
		} {
			draft := defaultDraft()
			set(draft)
			if _, err := draft.applyTo(config.Config{}, nil); err == nil || !strings.Contains(err.Error(), field) {
				t.Errorf("%s: err = %v", field, err)
			}
		}
	})

	t.Run("SD-06 an empty model keeps following the server", func(t *testing.T) {
		layer, _ := defaultDraft().applyTo(config.Config{Provider: "llama-server"}, nil)

		if model := layer.Providers["llama-server"].Model; model != "" || layer.Model != "" {
			t.Errorf("model = %q / %q", model, layer.Model)
		}
	})

	t.Run("SD-09 a typed model goes to its provider, or to the top-level model the file already has", func(t *testing.T) {
		draft := defaultDraft()
		draft.model = "qwen3"
		perProvider, _ := draft.applyTo(config.Config{Provider: "llama-server"}, nil)

		withTopLevel := newSettingsDraft(config.Config{Provider: "llama-server", Model: "old"})
		withTopLevel.model = "new"
		topLevel, _ := withTopLevel.applyTo(config.Config{Provider: "llama-server", Model: "old"}, nil)

		if perProvider.Providers["llama-server"].Model != "qwen3" || perProvider.Model != "" {
			t.Errorf("per provider = %+v", perProvider)
		}
		if topLevel.Model != "new" || topLevel.Providers["llama-server"].Model != "" {
			t.Errorf("top level = %+v", topLevel)
		}
	})

	t.Run("SD-10 changing only the provider keeps the models already saved", func(t *testing.T) {
		file := config.Config{Provider: "ollama", Providers: map[string]config.ProviderConfig{"ollama": {Model: "qwen3"}}}
		draft := newSettingsDraft(file)
		draft.provider = "lmstudio"

		layer, _ := draft.applyTo(file, nil)

		if layer.Provider != "lmstudio" || layer.Providers["ollama"].Model != "qwen3" || layer.Providers["lmstudio"].Model != "" {
			t.Errorf("layer = %+v", layer)
		}
		if file.Providers["ollama"].Model != "qwen3" || len(file.Providers) != 1 {
			t.Errorf("the original layer was modified: %+v", file)
		}
	})
}

func TestSettingsScreen(t *testing.T) {
	t.Run("SD-07 esc closes the settings without saving", func(t *testing.T) {
		session := Session{Agent: &agent.Agent{}, Provider: "llama-server", Config: config.Defaults()}
		m := model{session: session, printer: newPrinter(), input: newInput()}
		m.settings = &settingsScreen{draft: defaultDraft()}

		updated, cmd := m.Update(keyEsc)

		if updated.(model).settings != nil || cmd == nil {
			t.Error("settings still open")
		}
	})

	t.Run("SD-08 the budget line adds up every item", func(t *testing.T) {
		text := budgetText([]agent.BudgetItem{{Name: "system prompt", Tokens: 90}, {Name: "tool bash", Tokens: 40}})

		if text != "≈ 130 tokens: system prompt 90 · bash 40" {
			t.Errorf("text = %q", text)
		}
	})
}

func TestSettingsGuards(t *testing.T) {
	t.Run("SD-11 a run cannot start while settings load or save", func(t *testing.T) {
		m := model{session: Session{Agent: &agent.Agent{}}, printer: newPrinter(), input: newInput(), settingsInFlight: true}
		m.input.SetValue("hello")

		updated, cmd := m.Update(keyEnter)

		if updated.(model).running || cmd != nil {
			t.Error("a run started while the settings were in flight")
		}
	})

	t.Run("SD-12 agent messages still reach the UI while the form is open", func(t *testing.T) {
		m := model{session: Session{Agent: &agent.Agent{}}, printer: newPrinter(), input: newInput(), running: true, cancel: func() {}}
		m.settings = &settingsScreen{draft: defaultDraft()}

		updated, _ := m.Update(runDoneMsg{})

		if updated.(model).running {
			t.Error("runDoneMsg went to the form")
		}
	})
}

func TestSettingsEdges(t *testing.T) {
	t.Run("SD-13 a saved provider that is not found now stays selected and saved", func(t *testing.T) {
		options := providerOptions(config.Defaults(), nil, "ghost")
		found := false
		for _, option := range options {
			found = found || option.Value == "ghost"
		}
		file := config.Config{Provider: "ghost"}

		layer, _ := newSettingsDraft(file).applyTo(file, nil)

		if !found || layer.Provider != "ghost" {
			t.Errorf("listed = %v provider = %q", found, layer.Provider)
		}
	})

	t.Run("SD-14 with auto, a typed model becomes the top-level model", func(t *testing.T) {
		draft := newSettingsDraft(config.Config{})
		draft.model = "qwen3"

		layer, _ := draft.applyTo(config.Config{}, nil)

		if layer.Model != "qwen3" || layer.Provider != "" {
			t.Errorf("layer = %+v", layer)
		}
	})

	t.Run("SD-15 a resize while the form is open refits it and the input", func(t *testing.T) {
		withTools := &agent.Agent{Tools: tools.NewRegistry(tools.Env{}, tools.Options{Mode: tools.ModeFull})}
		session := Session{Agent: withTools, Config: config.Defaults()}
		m := model{session: session, printer: newPrinter(), input: newInput(), width: 120, height: 50}
		m.settings = &settingsScreen{draft: defaultDraft(), form: buildSettingsForm(session, defaultDraft(), nil, nil)}

		updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})

		after := updated.(model)
		if after.width != 60 || after.input.Width() > 60 || lipgloss.Height(after.settings.form.View()) > 18 {
			t.Errorf("width = %d input = %d form height = %d", after.width, after.input.Width(), lipgloss.Height(after.settings.form.View()))
		}
	})
}

func TestStrategySetting(t *testing.T) {
	t.Run("SD-16 choosing to drop old messages is saved; the default is not", func(t *testing.T) {
		untouched, _ := defaultDraft().applyTo(config.Config{Provider: "llama-server"}, nil)
		draft := defaultDraft()
		draft.strategy = "drop"
		dropping, _ := draft.applyTo(config.Config{Provider: "llama-server"}, nil)

		if untouched.Context.Strategy != "" || dropping.Context.Strategy != "drop" {
			t.Errorf("untouched = %q dropping = %q", untouched.Context.Strategy, dropping.Context.Strategy)
		}
	})
}
