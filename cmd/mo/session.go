package main

import (
	"context"
	"fmt"
	"maps"
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
	mcp *mcphub.Hub
	// connecting is the first connection to the MCP servers, made while the
	// provider is chosen; mcp is unset until it is taken.
	connecting *pendingHub
	plugins    *plugins.Set
	hubs       *openHubs
	// jobs are the background jobs of the whole run, kept across settings
	// changes; nil when mo answers once.
	jobs *tools.Jobs
	// titles renames the conversation from the title tool; nil when mo
	// answers once.
	titles *tools.Titles
	// recall searches memories and saved conversations.
	recall *tools.Recall
	// uis are the interfaces the model draws; nil when mo answers once.
	uis *tools.UIs
}

// pendingHub is a connection to the MCP servers being made in the
// background; get waits for it.
type pendingHub struct {
	once  sync.Once
	ready chan *mcphub.Hub
	hub   *mcphub.Hub
	hubs  *openHubs
}

// connectInBackground starts connecting so the provider is chosen
// meanwhile.
func connectInBackground(connect func() *mcphub.Hub, hubs *openHubs) *pendingHub {
	pending := &pendingHub{ready: make(chan *mcphub.Hub, 1), hubs: hubs}
	go func() { pending.ready <- connect() }()
	return pending
}

func (p *pendingHub) get() *mcphub.Hub {
	p.once.Do(func() {
		p.hub = <-p.ready
		p.hubs.mu.Lock()
		if p.hubs.current == nil && !p.hubs.closed {
			p.hubs.current = p.hub
		}
		p.hubs.mu.Unlock()
	})
	return p.hub
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
	previous, closed := h.current, h.closed
	if !closed {
		h.current = next
	}
	h.mu.Unlock()
	switch {
	case closed:
		next.Close()
	case previous != nil && previous != next:
		// What next shares with previous stays open for next.
		previous.CloseExcept(next)
	}
}

func (h *openHubs) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.current != nil {
		h.current.Close()
	}
}

// buildSession puts together the agent the config describes. Its Reload
// builds a session from a new user layer, as the settings screen edits it,
// after checking that the layer works on its own, outside any project; the
// provider and model given on the command line no longer apply then.
func buildSession(ctx context.Context, workDir string, cfg config.Config, choice sessionChoice, loaded extensions) (tui.Session, error) {
	chosen, err := chooseProvider(ctx, cfg, choice.provider, choice.model)
	if err != nil {
		return tui.Session{}, providerError{err}
	}
	hub := loaded.mcp
	if hub == nil && loaded.connecting != nil {
		hub = loaded.connecting.get()
		loaded.mcp = hub
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
	// Checked when the config was read.
	readMaxSize, _ := config.ParseSize(cfg.Tools.ReadMaxSize)
	env := toolEnv(workDir, cfg.Memory.Dir)
	env.Jobs = loaded.jobs
	env.Titles = loaded.titles
	env.Recall = loaded.recall
	env.UIs = loaded.uis
	found := skills.Find(cfg.SkillDirs(workDir))
	instructions := agent.FindInstructions(cfg.InstructionFiles(), filepath.Join(config.ConfigHome(), "mo"), workDir)
	extra := extraTools(hub, loaded.plugins)
	modes, groups := toolModes(cfg.Tools, extra, hub.ToolServers())
	registry := tools.NewRegistry(env, tools.Options{
		Modes:        modes,
		Groups:       groups,
		SkillModes:   cfg.Skills.Modes,
		Skills:       skills.Without(found, cfg.Skills.Disabled),
		SkillMode:    cfg.Skills.Mode,
		Searcher:     searcher,
		Extra:        extra,
		Mode:         cfg.Tools.Mode,
		Load:         cfg.Tools.Load,
		AlwaysLoaded: cfg.Tools.AlwaysLoaded,
		Disabled:     cfg.Tools.Disabled,
		MaxOutput:    cfg.Tools.MaxOutput,
		ReadMaxSize:  readMaxSize,
		Descriptions: cfg.Tools.Descriptions,
	})
	policy, err := permissions.New(permissions.Mode(cfg.Permissions.Mode), cfg.Permissions.Allow, cfg.Permissions.Deny)
	if err != nil {
		return tui.Session{}, err
	}
	assistant := &agent.Agent{
		Provider:      chosen.provider,
		Model:         chosen.model,
		System:        agent.SystemPrompt(cfg.SystemPrompt, workDir, instructions),
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
		EarlyCalls:    cfg.Tools.Calls != "reply",
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
		// Only the servers that changed connect again; the others keep
		// their connection, shared until one of the two hubs goes.
		next.mcp = mcphub.Reconnect(context.Background(), hub, fresh.ActiveMCP(), config.LogDir())
		session, err := buildSession(context.Background(), workDir, fresh, sessionChoice{yes: choice.yes}, next)
		if err != nil {
			next.mcp.CloseExcept(hub)
			return tui.Session{}, err
		}
		session.Activate = func() { loaded.hubs.replace(next.mcp) }
		session.Discard = func() { next.mcp.CloseExcept(hub) }
		return session, nil
	}
	return tui.Session{
		Agent:        assistant,
		Provider:     chosen.name,
		WindowFrom:   chosen.windowFrom,
		Model:        chosen.model,
		Config:       cfg,
		WorkDir:      workDir,
		Reload:       reload,
		MCP:          hub.Status(),
		Plugins:      loaded.plugins,
		ToolNames:    allToolNames(hub, loaded.plugins),
		Skills:       skills.Names(found),
		Instructions: instructionPaths(instructions),

		ToolDescriptions: toolDescriptions(hub, loaded.plugins),
	}, nil
}

// toolModes are the modes of single tools, an MCP server's mode given to
// its tools that have none of their own, and the groups: the tools of the
// servers in group mode, under the server's name.
func toolModes(settings config.Tools, extra []tools.Tool, servers map[string]string) (map[string]tools.Mode, map[string]string) {
	modes := maps.Clone(settings.Modes)
	if modes == nil {
		modes = map[string]tools.Mode{}
	}
	groups := map[string]string{}
	for _, tool := range extra {
		server, ok := servers[tool.Name]
		if !ok {
			continue
		}
		switch mode := settings.MCPModes[server]; {
		case mode == tools.ModeGroup:
			groups[tool.Name] = server
		case mode != "" && modes[tool.Name] == "":
			modes[tool.Name] = mode
		}
	}
	return modes, groups
}

func instructionPaths(found []agent.Instruction) []string {
	var paths []string
	for _, instruction := range found {
		paths = append(paths, instruction.Path)
	}
	return paths
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
