package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
)

// working tells whether the model or a subagent works, for a stop to reach.
func (m model) working() bool {
	return m.running || (m.session.Team != nil && m.session.Team.Working())
}

// escStop is esc while something works: each press stops sooner, after the
// round, after the calls running, then at once.
func (m model) escStop() (tea.Model, tea.Cmd) {
	switch m.stopping {
	case agent.StopNone, agent.StopAfterRound:
		m.stopAfter(agent.StopAfterCalls)
	default:
		m.stopNow()
	}
	return m, nil
}

// stopAfter asks the model and the subagents to stop as mode says, in place
// of a stop asked before. A stop that runs no more calls answers what they
// ask for: nothing more of theirs runs.
func (m *model) stopAfter(mode agent.StopMode) {
	m.stopping = mode
	if m.running {
		m.session.Agent.RequestStop(mode)
	}
	if mode == agent.StopAfterCalls {
		m.closeDialog(true)
	}
	if m.session.Team != nil {
		m.session.Team.StopAllAfter(mode)
	}
}

// stopNow stops the model, its calls and the subagents at once.
func (m *model) stopNow() {
	if m.running {
		m.stop()
	} else if m.session.Team != nil {
		m.session.Team.StopAll()
	}
	m.stopping = agent.StopNone
}

// stopCommand is /stop: after the calls running, as esc; next: after the
// round; now: at once; cancel: takes back a stop not done yet.
func (m model) stopCommand(argument string) (tea.Model, tea.Cmd) {
	if !m.working() {
		return m.emit(m.printer.Note("nothing is working"))
	}
	switch argument {
	case "":
		m.stopAfter(agent.StopAfterCalls)
	case "next":
		m.stopAfter(agent.StopAfterRound)
		return m.emit(m.printer.Note("stops once this round ends · /stop cancel takes it back"))
	case "now":
		m.stopNow()
	case "cancel":
		if m.stopping == agent.StopNone {
			return m.emit(m.printer.Note("no stop asked"))
		}
		m.stopping = agent.StopNone
		if m.running {
			m.session.Agent.ClearStop()
		}
		if m.session.Team != nil {
			m.session.Team.ClearStops()
		}
		return m.emit(m.printer.Note("stop taken back"))
	default:
		return m.emit(m.printer.Note("/stop, /stop next, /stop now or /stop cancel"))
	}
	return m, nil
}

// stoppingState is what the status line says of a stop asked.
func (m model) stoppingState(colors stateColors) (state, bool) {
	switch m.stopping {
	case agent.StopAfterRound:
		return state{word: "stopping", detail: "after this round · esc: after the calls running", tint: colors.waiting}, true
	case agent.StopAfterCalls:
		return state{word: "stopping", detail: "after the calls running · esc: now", tint: colors.waiting}, true
	}
	return state{}, false
}
