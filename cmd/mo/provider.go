package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/llm"
)

const (
	windowProbeTimeout     = 2 * time.Second
	defaultLocalWindow     = 32768
	defaultAnthropicWindow = 200000
)

type chosenProvider struct {
	name     string
	model    string
	window   int
	provider llm.Provider
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
	}
	if !ok {
		known := slices.Sorted(maps.Keys(cfg.Providers))
		return chosenProvider{}, fmt.Errorf("unknown provider %q (known: %s; see mo -detect)", name, strings.Join(known, ", "))
	}
	if settings.NeedsKey() && settings.Key() == "" {
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
		provider := &llm.OpenAI{BaseURL: settings.BaseURL, APIKey: settings.Key()}
		if model == "" {
			first, err := firstModel(ctx, provider)
			if err != nil {
				return chosenProvider{}, fmt.Errorf("%s: no model configured and could not list models: %w", name, err)
			}
			model = first
		}
		window := cfg.Context.Window
		if window == 0 {
			window = probeWindow(ctx, provider)
		}
		return chosenProvider{name: name, model: model, window: window, provider: provider}, nil
	case "anthropic":
		key := settings.Key()
		if key == "" {
			return chosenProvider{}, fmt.Errorf("%s needs an API key: set providers.%s.api_key, api_key_env or api_key_from", name, name)
		}
		if model == "" {
			return chosenProvider{}, fmt.Errorf("%s needs a model: use -m or set model in the config", name)
		}
		window := cfg.Context.Window
		if window == 0 {
			window = defaultAnthropicWindow
		}
		provider := &llm.Anthropic{BaseURL: settings.BaseURL, APIKey: key}
		return chosenProvider{name: name, model: model, window: window, provider: provider}, nil
	}
	return chosenProvider{}, fmt.Errorf("provider %q has unknown type %q (use openai or anthropic)", name, settings.Protocol)
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
	case settings.APIKeyEnv != "":
		return "env " + settings.APIKeyEnv
	case settings.APIKeyFrom != "":
		return "api_key_from " + settings.APIKeyFrom
	}
	return "api_key"
}

func probeWindow(ctx context.Context, provider *llm.OpenAI) int {
	ctx, cancel := context.WithTimeout(ctx, windowProbeTimeout)
	defer cancel()
	if window := provider.ContextWindow(ctx); window > 0 {
		return window
	}
	return defaultLocalWindow
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
