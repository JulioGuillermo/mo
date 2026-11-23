package agent

import (
	"slices"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const readOnlyResult = "/btw only reads: this call would change something. Answer with what you can read, or tell the user to use /btw! for it."

// Aside is an agent for a side question, as /btw asks one: the same model,
// prompt and permissions, its own tools, and a copy of history without the
// calls still waiting for their results. readOnly blocks every call that
// would change something.
func (a *Agent) Aside(registry *tools.Registry, history []llm.Message, readOnly bool) *Agent {
	a.modelMu.RLock()
	model, window := a.Model, a.Window
	a.modelMu.RUnlock()
	aside := &Agent{
		Provider:      a.Provider,
		ProviderName:  a.ProviderName,
		Model:         model,
		Window:        window,
		System:        a.System,
		MaxTokens:     a.MaxTokens,
		Effort:        a.Effort,
		Tools:         registry,
		ConfirmTools:  a.ConfirmTools,
		CompactAt:     a.CompactAt,
		CompactPrompt: a.CompactPrompt,
		Strategy:      a.Strategy,
		KeepReasoning: a.KeepReasoning,
		Loop:          a.Loop,
		EarlyCalls:    a.EarlyCalls,
		MaxSteps:      a.MaxSteps,
		Permissions:   a.Permissions,
		BlockToolCall: a.BlockToolCall,
		AfterToolCall: a.AfterToolCall,
		ReadOnly:      readOnly,
		History:       withoutPendingCalls(history),
	}
	aside.SetVisuals(a.visualsNote())
	return aside
}

// withoutPendingCalls is a copy of history without a last reply whose calls
// have no results yet.
func withoutPendingCalls(history []llm.Message) []llm.Message {
	kept := slices.Clone(history)
	last := len(kept) - 1
	if last >= 0 && kept[last].Role == llm.RoleAssistant && len(kept[last].ToolCalls) > 0 {
		kept = kept[:last]
	}
	return kept
}

// ChangesFrom lists the calls from the message at start on that ran well
// and may have changed something, each as its tool and summary.
func (a *Agent) ChangesFrom(start int) []string {
	succeeded := map[string]bool{}
	for _, message := range a.History[start:] {
		if message.Role == llm.RoleTool && !message.IsError {
			succeeded[message.ToolCallID] = true
		}
	}
	var changes []string
	for _, message := range a.History[start:] {
		for _, call := range message.ToolCalls {
			inner := a.unwrapped(call)
			if !succeeded[call.ID] || !a.mayChange(inner) {
				continue
			}
			changes = append(changes, inner.Name+" "+a.Tools.Summary(inner))
		}
	}
	return changes
}
