package main

import (
	"context"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
)

// subagentBuilder makes the subagents' agents: the main agent's model, or
// the one agents.model names, its tools and permissions, and a system
// prompt that gives them their task. They have no title tool, no UIs and
// no background jobs, which belong to the conversation; a subagent at the
// deepest level has no agent tool.
func subagentBuilder(ctx context.Context, cfg config.Config, main chosenProvider, env tools.Env, options tools.Options,
	policy *permissions.Policy, confirm []string, system string) (team.Builder, error) {
	chosen := main
	if provider, model, ok := strings.Cut(cfg.Agents.Model, "/"); ok {
		var err error
		if chosen, err = chooseProvider(ctx, cfg, provider, model); err != nil {
			return nil, err
		}
	}
	return func(info team.Info, below tools.Subagents, parent tools.Parent) (*agent.Agent, error) {
		own := env
		own.Agents, own.Parent, own.Titles, own.UIs, own.Jobs = below, parent, nil, nil, nil
		registry := tools.NewRegistry(own, options)
		subagent := newAgent(cfg, chosen, registry, system+"\n\n"+team.Prompt(info, below != nil), confirm, policy)
		subagent.MaxSteps = cfg.Agents.MaxSteps
		return subagent, nil
	}, nil
}
