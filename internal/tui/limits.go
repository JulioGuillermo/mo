package tui

import (
	"fmt"
	"time"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

// knownLimit tells whether err is a provider's limit it names in a way mo
// recognises, said plainly instead of as it came.
func knownLimit(err error) bool {
	limit, limited := llm.LimitOf(err)
	return limited && limit.Known
}

// limitText is a known limit said plainly: which, and when it lifts; a
// short one was already retried.
func (m model) limitText(err error) string {
	limit, _ := llm.LimitOf(err)
	text := limit.Describe(m.session.Provider, time.Now())
	if !limit.Plan {
		text += fmt.Sprintf(" · still so after %d retries; try again in a moment", agent.LimitRetries)
	}
	return text
}
