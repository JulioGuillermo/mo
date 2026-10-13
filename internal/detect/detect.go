// Package detect looks for providers mo could use without more setup: API
// keys in the environment, local model servers, and the keys and providers
// that codex and opencode already have configured.
package detect

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

const localProbeTimeout = 500 * time.Millisecond

// Finding is one provider found. Settings is set when the provider is not in
// the config yet and has to be added under that name to be used.
type Finding struct {
	Provider string
	Settings *config.ProviderConfig
	Origin   string
	Usable   bool
	Note     string
	Models   []string
}

func Run(ctx context.Context, cfg config.Config) []Finding {
	var findings []Finding
	findings = append(findings, fromEnvironment(cfg)...)
	findings = append(findings, localServers(ctx, cfg)...)
	findings = append(findings, fromCodex(cfg)...)
	findings = append(findings, fromOpencode(cfg)...)
	return findings
}

func fromEnvironment(cfg config.Config) []Finding {
	var findings []Finding
	for _, name := range slices.Sorted(maps.Keys(cfg.Providers)) {
		settings := cfg.Providers[name]
		if settings.APIKeyEnv != "" && os.Getenv(settings.APIKeyEnv) != "" {
			findings = append(findings, Finding{Provider: name, Origin: "env " + settings.APIKeyEnv, Usable: true})
		}
	}
	return findings
}

// localServers asks every keyless openai provider for its models at once.
func localServers(ctx context.Context, cfg config.Config) []Finding {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(cfg.Providers)) {
		settings := cfg.Providers[name]
		if settings.Protocol == "openai" && !settings.NeedsKey() {
			names = append(names, name)
		}
	}
	results := make([]*Finding, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			results[i] = probeLocal(ctx, name, cfg.Providers[name])
		})
	}
	wg.Wait()
	var findings []Finding
	for _, found := range results {
		if found != nil {
			findings = append(findings, *found)
		}
	}
	return findings
}

func probeLocal(ctx context.Context, name string, settings config.ProviderConfig) *Finding {
	models, err := LocalModels(ctx, settings)
	if err != nil {
		return nil
	}
	return &Finding{Provider: name, Origin: settings.BaseURL, Usable: true, Note: fmt.Sprintf("%d models", len(models)), Models: models}
}

// LocalModels asks a keyless server for its models, giving up quickly.
func LocalModels(ctx context.Context, settings config.ProviderConfig) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, localProbeTimeout)
	defer cancel()
	return (&llm.OpenAI{BaseURL: settings.BaseURL}).ListModels(ctx)
}
