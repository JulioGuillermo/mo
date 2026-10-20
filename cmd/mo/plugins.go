package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/plugins"
)

// openPlugins holds the plugins in use. A set loaded with new settings
// takes over from it; the one in use when the run ends is closed.
type openPlugins struct {
	mu      sync.Mutex
	current *plugins.Set
	closed  bool
}

func (o *openPlugins) replace(next *plugins.Set) {
	o.mu.Lock()
	previous, closed := o.current, o.closed
	if !closed {
		o.current = next
	}
	o.mu.Unlock()
	if closed {
		next.Close()
		return
	}
	next.TakeOver(previous)
}

func (o *openPlugins) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.current.Close()
}

// loadPlugins runs the user's plugins and reports the ones that failed.
func loadPlugins(workDir string, settings map[string]map[string]any) *plugins.Set {
	loaded := plugins.Load(config.PluginDir(), workDir, config.LogDir(), settings)
	for _, status := range loaded.Status() {
		if status.Err != nil {
			fmt.Fprintf(os.Stderr, "mo: plugin %s: %v\n", status.Name, status.Err)
		}
	}
	return loaded
}

// activeServers are the MCP servers of the settings and those the plugins
// declare, a plugin's left out when a server of the settings has its name,
// without those turned off.
func activeServers(cfg config.Config, loaded *plugins.Set) map[string]config.MCPServer {
	servers := cfg.ActiveMCP()
	for name, server := range loaded.MCPServers(slices.Collect(maps.Keys(cfg.MCP))) {
		if !slices.Contains(cfg.Tools.MCPDisabled, name) {
			servers[name] = server
		}
	}
	return servers
}

// withPluginProviders adds to cfg the providers the plugins declare, a
// plugin's left out when one of the settings has its name.
func withPluginProviders(cfg config.Config, loaded *plugins.Set) config.Config {
	declared := loaded.Providers(slices.Collect(maps.Keys(cfg.Providers)))
	if len(declared) == 0 {
		return cfg
	}
	cfg.Providers = maps.Clone(cfg.Providers)
	if cfg.Providers == nil {
		cfg.Providers = map[string]config.ProviderConfig{}
	}
	maps.Copy(cfg.Providers, declared)
	return cfg
}
