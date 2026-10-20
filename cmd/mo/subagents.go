package main

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
)

// subagentBuilder makes the subagents' agents: the main agent's model, or
// the one agents.model names, its tools and permissions, and a system
// prompt that gives them their task. They have no title tool, no UIs and
// no background jobs, which belong to the conversation; a subagent at the
// deepest level has no agent tool. A subagent of a type from the settings
// has what the type says instead.
func subagentBuilder(ctx context.Context, cfg config.Config, main chosenProvider, env tools.Env, options tools.Options,
	policy *permissions.Policy, confirm []string, system string, loaded *plugins.Set) (team.Builder, error) {
	plain := main
	if provider, model, ok := strings.Cut(cfg.Agents.Model, "/"); ok {
		var err error
		if plain, err = chooseProvider(ctx, cfg, provider, model, loaded); err != nil {
			return nil, err
		}
	}
	models := newTypeModels(ctx, cfg, loaded)
	return func(info team.Info, below tools.Subagents, parent tools.Parent) (*agent.Agent, error) {
		kind, err := typeOf(cfg.Agents.Types, info.Type)
		if err != nil {
			return nil, err
		}
		chosen := plain
		if kind.Model != "" {
			if chosen, err = models.get(kind.Model); err != nil {
				return nil, fmt.Errorf("subagent type %s: %w", info.Type, err)
			}
		}
		if info.Type != "" && !kind.CanStart {
			below = nil
		}
		ownPolicy, err := policyFor(policy, kind)
		if err != nil {
			return nil, fmt.Errorf("subagent type %s: %w", info.Type, err)
		}
		own := env
		own.Agents, own.Parent, own.Titles, own.UIs, own.Jobs, own.Peers = below, parent, nil, nil, nil, nil
		registry := tools.NewRegistry(own, optionsFor(options, kind))
		prompt := system + "\n\n" + team.Prompt(info, below != nil)
		if kind.Prompt != "" {
			prompt += "\n\n" + kind.Prompt
		}
		subagent := newAgent(cfg, chosen, registry, prompt, confirm, ownPolicy)
		subagent.MaxSteps = cfg.Agents.MaxSteps
		if kind.MaxSteps > 0 {
			subagent.MaxSteps = kind.MaxSteps
		}
		if kind.Effort != "" {
			subagent.Effort = kind.Effort
		}
		return subagent, nil
	}, nil
}

// typeOf is the settings of a type of subagent; no type is the plain one.
func typeOf(types map[string]config.AgentType, name string) (config.AgentType, error) {
	if name == "" {
		return config.AgentType{}, nil
	}
	kind, known := types[name]
	if !known {
		if len(types) == 0 {
			return kind, fmt.Errorf("no subagent type %q: there are no types, leave type out", name)
		}
		return kind, fmt.Errorf("no subagent type %q: use %s, or leave type out", name, strings.Join(slices.Sorted(maps.Keys(types)), ", "))
	}
	return kind, nil
}

// agentKinds are the types of subagent as the agent tool offers them.
func agentKinds(types map[string]config.AgentType) []tools.AgentKind {
	var kinds []tools.AgentKind
	for _, name := range slices.Sorted(maps.Keys(types)) {
		kind := types[name]
		kinds = append(kinds, tools.AgentKind{Name: name, Description: kind.Description, Model: kind.Model, Effort: kind.Effort,
			Tools: kind.Tools, Skills: kind.Skills, CanStart: kind.CanStart, Permissions: kind.Permissions, MaxSteps: kind.MaxSteps})
	}
	return kinds
}
