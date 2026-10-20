package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/plugins"
)

const (
	windowProbeTimeout     = 3 * time.Second
	defaultLocalWindow     = 32768
	defaultAnthropicWindow = 200000
	defaultResponsesWindow = 272000
)

type chosenProvider struct {
	name     string
	model    string
	window   int
	provider llm.Provider
	// windowFrom says where the window came from: the settings, the server,
	// or mo's default.
	windowFrom string
}

// chooseProvider picks the provider from the flag or the config. With
// neither, it uses the first local server that answers.
func chooseProvider(ctx context.Context, cfg config.Config, name, model string, loaded *plugins.Set) (chosenProvider, error) {
	if name == "" {
		name = cfg.Provider
	}
	if name == "" {
		found, err := findLocalServer(ctx, cfg)
		if err != nil {
			return chosenProvider{}, err
		}
		name = found
	}
	settings, ok := cfg.Providers[name]
	if !ok {
		settings, ok = detectedProvider(ctx, cfg, name)
	} else if settings.OAuth == nil && settings.TokenFrom == "" && settings.NeedsKey() && settings.Key() == "" {
		// A sign-in needs no key; looking for one would probe every local
		// server for a second.
		if detected, found := detectedProvider(ctx, cfg, name); found && detected.Key() != "" {
			settings = detected
		} else if detected, found := detectedProvider(ctx, cfg, name+"-opencode"); found && detected.Key() != "" {
			settings = detected
		}
	}
	if !ok {
		known := slices.Sorted(maps.Keys(cfg.Providers))
		return chosenProvider{}, fmt.Errorf("unknown provider %q (known: %s; see mo -detect)", name, strings.Join(known, ", "))
	}
	token := auth.TokenFor(name, settings)
	client := loaded.ProviderClient(name, settings)
	if token == nil && settings.NeedsKey() && settings.Key() == "" {
		return chosenProvider{}, fmt.Errorf("%s needs an API key and none was found (%s)", name, keyOrigin(settings))
	}

	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		model = settings.Model
	}

	switch settings.Protocol {
	case "openai":
		provider := &llm.OpenAI{BaseURL: settings.BaseURL, APIKey: settings.Key(), Token: token, Headers: settings.Headers, Client: client}
		if model == "" {
			first, err := firstModel(ctx, provider)
			if err != nil {
				return chosenProvider{}, fmt.Errorf("%s: no model configured and could not list models: %w", name, err)
			}
			model = first
		}
		window, from, llama := openAIWindow(ctx, provider, model, cfg.Context.Window)
		provider.PromptProgress = llama
		return chosenProvider{name: name, model: model, window: window, provider: provider, windowFrom: from}, nil
	case "anthropic", "claude-code":
		key := settings.Key()
		if key == "" && token == nil && client == nil {
			return chosenProvider{}, fmt.Errorf("%s needs an API key: set providers.%s.api_key, api_key_env or api_key_from", name, name)
		}
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		provider := &llm.Anthropic{BaseURL: settings.BaseURL, APIKey: key, Token: token, Client: client, Subscription: settings.Protocol == "claude-code",
			NoPreamble: settings.Preamble != nil && !*settings.Preamble}
		window, from := cfg.Context.Window, windowFromSettings
		if window == 0 {
			window, from = anthropicWindow(ctx, provider, model)
		}
		return chosenProvider{name: name, model: model, window: window, provider: provider, windowFrom: from}, nil
	case "responses":
		if token == nil && client == nil {
			return chosenProvider{}, fmt.Errorf("%s needs a sign-in: set providers.%s.oauth", name, name)
		}
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		window, from := cfg.Context.Window, windowFromSettings
		if window == 0 {
			window, from = defaultResponsesWindow, windowFromDefault
		}
		return chosenProvider{name: name, model: model, window: window, provider: &llm.Responses{BaseURL: settings.BaseURL, Token: token, Client: client}, windowFrom: from}, nil
	case "cloudcode":
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		window, from := cfg.Context.Window, windowFromSettings
		if window == 0 {
			window, from = defaultCloudCodeWindow(model), windowFromDefault
		}
		return chosenProvider{name: name, model: model, window: window, provider: &llm.CloudCode{BaseURL: settings.BaseURL, Token: token, Client: client, NoPreamble: settings.Preamble != nil && !*settings.Preamble}, windowFrom: from}, nil
	}
	return chosenProvider{}, fmt.Errorf("provider %q has unknown type %q (use openai, anthropic, claude-code, responses or cloudcode)", name, settings.Protocol)
}

// defaultCloudCodeWindow is the context of the models Cloud Code serves,
// which it does not say: a million tokens for Gemini, 200000 for the rest.
func defaultCloudCodeWindow(model string) int {
	if strings.HasPrefix(model, "gemini") {
		return 1000000
	}
	return defaultAnthropicWindow
}

// detectedProvider finds a usable provider by the name mo -detect gives it.
func detectedProvider(ctx context.Context, cfg config.Config, name string) (config.ProviderConfig, bool) {
	for _, found := range detect.Run(ctx, cfg) {
		if found.Provider == name && found.Usable && found.Settings != nil {
			return *found.Settings, true
		}
	}
	return config.ProviderConfig{}, false
}

func keyOrigin(settings config.ProviderConfig) string {
	switch {
	case settings.APIKeyFrom != "":
		return "api_key_from " + settings.APIKeyFrom
	case settings.APIKeyEnv != "":
		return "env " + settings.APIKeyEnv
	}
	return "api_key"
}

const (
	windowFromSettings = "your settings"
	windowFromDefault  = "mo's default: the server does not say"
)

// openAIWindow is the context of the model: the one set, else the one the
// server says (llama-server, the model list, Ollama, LM Studio, Gemini),
// else the default; and whether the server is llama-server, which streams
// how far it is in reading the prompt.
func openAIWindow(ctx context.Context, provider *llm.OpenAI, model string, set int) (int, string, bool) {
	ctx, cancel := context.WithTimeout(ctx, windowProbeTimeout)
	defer cancel()
	size, llama := provider.Props(ctx)
	switch {
	case set > 0:
		return set, windowFromSettings, llama
	case size > 0:
		return size, "llama-server", llama
	}
	if size, from := provider.ModelWindow(ctx, model); size > 0 {
		return size, from, llama
	}
	return defaultLocalWindow, windowFromDefault, llama
}

// anthropicWindow is the model's context from the Models API, else the
// default.
func anthropicWindow(ctx context.Context, provider *llm.Anthropic, model string) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, windowProbeTimeout)
	defer cancel()
	if size := provider.ModelWindow(ctx, model); size > 0 {
		return size, "the Models API"
	}
	return defaultAnthropicWindow, windowFromDefault
}

func findLocalServer(ctx context.Context, cfg config.Config) (string, error) {
	for _, name := range config.LocalPresets {
		if _, err := detect.LocalModels(ctx, cfg.Providers[name]); err == nil {
			return name, nil
		}
	}
	return "", errors.New("no provider configured and no local server found (llama-server :8080, ollama :11434, lmstudio :1234); use -p or set provider in ~/.config/mo/config.toml")
}

func firstModel(ctx context.Context, provider *llm.OpenAI) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	models, err := provider.ListModels(ctx)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", errors.New("the server lists no models")
	}
	return models[0], nil
}

// modelRefresher checks the model and its context, so the agent does not go
// on with what the server no longer has.
//
// llama-server may be restarted with another model or another context, so it
// is asked at every turn, before a compaction and after a failure: when the
// one model it lists is not the one it listed before, the agent follows it (a
// name the user chose is kept while the server stays), and the window is the
// server's, never more than a size set in the settings. Any other provider
// is stable, and while the model writes it is there: it is asked for its
// models when a conversation starts, when the list of models is opened and
// after a request failed, a model it no
// longer offers is told to the user once, and the window is read again where
// the server says it.
func modelRefresher(chosen chosenProvider, set int) func(ctx context.Context, reason agent.CheckReason, model string, window int) agent.ModelCheck {
	var list func(context.Context) ([]string, error)
	var windowOf func(context.Context, string) int
	switch provider := chosen.provider.(type) {
	case *llm.OpenAI:
		if provider.PromptProgress {
			return llamaRefresher(provider, set)
		}
		list = provider.ListModels
		windowOf = func(ctx context.Context, model string) int {
			size, _ := provider.ModelWindow(ctx, model)
			return size
		}
	case *llm.Anthropic:
		list, windowOf = provider.ListModels, provider.ModelWindow
	case *llm.Responses:
		list = provider.ListModels
	case *llm.CloudCode:
		list = provider.ListModels
	default:
		return nil
	}
	warned := ""
	return func(ctx context.Context, reason agent.CheckReason, model string, window int) agent.ModelCheck {
		check := agent.ModelCheck{Model: model, Window: window}
		if reason != agent.CheckNew && reason != agent.CheckFailure && reason != agent.CheckModels {
			return check
		}
		ctx, cancel := context.WithTimeout(ctx, windowProbeTimeout)
		defer cancel()
		if models, err := list(ctx); err == nil && len(models) > 0 {
			switch {
			case slices.ContainsFunc(models, func(id string) bool { return sameModel(id, model) }):
				warned = ""
			case warned != model:
				warned = model
				check.Notice = fmt.Sprintf("%s no longer appears in the models %s offers; pick another with /model", model, chosen.name)
			}
		}
		if set == 0 && windowOf != nil {
			if size := windowOf(ctx, model); size > 0 {
				check.Window = size
			}
		}
		return check
	}
}

// sameModel tells a listed id from the name in use: Gemini lists models/ before
// the name and Ollama :latest after it.
func sameModel(id, model string) bool {
	return id == model || strings.TrimPrefix(id, "models/") == model || id == model+":latest"
}

// llamaRefresher follows what llama-server serves between messages.
func llamaRefresher(provider *llm.OpenAI, set int) func(ctx context.Context, reason agent.CheckReason, model string, window int) agent.ModelCheck {
	listed := func(ctx context.Context) string {
		if models, err := provider.ListModels(ctx); err == nil && len(models) == 1 {
			return models[0]
		}
		return ""
	}
	seeded, cancel := context.WithTimeout(context.Background(), windowProbeTimeout)
	seen := listed(seeded)
	cancel()
	return func(ctx context.Context, _ agent.CheckReason, model string, window int) agent.ModelCheck {
		ctx, cancel := context.WithTimeout(ctx, windowProbeTimeout)
		defer cancel()
		if now := listed(ctx); now != "" {
			if seen != "" && now != seen {
				model = now
			}
			seen = now
		}
		if size, ok := provider.Props(ctx); ok {
			window = size
			if set > 0 {
				window = min(set, size)
			}
		}
		return agent.ModelCheck{Model: model, Window: window}
	}
}
