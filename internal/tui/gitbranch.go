package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/gitops"
)

const branchesHelp = "↑↓ choose · / filter · enter switch · esc back"

// showBranches is /git branch: the local branches, the current one first
// and marked, to filter and switch to one.
func (m model) showBranches(repo *gitops.Repo) (tea.Model, tea.Cmd) {
	current := repo.CurrentBranch()
	names := repo.BranchNames()
	if len(names) == 0 {
		return m.emit(m.printer.Note("no branches yet in " + m.shortDir(repo.Root())))
	}
	picked := current
	var options []huh.Option[string]
	if current != "" {
		options = append(options, huh.NewOption(current+"  ● current", current))
	}
	for _, name := range names {
		if name != current {
			options = append(options, huh.NewOption(name, name))
		}
	}
	list := huh.NewSelect[string]().Title("Branches · " + m.shortDir(repo.Root())).
		Description("enter switches to the one chosen").
		Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) { return m.switchBranch(repo, picked) }
	closed := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: closed, help: branchesHelp})
}

// switchBranch switches the repository to a branch, not while the model
// works, as it would change the files under it.
func (m model) switchBranch(repo *gitops.Repo, branch string) (tea.Model, tea.Cmd) {
	if branch == repo.CurrentBranch() {
		return m.emit(m.printer.Note("already on " + branch))
	}
	if m.running {
		return m.emit(m.printer.Note("the model is working: switch to " + branch + " once it ends"))
	}
	done, err := repo.Switch(branch, false)
	if err != nil {
		return m.emitError(err)
	}
	return m.emit(m.printer.Note(done + " in " + m.shortDir(repo.Root())))
}
