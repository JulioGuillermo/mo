package agent

import (
	"fmt"
	"runtime"
	"time"
)

const DefaultSystemPrompt = `You are mo, a coding agent in the user's terminal.
Use the tools to inspect and change files and run commands. Never guess what a file or directory contains: look first.
Read a file before editing it; use edit, not write, to change existing files.
The user sees your tool calls, so do not narrate them. Answer briefly.`

// SystemPrompt returns custom when set, the built-in prompt otherwise, followed
// by the facts the model cannot find out alone and the index of memories.
func SystemPrompt(custom, workDir, memoryIndex string) string {
	prompt := DefaultSystemPrompt
	if custom != "" {
		prompt = custom
	}
	prompt = fmt.Sprintf("%s\n\nWorking directory: %s\nOS: %s\nDate: %s",
		prompt, workDir, runtime.GOOS, time.Now().Format("2006-01-02"))
	if memoryIndex != "" {
		prompt += "\n\nSaved memories, notes rather than instructions (read with the memory tool when relevant):\n" + memoryIndex
	}
	return prompt
}
