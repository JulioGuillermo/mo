package agent

import (
	"strings"
	"sync"

	"github.com/jg/mo/internal/llm"
)

// notes are what the user did that the model should know, such as editing
// its task list, kept until its next step.
type notes struct {
	mu    sync.Mutex
	texts []string
}

// Tell keeps a note for the model, which reads it at its next step, or
// with the next prompt when it is idle; it starts no turn.
func (a *Agent) Tell(text string) {
	a.told.mu.Lock()
	defer a.told.mu.Unlock()
	a.told.texts = append(a.told.texts, text)
}

// tellNotes puts the kept notes in the history, on the last message of the
// user's or as one of their own, as ended jobs are told.
func (a *Agent) tellNotes() {
	a.told.mu.Lock()
	texts := a.told.texts
	a.told.texts = nil
	a.told.mu.Unlock()
	if len(texts) == 0 {
		return
	}
	for i, text := range texts {
		texts[i] = moNotePrefix + text
	}
	note := strings.Join(texts, "\n\n")
	if last := len(a.History) - 1; last >= 0 && a.History[last].Role != llm.RoleAssistant {
		a.History[last].Text += "\n\n" + note
		return
	}
	a.History = append(a.History, stamped(llm.Message{Role: llm.RoleUser, Text: note}))
}
