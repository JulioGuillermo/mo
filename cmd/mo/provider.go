package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/llm"
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
func chooseProvider(ctx context.Context, cfg config.Config, name, model string) (chosenProvider, error) {
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
	} else if settings.NeedsKey() && settings.Key() == "" {
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
	var token func(context.Context) (string, error)
	if settings.OAuth != nil {
		token = auth.NewSource(name, *settings.OAuth).WithHeaders(settings.Headers).Token
	}
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
		provider := &llm.OpenAI{BaseURL: settings.BaseURL, APIKey: settings.Key(), Token: token, Headers: settings.Headers}
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
		if key == "" && token == nil {
			return chosenProvider{}, fmt.Errorf("%s needs an API key: set providers.%s.api_key, api_key_env or api_key_from", name, name)
		}
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		provider := &llm.Anthropic{BaseURL: settings.BaseURL, APIKey: key, Token: token, Subscription: settings.Protocol == "claude-code"}
		window, from := cfg.Context.Window, windowFromSettings
		if window == 0 {
			window, from = anthropicWindow(ctx, provider, model)
		}
		return chosenProvider{name: name, model: model, window: window, provider: provider, windowFrom: from}, nil
	case "responses":
		if token == nil {
			return chosenProvider{}, fmt.Errorf("%s needs a sign-in: set providers.%s.oauth", name, name)
		}
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		window, from := cfg.Context.Window, windowFromSettings
		if window == 0 {
			window, from = defaultResponsesWindow, windowFromDefault
		}
		return chosenProvider{name: name, model: model, window: window, provider: &llm.Responses{BaseURL: settings.BaseURL, Token: token}, windowFrom: from}, nil
	case "cloudcode":
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		window, from := cfg.Context.Window, windowFromSettings
		if window == 0 {
			window, from = defaultCloudCodeWindow(model), windowFromDefault
		}
		return chosenProvider{name: name, model: model, window: window, provider: &llm.CloudCode{BaseURL: settings.BaseURL, Token: token}, windowFrom: from}, nil
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
