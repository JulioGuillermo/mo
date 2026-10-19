package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/gitops"
)

const repoPickHelp = "↑↓ choose · / filter · enter take · esc back"

// gitRequest is what /git was asked: an action, in the working directory's
// repository or in repo, with the words after them and the same text
// as typed, for a message.
type gitRequest struct {
	action string
	repo   string
	words  []string
	text   string
}

// parseGitRequest reads /git <action> [repo] [arguments]. The word after
// the action is the repository when isRepo says so; compare with two
// words takes the first as the repository whatever it is, and init takes
// its folder there.
func parseGitRequest(argument string, isRepo func(word string) bool) gitRequest {
	action, rest := cutWord(argument)
	request := gitRequest{action: action}
	first, afterFirst := cutWord(rest)
	repoFirst := first != "" && (isRepo(first) || action == "init" || (action == "compare" && len(strings.Fields(rest)) == 2))
	if repoFirst {
		request.repo, rest = first, afterFirst
	}
	request.text = rest
	request.words = strings.Fields(rest)
	return request
}

// cutWord splits off the first word of text, the rest trimmed.
func cutWord(text string) (word, rest string) {
	text = strings.TrimSpace(text)
	word, rest, _ = strings.Cut(text, " ")
	return word, strings.TrimSpace(rest)
}

// isRepoArgument tells whether a word after a /git action names a
// repository: a folder that is one, or any folder outside the working
// directory's repository, or any folder when the working directory is in
// none, to look for those below it.
func (m model) isRepoArgument(word string) bool {
	dir := m.absolutePath(word)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return false
	}
	if gitops.IsRepo(dir) {
		return true
	}
	here, err := gitops.Open(m.session.WorkDir)
	if err != nil {
		return true
	}
	relative, err := filepath.Rel(here.Root(), dir)
	return err != nil || strings.HasPrefix(relative, "..")
}

func (m model) gitCommand(argument string) (tea.Model, tea.Cmd) {
	request := parseGitRequest(argument, m.isRepoArgument)
	switch request.action {
	case "", "help":
		return m.emit(m.printer.Note(gitUsage()))
	case "repos":
		return m.showRepos()
	case "init":
		return m.runGitAction(request, "")
	}
	return m.runGitRequest(request)
}

// runGitRequest opens the repository and does what was asked; outside
// one, it offers those below.
func (m model) runGitRequest(request gitRequest) (tea.Model, tea.Cmd) {
	dir := m.session.WorkDir
	if request.repo != "" {
		dir = m.absolutePath(request.repo)
	}
	repo, err := gitops.Open(dir)
	if errors.Is(err, gitops.ErrNotRepository) {
		return m.pickRepo(request, dir)
	}
	if err != nil {
		return m.emitError(err)
	}
	switch {
	case request.action == "branch" && len(request.words) == 0:
		return m.showBranches(repo)
	case request.action == "show" || request.action == "compare":
		return m.showComparison(repo, request)
	}
	return m.runGitAction(request, repo.Root())
}

// showRepos is /git repos: the repositories known, each with its branch;
// enter lists the branches of the one chosen.
func (m model) showRepos() (tea.Model, tea.Cmd) {
	var options []huh.Option[string]
	for _, root := range gitops.KnownRepos(m.session.WorkDir) {
		repo, err := gitops.Open(root)
		if err != nil {
			continue
		}
		label := fmt.Sprintf("%s  %s · %s", m.shortDir(root), orDefault(repo.CurrentBranch(), "no branch"), countBranches(len(repo.BranchNames())))
		options = append(options, huh.NewOption(label, root))
	}
	if len(options) == 0 {
		return m.emit(m.printer.Note(fmt.Sprintf("no repositories in %s nor in the %d levels below it; /git show <repo> opens a deeper one",
			m.shortDir(m.session.WorkDir), gitops.SearchDepth)))
	}
	var picked string
	description := fmt.Sprintf("those in the %d levels below and every one opened; enter lists its branches", gitops.SearchDepth)
	list := huh.NewSelect[string]().Title("Repositories").Description(description).
		Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) { return m.runGitRequest(gitRequest{action: "branch", repo: picked}) }
	closed := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: closed, help: repoPickHelp})
}

func countBranches(count int) string {
	if count == 1 {
		return "1 branch"
	}
	return fmt.Sprintf("%d branches", count)
}

// pickRepo lists the repositories below dir, which is not one, to take
// one for the request; the only one found is taken at once.
func (m model) pickRepo(request gitRequest, dir string) (tea.Model, tea.Cmd) {
	repos, more := gitops.ReposBelow(dir)
	switch len(repos) {
	case 0:
		return m.emit(m.printer.Note(fmt.Sprintf("not a git repository: %s, nor is any in the %d levels below it; /git %s <repo> takes a deeper one",
			m.shortDir(dir), gitops.SearchDepth, request.action)))
	case 1:
		request.repo = filepath.Join(dir, repos[0])
		return m.runGitRequest(request)
	}
	description := fmt.Sprintf("%s is not a repository; these are in the %d levels below it, deeper ones are not listed", m.shortDir(dir), gitops.SearchDepth)
	if more {
		description = fmt.Sprintf("%s is not a repository; the first %d in the %d levels below it", m.shortDir(dir), len(repos), gitops.SearchDepth)
	}
	picked := repos[0]
	var options []huh.Option[string]
	for _, repo := range repos {
		options = append(options, huh.NewOption(repo, repo))
	}
	list := huh.NewSelect[string]().Title("Repository · /git " + request.action).Description(description).
		Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) {
		request.repo = filepath.Join(dir, picked)
		return m.runGitRequest(request)
	}
	closed := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: closed, help: repoPickHelp})
}

// absolutePath is a path the user typed: relative to the working
// directory, ~ for the home folder.
func (m model) absolutePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(m.session.WorkDir, path)
}
