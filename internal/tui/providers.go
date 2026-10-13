package tui

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
)

// providerDraft holds what the provider form edits for one provider.
type providerDraft struct {
	name      string
	isNew     bool
	hasPreset bool
	savedKey  bool
	protocol  string
	baseURL   string
	model     string
	keyEnv    string
	key       string
	keyFrom   string
	forgetKey bool
	remove    bool
	inUse     bool
	use       bool
}

func newProviderDraft(name string, cfg config.Config, findings []detect.Finding) *providerDraft {
	if name == newProviderChoice {
		return &providerDraft{isNew: true, protocol: "openai", baseURL: defaultNewURL, use: true}
	}
	settings, known := cfg.Providers[name]
	if found := findingFor(findings, name); !known && found != nil && found.Settings != nil {
		settings = *found.Settings
	}
	_, hasPreset := config.Defaults().Providers[name]
	inUse := cfg.Provider == name
	if inUse && cfg.Model != "" {
		settings.Model = cfg.Model
	}
	return &providerDraft{
		name:      name,
		hasPreset: hasPreset,
		savedKey:  settings.APIKey != "",
		protocol:  settings.Protocol,
		baseURL:   settings.BaseURL,
		model:     settings.Model,
		keyEnv:    settings.APIKeyEnv,
		keyFrom:   settings.APIKeyFrom,
		inUse:     inUse,
	}
}

// fields are the inputs for the draft; a new provider also gets a name.
func (d *providerDraft) fields(cfg config.Config) []huh.Field {
	var fields []huh.Field
	if d.isNew {
		fields = append(fields, huh.NewInput().Title("Name").Description("how mo calls it, e.g. my-server").
			Validate(validNewName(cfg.Providers)).Value(&d.name))
	}
	fields = append(fields,
		huh.NewSelect[string]().Title("Type").Options(
			huh.NewOption("OpenAI-compatible: llama-server, ollama, vLLM, OpenAI, OpenRouter…", "openai"),
			huh.NewOption("Anthropic", "anthropic"),
		).Value(&d.protocol),
		huh.NewInput().Title("Base URL").Description("e.g. http://localhost:8080/v1").Validate(required).Value(&d.baseURL),
		huh.NewInput().Title("Model").Description("empty: the first the server lists (Anthropic needs one)").Value(&d.model),
		huh.NewInput().Title("API key variable").Description("environment variable holding the key; empty if it needs none").Value(&d.keyEnv),
		huh.NewInput().Title("API key").Description("saved in "+config.UserFile()+"; empty keeps the saved one").
			EchoMode(huh.EchoModePassword).Value(&d.key),
	)
	if d.savedKey {
		fields = append(fields, huh.NewConfirm().Title("Forget the saved API key").Value(&d.forgetKey))
	}
	if sources := keySources(d.keyFrom); len(sources) > 1 {
		fields = append(fields, huh.NewSelect[string]().Title("Or take the key from").Options(sources...).Value(&d.keyFrom))
	}
	if !d.inUse {
		fields = append(fields, huh.NewConfirm().Title("Use it from now on").Value(&d.use))
	}
	if !d.isNew {
		title := "Delete this provider"
		if d.hasPreset {
			title = "Reset it to the built-in settings"
		}
		fields = append(fields, huh.NewConfirm().Title(title).Value(&d.remove))
	}
	return fields
}

// keySources lists the logins of other tools mo can take a key from, and
// the current source even when it is no longer found.
func keySources(current string) []huh.Option[string] {
	options := []huh.Option[string]{huh.NewOption("nowhere else", "")}
	sources := map[string]bool{}
	if config.ReadCodexAuth().UsableKey() != "" {
		sources[config.KeyFromCodex] = true
	}
	for name, entry := range config.OpencodeAuth() {
		if entry.Key != "" {
			sources[config.OpencodeKeyFrom(name)] = true
		}
	}
	if current != "" {
		sources[current] = true
	}
	for _, source := range slices.Sorted(maps.Keys(sources)) {
		options = append(options, huh.NewOption(source+" login", source))
	}
	return options
}

// applyTo writes the provider into the user layer, keeping only what differs
// from the built-in preset. A removed provider that was in use leaves the
// choice to auto. The provider in use takes the top-level model as its own.
func (d *providerDraft) applyTo(layer config.Config) (config.Config, error) {
	name := strings.TrimSpace(d.name)
	if name == "" {
		return layer, errors.New("the provider needs a name")
	}
	if d.remove {
		layer.Providers = withoutEntry(layer.Providers, name)
		if layer.Provider == name && !d.hasPreset {
			layer.Provider = autoProvider
		}
		return layer, nil
	}
	if d.protocol == "anthropic" && strings.TrimSpace(d.model) == "" {
		return layer, fmt.Errorf("%s: an Anthropic provider needs a model", name)
	}
	preset := config.Defaults().Providers[name]
	entry := config.ProviderConfig{
		Protocol:   unlessDefault(d.protocol, preset.Protocol),
		BaseURL:    unlessDefault(strings.TrimSpace(d.baseURL), preset.BaseURL),
		Model:      unlessDefault(strings.TrimSpace(d.model), preset.Model),
		APIKeyEnv:  unlessDefault(strings.TrimSpace(d.keyEnv), preset.APIKeyEnv),
		APIKeyFrom: unlessDefault(d.keyFrom, preset.APIKeyFrom),
		APIKey:     layer.Providers[name].APIKey,
	}
	if d.forgetKey {
		entry.APIKey = ""
	}
	if key := strings.TrimSpace(d.key); key != "" {
		entry.APIKey = key
	}
	if d.use || d.inUse {
		layer.Provider = name
		layer.Model = ""
	}
	if entry == (config.ProviderConfig{}) {
		layer.Providers = withoutEntry(layer.Providers, name)
		return layer, nil
	}
	layer.Providers = withEntry(layer.Providers, name, entry)
	return layer, nil
}

// pickProvider lists the providers to edit, then opens the chosen one.
func (m model) pickProvider(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	cfg := config.UserSettings(msg.layer)
	picked := m.session.Provider
	options := providerOptions(cfg, msg.findings, msg.layer.Provider)[1:]
	options = append(options, huh.NewOption("+ set up a new provider", newProviderChoice))
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Provider to edit").Options(options...).Value(&picked),
	)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		return m.editProvider(msg, cfg, picked)
	}
	return m.showSettings(&settingsScreen{form: form, layer: msg.layer, next: next})
}

func (m model) editProvider(msg settingsLoadedMsg, cfg config.Config, name string) (tea.Model, tea.Cmd) {
	draft := newProviderDraft(name, cfg, msg.findings)
	title := name
	if draft.isNew {
		title = "New provider"
	}
	form := huh.NewForm(huh.NewGroup(draft.fields(cfg)...).Title(title)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: msg.layer, apply: draft.applyTo})
}
