package agent

import (
	"regexp"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
)

// Prompt is a prompt of the user in the history, to go back to.
type Prompt struct {
	Index int
	Text  string
}

// Prompts lists the user's prompts, oldest first, as they typed them. The
// one holding a compaction summary is left out: what it replaced is gone.
func (a *Agent) Prompts() []Prompt {
	return PromptsIn(a.History)
}

// PromptsIn are the prompts of a history.
func PromptsIn(history []llm.Message) []Prompt {
	var prompts []Prompt
	for i, message := range history {
		if IsPrompt(message) && !IsSummary(message) {
			prompts = append(prompts, Prompt{Index: i, Text: PromptText(message)})
		}
	}
	return prompts
}

// IsPrompt tells whether a message is something the user wrote, not a note
// of mo's.
func IsPrompt(message llm.Message) bool {
	return isPrompt(message)
}

// IsSummary tells whether a message starts with a compaction summary.
func IsSummary(message llm.Message) bool {
	return message.Role == llm.RoleUser && strings.HasPrefix(message.Text, summaryHeader)
}

// Summary returns the summary a compaction left in a message.
func Summary(message llm.Message) string {
	summary := strings.TrimPrefix(message.Text, summaryHeader)
	if before, _, found := strings.Cut(summary, "\n\n"+currentRequestHeader); found {
		return before
	}
	return summary
}

// PromptText is a prompt as the user typed it, without what mo added to
// it: the list of attached files, the skills loaded with it, its notes,
// and its skill references back as $name.
func PromptText(message llm.Message) string {
	text, _, _ := strings.Cut(message.Text, "\n\n"+moNotePrefix)
	return skillRefShown.ReplaceAllString(text, "$$$1")
}

// SkillBlock is a skill the user referenced that the model cannot load with
// its skill tool, sent whole after the prompt: what the skill says, its
// folder and its other files, between [MO SKILL: name] and its end.
func SkillBlock(name, loaded string) string {
	return moNotePrefix + "The user referenced the skill " + name + ", which you cannot load with the skill tool; here it is, follow it:\n" +
		"[MO SKILL: " + name + "]\n" + loaded + "\n[END MO SKILL: " + name + "]"
}

// SkillRef stands in a prompt for a skill the user named with $name.
func SkillRef(name string) string {
	return "[SKILL REF: " + name + "]"
}

var skillRefShown = regexp.MustCompile(`\[SKILL REF: ([^\]]+)\]`)

// SkillRequest asks the model, after a prompt, to load with its skill tool
// the skills the user referenced.
func SkillRequest(names []string) string {
	return moNotePrefix + "The user referenced skills: " + strings.Join(names, ", ") +
		". Load each one with the skill tool unless you already loaded it in this conversation, and follow them."
}

// Rewind drops the history from index on, to go on from before that prompt.
// The size of what is left is estimated until the next answer.
func (a *Agent) Rewind(index int) {
	a.History = slices.Clone(a.History[:min(max(index, 0), len(a.History))])
	a.LastUsage = llm.Usage{}
	a.historyAtUsage = 0
	a.sizeAfterCompaction = 0
	a.currentRequest = ""
}

// Restore puts a saved conversation in place of the current one.
func (a *Agent) Restore(history []llm.Message, usage llm.Usage) {
	a.Clear()
	a.History = history
	a.LastUsage = usage
	a.historyAtUsage = len(history)
}
