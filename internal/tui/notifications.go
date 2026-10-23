package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/notify"
)

const notifyTitle = "mo"

// notifyUser calls the user back, unless they are looking at mo and asked
// not to be called then.
func (m model) notifyUser(body string) tea.Cmd {
	settings := m.session.Config.Notify
	if settings.Focused == nil || (!*settings.Focused && m.focused) {
		return nil
	}
	return sendNotification(settings, body)
}

// sendNotification shows a notification and sounds it, each as set; the
// bell rings once though both ask for it.
func sendNotification(settings config.Notify, body string) tea.Cmd {
	sent := notify.Build(notify.Method(settings.Method), notifyTitle, body, os.Getenv)
	chime := notify.BuildChime(notify.Sound(settings.Sound), settings.SoundFile)
	if chime.Escape == sent.Escape {
		chime.Escape = ""
	}
	return tea.Batch(showNotification(sent), playChime(chime))
}

func showNotification(sent notify.Notification) tea.Cmd {
	switch {
	case sent.Escape != "":
		return tea.Raw(sent.Escape)
	case sent.Desktop != nil:
		return func() tea.Msg {
			_ = sent.Desktop()
			return nil
		}
	}
	return nil
}

func playChime(chime notify.Chime) tea.Cmd {
	switch {
	case chime.Escape != "":
		return tea.Raw(chime.Escape)
	case chime.Play != nil:
		return func() tea.Msg {
			_ = chime.Play()
			return nil
		}
	}
	return nil
}

// turnEnded is the notification for the end of a turn: the start of the
// last answer, which holds the question when the model asks one.
func (m model) turnEnded(err error) tea.Cmd {
	if finish := m.session.Config.Notify.Finish; finish == nil || !*finish {
		return nil
	}
	if err != nil {
		return m.notifyUser("stopped: " + err.Error())
	}
	history := m.session.Agent.History
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleAssistant && strings.TrimSpace(history[i].Text) != "" {
			return m.notifyUser(oneLine(history[i].Text, 160))
		}
	}
	return m.notifyUser("done")
}

// interactionAsked calls the user back when the agent waits on them: the
// first call to approve or the first question, and how many more.
func (m model) interactionAsked(interaction agent.Interaction) tea.Cmd {
	if approval := m.session.Config.Notify.Approval; approval == nil || !*approval {
		return nil
	}
	var body string
	if len(interaction.Approvals) > 0 {
		request := interaction.Approvals[0]
		body = "Run " + request.Call.Name + "? " + oneLine(request.Summary, 120)
	} else {
		body = oneLine(interaction.Questions[0].Text, 140)
	}
	if more := len(interaction.Approvals) + len(interaction.Questions) - 1; more > 0 {
		body += fmt.Sprintf(" (+%d more)", more)
	}
	return m.notifyUser(body)
}
