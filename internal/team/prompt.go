package team

import "fmt"

// Prompt tells a subagent what it is, after the system prompt it shares
// with the main agent.
func Prompt(info Info, canStart bool) string {
	text := fmt.Sprintf("You are a subagent, %q (agent %d, level %d), started by another agent for one task, which is your first message. "+
		"Work on it with your tools until it is done, then end with a report: what you found or did, with the paths and facts the one "+
		"who started you needs, and what is left open. Only that last reply reaches it; it may read the rest if it asks. "+
		"tell_parent tells it something sooner, only when it matters to it or the user asks you to. "+
		"Do not ask about what you can find out yourself; ask the user only when you cannot go on without them.", info.Name, info.ID, info.Depth)
	if canStart {
		text += " You may hand parts of your task to subagents of your own with the agent tool."
	}
	return text
}
