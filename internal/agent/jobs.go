package agent

import (
	"context"
	"strings"

	"github.com/jg/mo/internal/llm"
)

// tellEndedJobs puts a note in the conversation for each background job
// that ended since the model was last told: in the last message when it is
// the user's side, so two user messages never follow each other.
func (a *Agent) tellEndedJobs() {
	var notes []string
	if jobs := a.Tools.Jobs(); jobs != nil {
		for _, status := range jobs.TakeEnded() {
			notes = append(notes, moNotePrefix+jobs.EndedNote(status))
		}
	}
	if uis := a.Tools.UIs(); uis != nil {
		for _, event := range uis.TakeEvents() {
			notes = append(notes, moNotePrefix+event.Note())
		}
	}
	if len(notes) == 0 {
		return
	}
	note := strings.Join(notes, "\n\n")
	if last := len(a.History) - 1; last >= 0 && a.History[last].Role != llm.RoleAssistant {
		a.History[last].Text += "\n\n" + note
		return
	}
	a.History = append(a.History, llm.Message{Role: llm.RoleUser, Text: note})
}

// HasEndedJobs tells whether a background job ended that the model has not
// heard of.
func (a *Agent) HasEndedJobs() bool {
	if a.Tools == nil {
		return false
	}
	jobs, uis := a.Tools.Jobs(), a.Tools.UIs()
	return (jobs != nil && jobs.HasEnded()) || (uis != nil && uis.HasEvents())
}

// RunEndedJobs lets the model react to background jobs that ended while it
// was idle: a turn that starts from their notes, without a prompt.
func (a *Agent) RunEndedJobs(ctx context.Context, hooks Hooks) error {
	if !a.HasEndedJobs() {
		return nil
	}
	a.loops = 0
	return a.turn(ctx, hooks)
}
