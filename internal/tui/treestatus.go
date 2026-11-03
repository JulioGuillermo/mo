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
	return gitMarks(m.repoRoot, m.changes)
}

// gitMarks are what git says of each file changed, by its full path, and a
// folder holding any of them marked as changed.
func gitMarks(root string, changes []gitops.Change) map[string]rune {
	if root == "" || len(changes) == 0 {
		return nil
	}
	marks := map[string]rune{}
	for _, change := range changes {
		path := filepath.Join(root, filepath.FromSlash(change.Path))
		marks[path] = change.Kind
		for dir := filepath.Dir(path); strings.HasPrefix(dir, root) && dir != root; dir = filepath.Dir(dir) {
			if _, marked := marks[dir]; marked {
				break
			}
			marks[dir] = gitops.ChangeModified
		}
	}
	return marks
}
