package main

import (
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/tools"
)

// btwTools makes the tools of /btw, or of /btw! with bang: those the
// settings name of the session's, with no subagents, titles, UIs,
// background jobs or peers, which belong to the conversation.
func btwTools(env tools.Env, options tools.Options, settings config.Tools) func(bang bool) *tools.Registry {
	env.Agents, env.Parent, env.Titles, env.UIs, env.Jobs, env.Peers = nil, nil, nil, nil, nil, nil
	return func(bang bool) *tools.Registry {
		allowed := settings.Btw
		if bang {
			allowed = settings.BtwBang
		}
		return tools.NewRegistry(env, onlyTools(options, allowed))
	}
}
