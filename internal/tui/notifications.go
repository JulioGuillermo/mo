package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	return sendNotification(notify.Method(settings.Method), body)
}

func sendNotification(method notify.Method, body string) tea.Cmd {
	sent := notify.Build(method, notifyTitle, body, os.Getenv)
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

func (m model) permissionAsked(request approvalMsg) tea.Cmd {
	if approval := m.session.Config.Notify.Approval; approval == nil || !*approval {
		return nil
	}
	return m.notifyUser("Run " + request.request.Call.Name + "? " + oneLine(request.request.Summary, 120))
}
