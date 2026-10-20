package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

const (
	userRunTell    = "run:tell"
	userRunSend    = "run:send"
	userRunCommand = "run:command"
	userRunOutput  = "run:output"
)

// userRun is a command the user ran with ! that ended, and the note that
// gives it to the model; told once the model has it or will.
type userRun struct {
	command string
	output  string
	note    string
	told    bool
}

// tellLater gives a run to the model with the next message.
func (m *model) tellLater(run *userRun) {
	run.told = true
	m.shellNotes = append(m.shellNotes, run.note)
}

// userRunAt is the command run with ! drawn at row y of the conversation,
// once it ended.
func (m model) userRunAt(y int) (*userRun, bool) {
	conversation := m.conversation(m.conversationHeight())
	i, found := m.conversationLines().blockAt(y + conversation.YOffset())
	if !found || m.blocks[i].tool == nil {
		return nil, false
	}
	run, ended := m.ranByUser[m.blocks[i].tool.id]
	return run, ended
}

// userRunOptions are what a command run with ! offers: giving it to the
// model, now when it is idle or with the next message, unless it has it
// already, and copying the command or what it printed.
func userRunOptions(run *userRun, modelWorks bool) []huh.Option[string] {
	var options []huh.Option[string]
	if !run.told {
		options = append(options, huh.NewOption("give it to the model with your next message", userRunTell))
		if !modelWorks {
			options = append(options, huh.NewOption("send it to the model now", userRunSend))
		}
	}
	return append(options,
		huh.NewOption("copy the command", userRunCommand),
		huh.NewOption("copy what it printed", userRunOutput),
	)
}

// userRunMenu is what a right click on a command run with ! offers.
func (m model) userRunMenu(run *userRun) (tea.Model, tea.Cmd) {
	description := "$ " + oneLine(run.command, 60)
	if run.told {
		description += "\nthe model has it, or reads it with your next message"
	}
	return m.showChoices("Your command", description, userRunOptions(run, m.running), func(m model, choice string) (tea.Model, tea.Cmd) {
		switch choice {
		case userRunTell:
			m.tellLater(run)
			return m.emit(m.printer.Note("the model reads this with your next message"))
		case userRunSend:
			return m.sendUserRun(run)
		case userRunCommand:
			return m.flashCopied(run.command, "copied the command")
		case userRunOutput:
			return m.flashCopied(strings.TrimRight(run.output, "\n"), "copied what it printed")
		}
		return m, nil
	})
}

// sendUserRun starts a turn that gives the model a command the user ran,
// with the other runs it was to read with the next message; while the
// model works, it reads them with the next message instead.
func (m model) sendUserRun(run *userRun) (tea.Model, tea.Cmd) {
	m.tellLater(run)
	if m.running {
		return m.emit(m.printer.Note("the model reads this with your next message"))
	}
	return m.beginTurn("!!"+run.command, m.takeShellNotes())
}
