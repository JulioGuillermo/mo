package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/plugins"
)

// pluginSendMsg brings what a plugin sends to the model.
type pluginSendMsg plugins.SentMessage

// pluginJudgedMsg brings what the model made of a plugin's message in auto
// mode.
type pluginJudgedMsg struct {
	message plugins.SentMessage
	safe    bool
	reason  string
}

// pluginRetryMsg asks again about a plugin's message once nothing else is
// open.
type pluginRetryMsg struct{}

// pluginSends is how the messages of plugins stand: those waiting for the
// user, the plugins let in for the session, and how many got in unasked
// since the user last wrote.
type pluginSends struct {
	pending   []pendingPluginMessage
	trusted   map[string]bool
	streak    int
	postponed bool
	retrying  bool
}

type pendingPluginMessage struct {
	message plugins.SentMessage
	reason  string
}

func (m *model) sends() *pluginSends {
	if m.pluginSends == nil {
		m.pluginSends = &pluginSends{trusted: map[string]bool{}}
	}
	return m.pluginSends
}

// pluginSent decides on a plugin's message as the permission mode says:
// let in under all, judged under auto, asked about otherwise.
func (m model) pluginSent(message plugins.SentMessage) (tea.Model, tea.Cmd) {
	if m.session.Agent == nil {
		return m, nil
	}
	approval := m.session.Agent.IncomingApproval("tools")
	switch {
	case approval == "allow" || m.sends().trusted[message.Plugin]:
		return m.letPluginInUnasked(message, "")
	case approval == "auto":
		assistant := m.session.Agent
		return m, func() tea.Msg {
			safe, reason := assistant.JudgePlugin(context.Background(), message.Plugin, message.Title, message.Text)
			return pluginJudgedMsg{message: message, safe: safe, reason: reason}
		}
	}
	return m.askAboutPlugin(message, "")
}

func (m model) pluginJudged(msg pluginJudgedMsg) (tea.Model, tea.Cmd) {
	if msg.safe {
		return m.letPluginInUnasked(msg.message, "auto: "+msg.reason)
	}
	return m.askAboutPlugin(msg.message, "auto: "+msg.reason)
}

// letPluginInUnasked lets a message in without the user, unless too many
// came so since the user last wrote: a hook that sends at every turn's end
// would otherwise wake the model for ever.
func (m model) letPluginInUnasked(message plugins.SentMessage, status string) (tea.Model, tea.Cmd) {
	sends := m.sends()
	if sends.streak >= peerStreakMost {
		return m.askAboutPlugin(message, fmt.Sprintf("%d messages from plugins in a row without you", peerStreakMost))
	}
	sends.streak++
	return m.letPluginIn(message, status)
}

// letPluginIn shows the message and gives it to the model.
func (m model) letPluginIn(message plugins.SentMessage, status string) (tea.Model, tea.Cmd) {
	shown, show := m.emit(m.printer.PluginIn(message.Plugin, message.Title, message.Text, status))
	delivered, deliver := shown.(model).deliverPlugin(message)
	return delivered, tea.Batch(show, deliver)
}

// askAboutPlugin shows the message, waiting, and asks the user whether to
// let it in.
func (m model) askAboutPlugin(message plugins.SentMessage, reason string) (tea.Model, tea.Cmd) {
	status := "waiting for you"
	if reason != "" {
		status += " · " + reason
	}
	sends := m.sends()
	sends.pending = append(sends.pending, pendingPluginMessage{message: message, reason: reason})
	sends.postponed = false
	shown, show := m.emit(m.printer.PluginIn(message.Plugin, message.Title, message.Text, status))
	m = shown.(model)
	asked, ask := m.askNextPlugin()
	return asked, tea.Batch(show, ask)
}

// askNextPlugin opens the question about the first message that waits, or
// tries again in a moment while something else is open.
func (m model) askNextPlugin() (tea.Model, tea.Cmd) {
	sends := m.sends()
	if len(sends.pending) == 0 || sends.postponed || (m.popup != nil && m.popup.plugin) {
		return m, nil
	}
	if m.popup != nil || m.dialog != nil || m.settings != nil {
		if sends.retrying {
			return m, nil
		}
		sends.retrying = true
		return m, tea.Tick(peerRetry, func(time.Time) tea.Msg { return pluginRetryMsg{} })
	}
	waiting := sends.pending[0]
	name := waiting.message.Plugin
	description := "It is above, in the conversation."
	if waiting.reason != "" {
		description = waiting.reason + ". " + description
	}
	if len(sends.pending) > 1 {
		description += fmt.Sprintf(" %d more wait.", len(sends.pending)-1)
	}
	m.popup = &popup{
		title:       "⇠ plugin " + name + " sends the model: " + waiting.message.Title,
		description: description,
		options: []huh.Option[string]{
			huh.NewOption("Let it in", "in"),
			huh.NewOption("Let in all from "+name+" this session", "trust"),
			huh.NewOption("Reject it", "reject"),
		},
		border: peerPalette(m.printer.isDark()).incoming,
		plugin: true,
		do: func(m model, choice string) (tea.Model, tea.Cmd) {
			return m.answerPlugin(choice)
		},
		dismiss: func(m model) (tea.Model, tea.Cmd) {
			m.sends().postponed = true
			return m.emit(m.printer.Note("a plugin's message waits for you · /plugins answers it"))
		},
	}
	return m, nil
}

func (m model) pluginRetried() (tea.Model, tea.Cmd) {
	m.sends().retrying = false
	return m.askNextPlugin()
}

// answerPlugin does what the user chose for the first message that waits,
// then asks about the next.
func (m model) answerPlugin(choice string) (tea.Model, tea.Cmd) {
	sends := m.sends()
	if len(sends.pending) == 0 {
		return m, nil
	}
	waiting := sends.pending[0]
	sends.pending = sends.pending[1:]
	name := waiting.message.Plugin
	var done tea.Cmd
	if choice == "reject" {
		var shown tea.Model
		shown, done = m.emit(m.printer.Note("message from plugin " + name + " rejected"))
		m = shown.(model)
	} else {
		sends.streak = 0
		note := "message from plugin " + name + " let in"
		if choice == "trust" {
			sends.trusted[name] = true
			note += " · and all it sends this session"
		}
		var shown tea.Model
		shown, done = m.emit(m.printer.Note(note))
		m = shown.(model)
		delivered, deliver := m.deliverPlugin(waiting.message)
		m = delivered.(model)
		done = tea.Batch(done, deliver)
	}
	asked, ask := m.askNextPlugin()
	return asked, tea.Batch(done, ask)
}

// deliverPlugin gives the model a message, already shown: at its next
// step while it works, else in a turn of its own when the plugin asked to
// wake it, else with the next message.
func (m model) deliverPlugin(message plugins.SentMessage) (tea.Model, tea.Cmd) {
	if message.Wake {
		m.session.Agent.DeliverPluginMessage(message.Plugin, message.Title, message.Text)
	} else {
		m.session.Agent.Tell(agent.PluginNote(message.Plugin, message.Title, message.Text))
	}
	if m.running || !message.Wake {
		return m, nil
	}
	return m.reactToJobs()
}

// askPostponedPlugin asks again about the messages the user put off.
func (m model) askPostponedPlugin() (tea.Model, tea.Cmd) {
	if m.pluginSends == nil || len(m.pluginSends.pending) == 0 {
		return m, nil
	}
	m.pluginSends.postponed = false
	return m.askNextPlugin()
}
