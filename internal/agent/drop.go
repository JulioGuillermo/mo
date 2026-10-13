package agent

import (
	"fmt"

	"github.com/jg/mo/internal/llm"
)

const (
	StrategyCompact = "compact"
	StrategyDrop    = "drop"

	dropTargetShare = 0.75
)

// dropOldest removes whole turns from the start of the history, oldest
// first, until the context is under dropTargetShare of the compaction
// threshold. A turn starts at a user message, so no tool result loses its
// call, and the turn in progress is never dropped.
func (a *Agent) dropOldest(hooks Hooks) {
	before := a.ContextUsed()
	target := int(float64(a.Window) * a.CompactAt * dropTargetShare)
	a.LastUsage = llm.Usage{}
	a.historyAtUsage = 0
	dropped := 0
	for a.ContextUsed() > target {
		next := a.nextTurnStart()
		if next == 0 {
			break
		}
		a.History = a.History[next:]
		dropped += next
	}
	a.sizeAfterCompaction = a.ContextUsed()
	if hooks.OnNotice == nil {
		return
	}
	if dropped == 0 {
		hooks.OnNotice("the turn in progress alone fills the context; /compact or /clear can help")
		return
	}
	hooks.OnNotice(fmt.Sprintf("dropped the %d oldest messages: %s → %s tokens", dropped, approxTokens(before), approxTokens(a.ContextUsed())))
}

// nextTurnStart is where the second turn begins, or 0 when only the turn in
// progress is left.
func (a *Agent) nextTurnStart() int {
	for i := 1; i < len(a.History); i++ {
		if a.History[i].Role == llm.RoleUser {
			return i
		}
	}
	return 0
}

func approxTokens(count int) string {
	if count < 1000 {
		return fmt.Sprint(count)
	}
	return fmt.Sprintf("%.1fk", float64(count)/1000)
}
