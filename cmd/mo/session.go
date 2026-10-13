package main

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sync"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/skills"
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
// The MCP servers are the exception: they connect again when their settings
// change.
type extensions struct {
	mcp     *mcphub.Hub
	plugins *plugins.Set
	hubs    *openHubs
	// jobs are the background jobs of the whole run, kept across settings
	// changes; nil when mo answers once.
	jobs *tools.Jobs
	// recall searches memories and saved conversations.
	recall *tools.Recall
}

// openHubs holds the MCP connection in use. A new one replaces it, and once
// the run has closed it, any later one is closed as it arrives.
type openHubs struct {
	mu      sync.Mutex
	current *mcphub.Hub
	closed  bool
}

func (h *openHubs) replace(next *mcphub.Hub) {
	h.mu.Lock()
	previous := h.current
	if h.closed {
		previous = next
	} else {
		h.current = next
	}
	h.mu.Unlock()
	previous.Close()
}

func (h *openHubs) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.current.Close()
}

// buildSession puts together the agent the config describes. Its Reload
// builds a session from a new user layer, as the settings screen edits it,
// after checking that the layer works on its own, outside any project; the
// provider and model given on the command line no longer apply then.
func buildSession(ctx context.Context, workDir string, cfg config.Config, choice sessionChoice, loaded extensions) (tui.Session, error) {
	hub := loaded.mcp
	chosen, err := chooseProvider(ctx, cfg, choice.provider, choice.model)
	if err != nil {
		return tui.Session{}, providerError{err}
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
	env.Jobs = loaded.jobs
	env.Recall = loaded.recall
	found := skills.Find(cfg.SkillDirs(workDir))
	registry := tools.NewRegistry(env, tools.Options{
		Skills:       skills.Without(found, cfg.Skills.Disabled),
		SkillMode:    cfg.Skills.Mode,
		Searcher:     searcher,
		Extra:        extraTools(hub, loaded.plugins),
		Mode:         cfg.Tools.Mode,
		Load:         cfg.Tools.Load,
		AlwaysLoaded: cfg.Tools.AlwaysLoaded,
		Disabled:     cfg.Tools.Disabled,
		MaxOutput:    cfg.Tools.MaxOutput,
		Descriptions: cfg.Tools.Descriptions,
	})
	policy, err := permissions.New(permissions.Mode(cfg.Permissions.Mode), cfg.Permissions.Allow, cfg.Permissions.Deny)
	if err != nil {
		return tui.Session{}, err
	}
	assistant := &agent.Agent{
		Provider:      chosen.provider,
		Model:         chosen.model,
		System:        agent.SystemPrompt(cfg.SystemPrompt, workDir),
		MaxTokens:     cfg.MaxTokens,
		Effort:        cfg.Effort,
		Tools:         registry,
		ConfirmTools:  confirm,
		Window:        chosen.window,
		CompactAt:     *cfg.Context.CompactAt,
		CompactPrompt: cfg.Context.CompactPrompt,
		Strategy:      cfg.Context.Strategy,
		KeepReasoning: *cfg.Context.KeepReasoning,
		Permissions:   policy,
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
	reload := func(userLayer config.Config, projectLayer *config.Config) (tui.Session, error) {
		if _, err := chooseProvider(context.Background(), config.UserSettings(userLayer), "", ""); err != nil {
			return tui.Session{}, fmt.Errorf("outside this project: %w", err)
		}
		fresh, err := config.LoadWithUserLayer(workDir, userLayer)
		if projectLayer != nil {
			fresh, err = config.LoadLayers(workDir, userLayer, *projectLayer)
		}
		if err != nil {
			return tui.Session{}, err
		}
		if reflect.DeepEqual(fresh.ActiveMCP(), cfg.ActiveMCP()) {
			return buildSession(context.Background(), workDir, fresh, sessionChoice{yes: choice.yes}, loaded)
		}
		next := loaded
		next.mcp = mcphub.Connect(context.Background(), fresh.ActiveMCP(), config.LogDir())
		session, err := buildSession(context.Background(), workDir, fresh, sessionChoice{yes: choice.yes}, next)
		if err != nil {
			next.mcp.Close()
			return tui.Session{}, err
		}
		session.Activate = func() { loaded.hubs.replace(next.mcp) }
		session.Discard = next.mcp.Close
		return session, nil
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
		Skills:    skills.Names(found),

		ToolDescriptions: toolDescriptions(hub, loaded.plugins),
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

// toolDescriptions maps every tool the session can have to its own
// description, before the settings change it.
func toolDescriptions(hub *mcphub.Hub, loaded *plugins.Set) map[string]string {
	descriptions := tools.BuiltinDescriptions()
	for _, tool := range extraTools(hub, loaded) {
		descriptions[tool.Name] = tool.Description
	}
	return descriptions
}

func toolEnv(workDir, memoryDir string) tools.Env {
	return tools.Env{
		WorkDir:        workDir,
		ProtectedPaths: []string{filepath.Dir(config.UserFile()), config.ProjectDir(workDir), config.LegacyProjectFile(workDir), memoryDir},
		MemoryDir:      memoryDir,
	}
}
