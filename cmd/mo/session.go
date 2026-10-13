package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/tui"
	"github.com/jg/mo/internal/web"
)

type sessionChoice struct {
	provider string
	model    string
	yes      bool
}

// buildSession puts together the agent the config describes. Its Reload
// builds a session from a new user layer, as the settings screen edits it,
// after checking that the layer works on its own, outside any project; the
// provider and model given on the command line no longer apply then.
func buildSession(ctx context.Context, workDir string, cfg config.Config, choice sessionChoice, hub *mcphub.Hub) (tui.Session, error) {
	chosen, err := chooseProvider(ctx, cfg, choice.provider, choice.model)
	if err != nil {
		return tui.Session{}, err
	}
	confirm := append(slices.Clone(cfg.Tools.Confirm), hub.NeedConfirmation()...)
	if choice.yes {
		confirm = nil
	}
	searcher, err := web.NewSearcher(web.SearchSettings{Backend: cfg.Web.Search, URL: cfg.Web.SearchURL, Key: cfg.Web.Key()}, web.SearchClient)
	if err != nil {
		return tui.Session{}, err
	}
	env := toolEnv(workDir, cfg.Memory.Dir)
	registry := tools.NewRegistry(env, tools.Options{
		Searcher:     searcher,
		Extra:        hub.Tools(),
		Mode:         cfg.Tools.Mode,
		AlwaysLoaded: cfg.Tools.AlwaysLoaded,
		Disabled:     cfg.Tools.Disabled,
		MaxOutput:    cfg.Tools.MaxOutput,
		Descriptions: cfg.Tools.Descriptions,
	})
	memoryIndex := ""
	if !slices.Contains(cfg.Tools.Disabled, "memory") {
		memoryIndex = tools.MemoryIndex(env)
	}
	assistant := &agent.Agent{
		Provider:      chosen.provider,
		Model:         chosen.model,
		System:        agent.SystemPrompt(cfg.SystemPrompt, workDir, memoryIndex),
		MaxTokens:     cfg.MaxTokens,
		Tools:         registry,
		ConfirmTools:  confirm,
		Window:        chosen.window,
		CompactAt:     *cfg.Context.CompactAt,
		CompactPrompt: cfg.Context.CompactPrompt,
	}
	reload := func(userLayer config.Config) (tui.Session, error) {
		if _, err := chooseProvider(context.Background(), config.UserSettings(userLayer), "", ""); err != nil {
			return tui.Session{}, fmt.Errorf("outside this project: %w", err)
		}
		fresh, err := config.LoadWithUserLayer(workDir, userLayer)
		if err != nil {
			return tui.Session{}, err
		}
		return buildSession(context.Background(), workDir, fresh, sessionChoice{yes: choice.yes}, hub)
	}
	return tui.Session{
		Agent:    assistant,
		Provider: chosen.name,
		Model:    chosen.model,
		Config:   cfg,
		Reload:   reload,
		MCP:      hub.Status(),
	}, nil
}

func toolEnv(workDir, memoryDir string) tools.Env {
	return tools.Env{
		WorkDir:        workDir,
		ProtectedPaths: []string{filepath.Dir(config.UserFile()), config.ProjectFile(workDir), memoryDir},
		MemoryDir:      memoryDir,
	}
}
