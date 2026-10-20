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

// PromptText is a prompt as the user typed it: its Input when mo kept one,
// else its text. Conversations from before Input are read without mo's
// notes and with the skill references back as $name.
func PromptText(message llm.Message) string {
	if message.Input != "" {
		return message.Input
	}
	return withoutNotes(message.Text)
}

// SentPrompt is what a prompt sent to the model said, without mo's notes
// after it: the attachments in it are their paths.
func SentPrompt(message llm.Message) string {
	return withoutNotes(message.Text)
}

func withoutNotes(text string) string {
	text, _, _ = strings.Cut(text, "\n\n"+moNotePrefix)
	return skillRefShown.ReplaceAllString(text, "$$$1")
}

// SkillBlock is a skill the user referenced that the model cannot load with
// its skill tool, sent whole after the prompt: what the skill says, its
// folder and its other files, between [MO SKILL: name] and its end.
func SkillBlock(name, loaded string) string {
	return moNotePrefix + "The user referenced the skill " + name + ", which you cannot load with the skill tool; here it is, follow it:\n" +
		"[MO SKILL: " + name + "]\n" + loaded + "\n[END MO SKILL: " + name + "]"
}

// ShellNote tells the model of a command the user ran in the shell, so it
// does not take it for one of its own: the command, how it ended and what
// it printed.
func ShellNote(command, ending, output string) string {
	return moNotePrefix + "The user ran this command in the shell themselves; you did not run it.\n$ " + command +
		"\n" + ending + "\n<output>\n" + strings.TrimRight(output, "\n") + "\n</output>"
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

// withInput keeps typed as the message's Input when its text is not typed
// as it is, or would not read back as it: typed text that looks like mo's
// notes or skill references.
func withInput(message llm.Message, typed string) llm.Message {
	message.Input = ""
	if message.Text != typed || withoutNotes(message.Text) != typed {
		message.Input = typed
	}
	return message
}
