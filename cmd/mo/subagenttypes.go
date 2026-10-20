package main

import (
	"context"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/skills"
	"github.com/jg/mo/internal/tools"
)

// typeModels reach the models the types of subagent name. Each starts
// being looked up when the session starts, in the background, so the
// first subagent of a type seldom waits and a model that fails to answer
// only fails its type.
type typeModels struct {
	lookups map[string]func() (chosenProvider, error)
}

func newTypeModels(ctx context.Context, cfg config.Config, loaded *plugins.Set) *typeModels {
	models := &typeModels{lookups: map[string]func() (chosenProvider, error){}}
	for _, kind := range cfg.Agents.Types {
		if kind.Model == "" || models.lookups[kind.Model] != nil {
			continue
		}
		provider, model, _ := strings.Cut(kind.Model, "/")
		lookup := sync.OnceValues(func() (chosenProvider, error) {
			return chooseProvider(ctx, cfg, provider, model, loaded)
		})
		models.lookups[kind.Model] = lookup
		go lookup()
	}
	return models
}

// get is the provider for "provider/model", waiting for its lookup.
func (m *typeModels) get(name string) (chosenProvider, error) {
	return m.lookups[name]()
}

// policyFor is the session's policy as a type narrows it: never looser.
func policyFor(session *permissions.Policy, kind config.AgentType) (*permissions.Policy, error) {
	if session == nil || (kind.Permissions == "" && len(kind.Deny) == 0) {
		return session, nil
	}
	return session.Narrowed(permissions.Mode(kind.Permissions), kind.Deny)
}

// optionsFor keeps of the session's tools and skills those a type names.
// The agent and tell_parent tools stay as the levels and can_start say.
func optionsFor(options tools.Options, kind config.AgentType) tools.Options {
	if len(kind.Tools) > 0 {
		names := tools.BuiltinNames()
		for _, extra := range options.Extra {
			names = append(names, extra.Name)
		}
		disabled := slices.Clone(options.Disabled)
		for _, name := range names {
			if name != tools.AgentName && name != tools.TellParentName && !matchesAny(kind.Tools, name) {
				disabled = append(disabled, name)
			}
		}
		options.Disabled = disabled
	}
	if len(kind.Skills) > 0 {
		options.Skills = slices.DeleteFunc(slices.Clone(options.Skills), func(skill skills.Skill) bool {
			return !matchesAny(kind.Skills, skill.Name)
		})
	}
	return options
}

// matchesAny tells whether name is one of patterns, where * stands for any
// characters.
func matchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	return false
}
