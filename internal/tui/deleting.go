package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/sessions"
)

const (
	removeMark = "✕"
	resumeHelp = "enter resume · ctrl+x, delete or ✕ deletes one · type to filter · esc back"
	cleanUsage = "usage: /clean <date or age>: /clean 2026-08-01, /clean 2 months, /clean 3 weeks, /clean 10 days"
)

// deletedMsg says which conversations were deleted, and whether to show
// the list of the rest again.
type deletedMsg struct {
	ids    []string
	err    error
	toList bool
}

// confirm asks a yes/no question before doing something that cannot be
// undone; no goes back.
func (m model) confirm(title, description string, yes, back func(m model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	sure := false
	form := newForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Description(description).Affirmative("Yes, delete").Negative("No").Value(&sure),
	)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		if !sure {
			return back(m)
		}
		return yes(m)
	}
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: "←→ choose · enter confirm · esc back"})
}

// deleteCurrent deletes the conversation in progress, after asking, and
// starts a new one.
func (m model) deleteCurrent() (tea.Model, tea.Cmd) {
	if m.running {
		return m.emit(m.printer.Note("stop the current run first"))
	}
	if m.thread.id == "" {
		return m.emit(m.printer.Note("nothing saved to delete yet"))
	}
	id, title := m.thread.id, orDefault(m.thread.title, "this conversation")
	stay := func(m model) (tea.Model, tea.Cmd) { return m.emit(m.printer.Note("kept")) }
	return m.confirm(fmt.Sprintf("Delete %q?", oneLine(title, shownPrompt)),
		"its messages and the output of its commands go for good · a new conversation starts",
		func(m model) (tea.Model, tea.Cmd) { return m, m.deleteConversations([]string{id}, false) }, stay)
}

// deleteConversations stops what the conversations run and deletes them,
// off the UI goroutine: stopping a job can take seconds.
func (m model) deleteConversations(ids []string, toList bool) tea.Cmd {
	jobs := m.jobs()
	return func() tea.Msg {
		var errs []error
		var deleted []string
		for _, id := range ids {
			if jobs != nil {
				jobs.KillConversation(id)
			}
			if err := sessions.Delete(config.SessionDir(), id); err != nil {
				errs = append(errs, err)
				continue
			}
			deleted = append(deleted, id)
		}
		return deletedMsg{ids: deleted, err: errors.Join(errs...), toList: toList}
	}
}

// conversationsDeleted starts a new conversation when the one in progress
// went, and shows the list again when the deletion came from it.
func (m model) conversationsDeleted(msg deletedMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, id := range msg.ids {
		if id == m.thread.id && id != "" {
			m.newConversation()
			cmds = append(cmds, printText(m.printer.Note("the conversation in progress was deleted · this is a new one")))
		}
	}
	switch {
	case msg.err != nil:
		cmds = append(cmds, printText(m.printer.Error(msg.err)))
	case len(msg.ids) == 1:
		cmds = append(cmds, printText(m.printer.Note("deleted 1 conversation")))
	default:
		cmds = append(cmds, printText(m.printer.Note(fmt.Sprintf("deleted %d conversations", len(msg.ids)))))
	}
	if msg.toList {
		cmds = append(cmds, listConversations(m.session.WorkDir, false))
	}
	return m, tea.Sequence(cmds...)
}

// removeFromList asks before deleting the conversation the list's cursor is
// on, and shows the list again either way.
func (m model) removeFromList(list *huh.Select[string], infos []sessions.Info) (tea.Model, tea.Cmd) {
	id, ok := list.Hovered()
	toList := func(m model) (tea.Model, tea.Cmd) { return m, listConversations(m.session.WorkDir, false) }
	if !ok {
		return toList(m)
	}
	if m.running && id == m.thread.id {
		updated, note := m.emit(m.printer.Note("stop the current run before deleting the conversation in progress"))
		return updated, tea.Batch(note, listConversations(m.session.WorkDir, false))
	}
	title := id
	for _, info := range infos {
		if info.ID == id {
			title = orDefault(info.Title, "(no prompt)")
		}
	}
	return m.confirm(fmt.Sprintf("Delete %q?", oneLine(title, shownPrompt)),
		"its messages and the output of its commands go for good",
		func(m model) (tea.Model, tea.Cmd) { return m, m.deleteConversations([]string{id}, true) }, toList)
}

// clean deletes, after asking, the conversations of this folder last
// updated before a date or an age, but the one in progress.
func (m model) clean(argument string) (tea.Model, tea.Cmd) {
	if argument == "" {
		return m.emit(m.printer.Note(cleanUsage))
	}
	cutoff, err := parseCutoff(argument, time.Now())
	if err != nil {
		return m.emit(m.printer.Note(err.Error() + " · " + cleanUsage))
	}
	old, err := sessions.Older(config.SessionDir(), config.ProjectKey(m.session.WorkDir), cutoff)
	if err != nil {
		return m.emitError(err)
	}
	var ids []string
	for _, info := range old {
		if info.ID != m.thread.id {
			ids = append(ids, info.ID)
		}
	}
	conversations := len(ids)
	ids = append(ids, orphanJobFolders(config.SessionDir(), cutoff, m.thread.id)...)
	if len(ids) == 0 {
		return m.emit(m.printer.Note("no conversation of this folder was last used before " + cutoff.Format("2006-01-02")))
	}
	stay := func(m model) (tea.Model, tea.Cmd) { return m.emit(m.printer.Note("nothing deleted")) }
	return m.confirm(fmt.Sprintf("Delete %d conversations?", conversations),
		"those of this folder last used before "+cutoff.Format("2006-01-02 15:04")+", with the output of their commands, go for good",
		func(m model) (tea.Model, tea.Cmd) { return m, m.deleteConversations(ids, false) }, stay)
}

// orphanJobFolders are the folders of commands whose conversation was never
// saved, older than cutoff: a first turn that failed leaves one.
func orphanJobFolders(dir string, cutoff time.Time, current string) []string {
	entries, _ := os.ReadDir(dir)
	var ids []string
	for _, entry := range entries {
		info, err := entry.Info()
		if !entry.IsDir() || err != nil || entry.Name() == current || !info.ModTime().Before(cutoff) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, entry.Name()+".json")); errors.Is(err, os.ErrNotExist) {
			ids = append(ids, entry.Name())
		}
	}
	return ids
}

// parseCutoff reads a date, 2026-08-01, or an age, "2 months", in English or
// Spanish, as the moment before which things are old.
func parseCutoff(argument string, now time.Time) (time.Time, error) {
	argument = strings.ToLower(strings.TrimSpace(argument))
	if date, err := time.ParseInLocation("2006-01-02", argument, time.Local); err == nil {
		return date, nil
	}
	fields := strings.Fields(argument)
	if len(fields) == 1 {
		split := strings.IndexFunc(argument, func(r rune) bool { return r < '0' || r > '9' })
		if split > 0 {
			fields = []string{argument[:split], argument[split:]}
		}
	}
	if len(fields) != 2 {
		return time.Time{}, fmt.Errorf("%q is not a date or an age", argument)
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count <= 0 {
		return time.Time{}, fmt.Errorf("%q is not a number above 0", fields[0])
	}
	switch strings.TrimSuffix(fields[1], ".") {
	case "d", "day", "days", "dia", "dias", "día", "días":
		return now.AddDate(0, 0, -count), nil
	case "w", "week", "weeks", "semana", "semanas":
		return now.AddDate(0, 0, -7*count), nil
	case "m", "mo", "month", "months", "mes", "meses":
		return now.AddDate(0, -count, 0), nil
	case "y", "year", "years", "año", "años", "ano", "anos":
		return now.AddDate(-count, 0, 0), nil
	}
	return time.Time{}, fmt.Errorf("%q is not days, weeks, months or years", fields[1])
}
