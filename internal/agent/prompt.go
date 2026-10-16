package agent

import (
	"fmt"
	"runtime"
	"time"

	"github.com/jg/mo/internal/tools"
)

const DefaultSystemPrompt = `You are mo, a coding agent in the user's terminal.
Use the tools to inspect and change files and run commands. Never guess what a file or directory contains: look first.
Read a file before editing it; use edit, not write, to change existing files.
The user sees your tool calls, so do not narrate them. Answer briefly.`

// SystemPrompt returns custom when set, the built-in prompt otherwise, followed
// by the facts the model cannot find out alone, how to see a part of a
// tool's output, and the instructions files
// found. Memories are not listed: the model searches them with recall when
// it needs them.
func SystemPrompt(custom, workDir string, instructions []Instruction) string {
	prompt := DefaultSystemPrompt
	if custom != "" {
		prompt = custom
	}
	prompt = fmt.Sprintf("%s\n\nWorking directory: %s\nOS: %s\nDate: %s\n\n%s",
		prompt, workDir, runtime.GOOS, time.Now().Format("2006-01-02"), tools.ViewGuide)
	return prompt + instructionsText(instructions)
}
