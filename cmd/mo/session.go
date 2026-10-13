package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/tui"
	"github.com/jg/mo/internal/web"
)

type sessionChoice struct {
	provider string
	model    string
	yes      bool
}

// extensions are what a run loads once and keeps across settings changes.
type extensions struct {
	mcp     *mcphub.Hub
	plugins *plugins.Set
}

// buildSession puts together the agent the config describes. Its Reload
// builds a session from a new user layer, as the settings screen edits it,
// after checking that the layer works on its own, outside any project; the
// provider and model given on the command line no longer apply then.
func buildSession(ctx context.Context, workDir string, cfg config.Config, choice sessionChoice, loaded extensions) (tui.Session, error) {
	hub := loaded.mcp
	chosen, err := chooseProvider(ctx, cfg, choice.provider, choice.model)
	if err != nil {
		return tui.Session{}, err
	}
	confirm := append(slices.Clone(cfg.Tools.Confirm), hub.NeedConfirmation()...)
	confirm = append(confirm, loaded.plugins.NeedConfirmation()...)
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
		Extra:        extraTools(hub, loaded.plugins),
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
		Strategy:      cfg.Context.Strategy,
		Loop: agent.LoopGuard{
			Enabled:       *cfg.Loop.Enabled,
			Replies:       cfg.Loop.Replies,
			Repeats:       cfg.Loop.Repeats,
			StreamRepeats: cfg.Loop.StreamRepeats,
			Warnings:      *cfg.Loop.Warnings,
		},
		RewritePrompt: loaded.plugins.RewritePrompt,
		BlockToolCall: loaded.plugins.BlockToolCall,
	}
	reload := func(userLayer config.Config) (tui.Session, error) {
		if _, err := chooseProvider(context.Background(), config.UserSettings(userLayer), "", ""); err != nil {
			return tui.Session{}, fmt.Errorf("outside this project: %w", err)
		}
		fresh, err := config.LoadWithUserLayer(workDir, userLayer)
		if err != nil {
			return tui.Session{}, err
		}
		return buildSession(context.Background(), workDir, fresh, sessionChoice{yes: choice.yes}, loaded)
	}
	return tui.Session{
		Agent:     assistant,
		Provider:  chosen.name,
		Model:     chosen.model,
		Config:    cfg,
		WorkDir:   workDir,
		Reload:    reload,
		MCP:       hub.Status(),
		Plugins:   loaded.plugins,
		ToolNames: allToolNames(hub, loaded.plugins),
	}, nil
}

// extraTools puts the MCP tools first and then the plugin tools whose names
// are still free.
func extraTools(hub *mcphub.Hub, loaded *plugins.Set) []tools.Tool {
	mcpTools := hub.Tools()
	taken := tools.ReservedNames()
	for _, tool := range mcpTools {
		taken = append(taken, tool.Name)
	}
	return append(mcpTools, loaded.Tools(taken)...)
}

// allToolNames lists every tool a session can have, also the disabled ones,
// for the settings screen.
func allToolNames(hub *mcphub.Hub, loaded *plugins.Set) []string {
	names := tools.BuiltinNames()
	for _, tool := range hub.Tools() {
		names = append(names, tool.Name)
	}
	return append(names, loaded.ToolNames()...)
}

func toolEnv(workDir, memoryDir string) tools.Env {
	return tools.Env{
		WorkDir:        workDir,
		ProtectedPaths: []string{filepath.Dir(config.UserFile()), config.ProjectFile(workDir), memoryDir},
		MemoryDir:      memoryDir,
	}
}
