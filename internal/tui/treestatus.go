package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/gitops"
)

const treeStatusDot = "●"

// treeStatusOn tells whether the trees of files mark what git says of
// each: what /tree status set for the session, else the setting, on unless
// it is false.
func (m model) treeStatusOn() bool {
	switch m.treeStatusSession {
	case sidebarOn:
		return true
	case sidebarOff:
		return false
	}
	setting := m.session.Config.Render.TreeStatus
	return setting == nil || *setting
}

// treeStatusCommand is /tree status: on or off for the session, alone the
// other one.
func (m model) treeStatusCommand(words []string) (tea.Model, tea.Cmd) {
	state := sidebarOn
	switch {
	case len(words) == 0 && m.treeStatusOn():
		state = sidebarOff
	case len(words) == 0:
	case len(words) == 1 && (words[0] == sidebarOn || words[0] == sidebarOff):
		state = words[0]
	default:
		return m.emit(m.printer.Note("/tree status takes on or off"))
	}
	m.treeStatusSession = state
	return m.emit(m.printer.Note("git marks in the file tree " + state + " · this session · File tree git marks in /settings keeps it"))
}

// gitMarksFor are the marks a tree shows beside its rows: none for a tree
// of changes, which colours its files itself, or with the marks off.
func (m model) gitMarksFor(tree *fileTree) map[string]rune {
	if tree == nil || tree.kinds != nil || !m.treeStatusOn() {
		return nil
	}
	return m.treeMarks
}

// anyTreeShown tells whether a tree of files is on the screen: at the
// left, or in the viewer.
func (m model) anyTreeShown() bool {
	return m.leftMode != "" || m.viewer != nil && m.viewer.tree != nil
}

// gitMarks are what git says of each file changed of the repository at
// root, by its full path, and each folder holding any of them, up to top,
// marked as changed.
func gitMarks(top, root string, changes []gitops.Change) map[string]rune {
	marks := map[string]rune{}
	addGitMarks(marks, top, root, changes)
	return marks
}

func addGitMarks(marks map[string]rune, top, root string, changes []gitops.Change) {
	for _, change := range changes {
		path := filepath.Join(root, filepath.FromSlash(change.Path))
		marks[path] = change.Kind
		for dir := filepath.Dir(path); dir != top && strings.HasPrefix(dir, top); dir = filepath.Dir(dir) {
			if _, marked := marks[dir]; marked {
				break
			}
			marks[dir] = gitops.ChangeModified
		}
	}
}

// gitMarksBelow are the marks of every repository below a folder that is
// in none, as /git finds them, their folders up to it marked too.
func gitMarksBelow(dir string) map[string]rune {
	repos, _ := gitops.ReposBelow(dir)
	marks := map[string]rune{}
	for _, relative := range repos {
		repo, err := gitops.Open(filepath.Join(dir, relative))
		if err != nil {
			continue
		}
		changes, _ := repo.Changes()
		addGitMarks(marks, dir, repo.Root(), changes)
	}
	return marks
}
