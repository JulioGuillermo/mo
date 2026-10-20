package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

const (
	shellUsage = "!<command> runs it in the shell, in this folder, for you alone; !!<command> also gives the model what it printed, with your next message"
	// shellNoteLimit is how much of a command's output the model is given,
	// its start and end kept.
	shellNoteLimit = 8000
)

// shellDoneMsg brings what a command run with ! printed; told is whether
// the model is told of it, as !! asks.
type shellDoneMsg struct {
	id, command, output string
	err                 error
	told                bool
}

// runShell is !command: run in the working directory, shown as a bash call,
// stopped by esc. With told, as !! asks, what it printed goes to the model
// with the next message; else only the user sees it.
func (m model) runShell(command string, told bool) (tea.Model, tea.Cmd) {
	if command == "" {
		return m.emit(m.printer.Note(shellUsage))
	}
	m.userRuns++
	id := fmt.Sprintf("user-shell-%d", m.userRuns)
	args, _ := json.Marshal(map[string]string{"command": command})
	m.addTool(llm.ToolCall{ID: id, Name: "bash", Args: args}, command)
	ctx, cancel := context.WithCancel(context.Background())
	if m.shells == nil {
		m.shells = map[string]context.CancelFunc{}
	}
	m.shells[id] = cancel
	workDir := m.session.WorkDir
	return m, func() tea.Msg {
		output, err := tools.RunUserCommand(ctx, workDir, command)
		if ctx.Err() != nil {
			err = errors.New("stopped with esc")
		}
		return shellDoneMsg{id: id, command: command, output: output, err: err, told: told}
	}
}

func (m model) finishShell(msg shellDoneMsg) (tea.Model, tea.Cmd) {
	if cancel, ok := m.shells[msg.id]; ok {
		cancel()
		delete(m.shells, msg.id)
	}
	output := msg.output
	ending := shellEnding(msg.err)
	if msg.err != nil {
		output = strings.TrimSpace(output + "\n" + msg.err.Error())
	}
	m.finishTool(msg.id, output, msg.err != nil)
	run := &userRun{command: msg.command, output: msg.output, note: agent.ShellNote(msg.command, ending, headAndTail(msg.output, shellNoteLimit))}
	if m.ranByUser == nil {
		m.ranByUser = map[string]*userRun{}
	}
	m.ranByUser[msg.id] = run
	if !msg.told {
		return m, m.readRepo()
	}
	m.tellLater(run)
	return m, tea.Batch(m.readRepo(), printText(m.printer.Note("the model reads this with your next message")))
}

// shellEnding says how a command ended, for the model: its exit code, or
// why it ended otherwise.
func shellEnding(err error) string {
	if err == nil {
		return "exit code 0"
	}
	if code, isExit := strings.CutPrefix(err.Error(), "exit status "); isExit {
		return "exit code " + code
	}
	return "ended: " + err.Error()
}

// stopShells stops the commands run with ! still going, telling whether
// there were any.
func (m model) stopShells() bool {
	for _, cancel := range m.shells {
		cancel()
	}
	return len(m.shells) > 0
}

// withShellNotes puts the commands run with !! since the last message, and
// what they printed, after the message as notes of mo's, for the model to
// read.
func (m *model) withShellNotes(prompt string) string {
	if len(m.shellNotes) == 0 {
		return prompt
	}
	return prompt + "\n\n" + m.takeShellNotes()
}

// takeShellNotes are the notes of the commands run with !! since the last
// message, which are then gone.
func (m *model) takeShellNotes() string {
	notes := strings.Join(m.shellNotes, "\n\n")
	m.shellNotes = nil
	return notes
}

func headAndTail(text string, limit int) string {
	text = strings.TrimRight(text, "\n")
	if len(text) <= limit {
		return text
	}
	half := limit / 2
	return text[:half] + fmt.Sprintf("\n… %d bytes left out …\n", len(text)-2*half) + text[len(text)-half:]
}
