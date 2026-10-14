package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

const (
	menuClose      = "\x00close"
	jobDialogLines = 30
	contextHelp    = "↑↓ choose · enter do it · esc close"
)

// showMenu opens a menu of what can be done with something: a list of
// options whose pick do runs; esc closes it.
func (m model) showChoices(title, description string, options []huh.Option[string], do func(m model, choice string) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	choice := ""
	if len(options) > 0 {
		choice = options[0].Value
	}
	options = append(options, huh.NewOption("close", menuClose))
	list := huh.NewSelect[string]().Title(title).Description(description).Options(options...).Value(&choice)
	form := huh.NewForm(huh.NewGroup(list)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		if choice == menuClose {
			return m, nil
		}
		return do(m, choice)
	}
	closed := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	return m.showSettings(&settingsScreen{form: form, next: next, back: closed, help: contextHelp})
}

// modeMenu lists the permission modes to pick one for the session.
func (m model) modeMenu() (tea.Model, tea.Cmd) {
	policy := m.session.Agent.Permissions
	if policy == nil {
		return m, nil
	}
	options := slices.Clone(modeOptions)
	for i, option := range options {
		if permissions.Mode(option.Value) == policy.Mode() {
			options[i] = huh.NewOption(option.Key+"  ● now", option.Value)
		}
	}
	return m.showChoices("Permission mode", "for this session · /settings › Permissions sets the one to start in", options,
		func(m model, choice string) (tea.Model, tea.Cmd) {
			policy.SetMode(permissions.Mode(choice))
			return m, nil
		})
}

// jobMenu is what a right click on a background job offers.
func (m model) jobMenu(id int) (tea.Model, tea.Cmd) {
	jobs := m.jobs()
	if jobs == nil {
		return m, nil
	}
	status, _, err := jobs.Output(id, 0)
	if err != nil {
		return m.emitError(err)
	}
	options := []huh.Option[string]{huh.NewOption("watch its output live: scroll, search, copy", "watch"), huh.NewOption("see its last lines and how it stands", "show")}
	options = append(options, runningOptions(status)...)
	return m.showChoices(fmt.Sprintf("Job %d", id), oneLine(status.Command, max(m.width-4, 20)), options,
		func(m model, choice string) (tea.Model, tea.Cmd) {
			if updated, cmd, done := m.jobChoice(id, choice); done {
				return updated, cmd
			}
			return m.jobDialog(id)
		})
}

// jobDialog shows how a job stands, for how long, and its last lines; it
// can be refreshed, and killed while it runs.
func (m model) jobDialog(id int) (tea.Model, tea.Cmd) {
	jobs := m.jobs()
	if jobs == nil {
		return m, nil
	}
	lines := max(min(jobDialogLines, m.height-10), 3)
	status, output, err := jobs.Output(id, lines)
	if err != nil {
		return m.emitError(err)
	}
	if strings.TrimSpace(output) == "" {
		output = "(no output yet)"
	}
	options := []huh.Option[string]{huh.NewOption("refresh", "refresh"), huh.NewOption("watch its output live: scroll, search, copy", "watch")}
	options = append(options, runningOptions(status)...)
	description := status.Describe() + "\n\n" + dimStyle.Render(output)
	return m.showChoices(fmt.Sprintf("Job %d", id), description, options,
		func(m model, choice string) (tea.Model, tea.Cmd) {
			if updated, cmd, done := m.jobChoice(id, choice); done {
				return updated, cmd
			}
			return m.jobDialog(id)
		})
}

// runningOptions are what can be done to a job while it runs: send it to
// the background when it blocks the model, and kill it.
func runningOptions(status tools.JobStatus) []huh.Option[string] {
	if status.State != tools.JobRunning {
		return nil
	}
	var options []huh.Option[string]
	if status.Foreground {
		options = append(options, huh.NewOption("send it to the background, so the model goes on", "background"))
	}
	return append(options, huh.NewOption("kill it", "kill"))
}

// jobChoice does what was picked in a job's menu, when it is an action.
func (m model) jobChoice(id int, choice string) (tea.Model, tea.Cmd, bool) {
	switch choice {
	case "watch":
		updated, cmd := m.openViewer(id)
		return updated, cmd, true
	case "kill":
		updated, cmd := m.killFromMenu(id)
		return updated, cmd, true
	case "background":
		jobs := m.jobs()
		if err := jobs.DetachJob(id); err != nil {
			updated, cmd := m.emitError(err)
			return updated, cmd, true
		}
		tick := m.tickJobs()
		updated, cmd := m.emit(m.printer.Note(fmt.Sprintf("job %d goes on in the background", id)))
		return updated, tea.Batch(cmd, tick), true
	}
	return m, nil, false
}

func (m model) killFromMenu(id int) (tea.Model, tea.Cmd) {
	jobs := m.jobs()
	if err := jobs.Kill(id); err != nil {
		return m.emitError(err)
	}
	return m.emit(m.printer.Note(fmt.Sprintf("job %d killed", id)))
}

// taskMenu changes the state of one of the model's tasks or deletes it, and
// tells the model at its next step.
func (m model) taskMenu(index int) (tea.Model, tea.Cmd) {
	if index >= len(m.todos) {
		return m, nil
	}
	todo := m.todos[index]
	var options []huh.Option[string]
	for _, state := range []struct{ status, label string }{
		{tools.TodoPending, "mark it pending"}, {tools.TodoInProgress, "mark it in progress"}, {tools.TodoCompleted, "mark it done"},
	} {
		if state.status != todo.Status {
			options = append(options, huh.NewOption(state.label, state.status))
		}
	}
	options = append(options, huh.NewOption("delete it", "delete"))
	return m.showChoices("Task", oneLine(todo.Text, max(m.width-4, 20)), options,
		func(m model, choice string) (tea.Model, tea.Cmd) {
			return m.editTask(index, choice)
		})
}

// editTask applies a change to a task, made by the user, and keeps a note
// with the whole list for the model.
func (m model) editTask(index int, choice string) (tea.Model, tea.Cmd) {
	if index >= len(m.todos) {
		return m, nil
	}
	todos := slices.Clone(m.todos)
	what := ""
	if choice == "delete" {
		what = "deleted the task " + fmt.Sprintf("%q", todos[index].Text)
		todos = slices.Delete(todos, index, index+1)
	} else {
		todos[index].Status = choice
		what = fmt.Sprintf("marked the task %q %s", todos[index].Text, choice)
	}
	m.todos = todos
	if m.session.Agent != nil {
		m.session.Agent.Tell("The user " + what + ". Your task list is now:\n" + todoList(todos) + "\nSend it whole with todo next time you change it.")
	}
	return m, nil
}

func todoList(todos []tools.Todo) string {
	if len(todos) == 0 {
		return "(empty)"
	}
	var lines []string
	for _, todo := range todos {
		lines = append(lines, fmt.Sprintf("- [%s] %s", todo.Status, todo.Text))
	}
	return strings.Join(lines, "\n")
}
