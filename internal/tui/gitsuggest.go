package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jg/mo/internal/gitops"
)

// gitSuggestions are what /git takes: its actions, then the repositories
// and folders show, branch and compare take, then the branches compare,
// switch, merge, rebase and cherry-pick do.
func (m model) gitSuggestions(partial string) []command {
	action, rest, spaced := strings.Cut(partial, " ")
	if !spaced {
		return gitActionSuggestions(action)
	}
	words := strings.Split(rest, " ")
	switch {
	case (action == "show" || action == "branch") && len(words) == 1:
		return m.repoSuggestions(action, words[0], false)
	case action == "compare" && len(words) == 1:
		branches := m.branchSuggestions(m.session.WorkDir, "git compare ", words[0])
		return append(branches, m.repoSuggestions(action, words[0], true)...)
	case action == "compare" && len(words) == 2:
		return m.branchSuggestions(m.absolutePath(words[0]), "git compare "+words[0]+" ", words[1])
	case takesBranch(action) && len(words) == 1:
		return m.branchSuggestions(m.session.WorkDir, "git "+action+" ", words[0])
	}
	return nil
}

func takesBranch(action string) bool {
	return action == "switch" || action == "merge" || action == "rebase" || action == "cherry-pick"
}

func gitActionSuggestions(typed string) []command {
	var matches []command
	for _, action := range gitActions {
		if !strings.HasPrefix(action.name, typed) {
			continue
		}
		description := action.description
		if action.usage != "" {
			description = action.usage + " · " + description
		}
		name := "git " + action.name
		if action.needsMore {
			name += " "
		}
		matches = append(matches, command{name: name, description: description, more: action.needsMore})
	}
	return matches
}

// branchSuggestions are the branches of the repository in dir that start
// with what was typed, but the current one.
func (m model) branchSuggestions(dir, before, typed string) []command {
	repo, err := gitops.Open(dir)
	if err != nil {
		return nil
	}
	var matches []command
	for _, name := range repo.BranchNames() {
		if strings.HasPrefix(name, typed) && name != repo.CurrentBranch() {
			matches = append(matches, command{name: before + name, description: "branch"})
		}
	}
	return matches
}

// repoSuggestions are the folders that start with what was typed: the
// repositories first, then the others, to go into. For compare a
// repository is followed by its branch, so it is not run at once.
func (m model) repoSuggestions(action, typed string, branchNext bool) []command {
	dir, prefix := filepath.Split(typed)
	entries, err := os.ReadDir(m.absolutePath(orDefault(dir, ".")))
	if err != nil {
		return nil
	}
	var repos, folders []command
	for _, entry := range entries {
		name := entry.Name()
		hidden := strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")
		if !entry.IsDir() || hidden || !strings.HasPrefix(name, prefix) {
			continue
		}
		path := dir + name
		if !gitops.IsRepo(m.absolutePath(path)) {
			folders = append(folders, command{name: "git " + action + " " + path + "/", description: "folder", more: true})
			continue
		}
		if branchNext {
			repos = append(repos, command{name: "git compare " + path + " ", description: "repository: its branch next", more: true})
			continue
		}
		repos = append(repos, command{name: "git " + action + " " + path, description: "repository"})
	}
	found := append(repos, folders...)
	return found[:min(len(found), maxPathSuggestions)]
}
