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

const (
	gitUsage = "usage: /git show [repo], the changes not committed · /git compare [repo] <branch>, what this branch adds over it, as a pull request · " +
		"/git branch [repo], the branches, to switch to one · /git repos, the repositories known"
	repoPickHelp = "↑↓ choose · / filter · enter take · esc back"
)

// gitRequest is what /git was asked: show, compare or branch, in the
// working directory's repository or in repo.
type gitRequest struct {
	action string
	repo   string
	branch string
}

// parseGitRequest reads /git show [repo], /git compare [repo] <branch> and
// /git branch [repo].
func parseGitRequest(argument string) (gitRequest, bool) {
	words := strings.Fields(argument)
	if len(words) == 0 {
		return gitRequest{}, false
	}
	request := gitRequest{action: words[0]}
	rest := words[1:]
	switch {
	case request.action == "compare" && len(rest) == 1:
		request.branch = rest[0]
	case request.action == "compare" && len(rest) == 2:
		request.repo, request.branch = rest[0], rest[1]
	case (request.action == "show" || request.action == "branch") && len(rest) == 1:
		request.repo = rest[0]
	case (request.action == "show" || request.action == "branch" || request.action == "repos") && len(rest) == 0:
	default:
		return request, false
	}
	return request, true
}

func (m model) gitCommand(argument string) (tea.Model, tea.Cmd) {
	request, ok := parseGitRequest(argument)
	if !ok {
		return m.emit(m.printer.Note(gitUsage))
	}
	if request.action == "repos" {
		return m.showRepos()
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
	if request.action == "branch" {
		return m.showBranches(repo)
	}
	return m.showComparison(repo, request)
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
