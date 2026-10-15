package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
)

const (
	everyProject  = "user"
	thisProject   = "project"
	rulesHelp     = "enter next · shift+enter new line · esc back to the question"
	ruleSyntax    = "one per line · * alone is any words or none, inside a word any characters · re: starts a regex"
	ruleSyntaxTip = "e.g. ls * · ls -la * /home · git log* · re:^npm (test|run lint)$ · fetch https://github.com/*"
)

type rulesSavedMsg struct {
	rules []string
	where string
	err   error
}

// editRules opens the commands of a call no rule allows as rules to edit:
// put a * where any words may go, then pick where they apply. Saving allows
// the call, and the other calls waiting that the rules now cover.
func (m model) editRules(index int) (tea.Model, tea.Cmd) {
	request := m.dialog.request(m.dialog.items[index])
	text := strings.Join(request.Commands, "\n")
	where := everyProject
	form := newForm(huh.NewGroup(
		huh.NewText().Title("Allow from now on").Description(ruleSyntax+"\n"+ruleSyntaxTip).
			Lines(max(len(request.Commands)+1, 3)).Validate(validRules).Value(&text),
		huh.NewSelect[string]().Title("Where").Options(
			huh.NewOption("every project", everyProject),
			huh.NewOption("only "+config.ProjectKey(m.session.WorkDir), thisProject),
		).Value(&where),
	).Title("Rules for " + request.Call.Name)).WithShowHelp(false)
	back := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	next := func(m model) (tea.Model, tea.Cmd) {
		rules := ruleLines(text)
		policy := m.session.Agent.Permissions
		for _, rule := range rules {
			_ = policy.Allow(rule)
		}
		save := saveRules(rules, where, m.session.WorkDir)
		if m.dialog == nil {
			return m, save
		}
		m.dialog.current = index
		m.allowByRules(policy)
		updated, cmd := m.decide(agent.Allow)
		return updated, tea.Batch(save, cmd)
	}
	return m.showSettings(&settingsScreen{form: form, layer: config.Config{}, next: next, back: back, help: rulesHelp})
}

// saveRules adds allow rules to the user file, for every project or for the
// working directory only.
func saveRules(rules []string, where, workDir string) tea.Cmd {
	return func() tea.Msg {
		layer, err := config.ReadUserLayer()
		if err != nil {
			return rulesSavedMsg{err: err}
		}
		if where == thisProject {
			key := config.ProjectKey(workDir)
			project := layer.Permissions.Projects[key]
			project.Allow = appendNew(project.Allow, rules)
			layer.Permissions.Projects = withEntry(layer.Permissions.Projects, key, project)
		} else {
			if layer.Permissions.Allow == nil {
				layer.Permissions.Allow = slices.Clone(config.DefaultAllow)
			}
			layer.Permissions.Allow = appendNew(layer.Permissions.Allow, rules)
		}
		return rulesSavedMsg{rules: rules, where: where, err: config.SaveUserLayer(layer)}
	}
}

func (m model) finishRules(msg rulesSavedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(fmt.Errorf("the rule works for this session but was not saved: %w", msg.err))
	}
	where := "every project"
	if msg.where == thisProject {
		where = "this project"
	}
	return m.emit(m.printer.Note(fmt.Sprintf("allowed from now on in %s: %s", where, strings.Join(msg.rules, " · "))))
}

func appendNew(list, added []string) []string {
	for _, item := range added {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

// ruleLines are the rules of a text box, one per line.
func ruleLines(text string) []string {
	var rules []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			rules = append(rules, line)
		}
	}
	return rules
}

func validRules(text string) error {
	var problems []error
	for _, rule := range ruleLines(text) {
		if err := permissions.Compile(rule); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
