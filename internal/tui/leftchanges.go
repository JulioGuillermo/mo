package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/gitops"
)

// leftChangesMsg brings the changes read for the panel at the left, to
// show as mode says; again says they are those shown read anew, after the
// files changed.
type leftChangesMsg struct {
	changes *changeView
	dir     string
	base    string
	mode    string
	again   bool
	err     error
}

// loadLeftChanges reads the changes not committed, or those of the branch
// over base when one is named.
func loadLeftChanges(dir, base, mode string, again bool) tea.Cmd {
	return func() tea.Msg {
		failed := leftChangesMsg{dir: dir, base: base, mode: mode, again: again}
		repo, err := gitops.Open(dir)
		if err != nil {
			failed.err = err
			return failed
		}
		changes := &changeView{root: repo.Root()}
		if base == "" {
			changes.title, changes.uncommitted = "Changes not committed", true
			changes.comparison, err = repo.Uncommitted()
		} else {
			changes.title = base + " → " + orDefault(repo.CurrentBranch(), "HEAD")
			changes.comparison, err = repo.Compare(base)
		}
		if err != nil {
			failed.err = err
			return failed
		}
		changes.tree = newChangeTree(changes.root, changes.comparison.Files)
		changes.tree.label = changesLabel(base, len(changes.comparison.Files))
		return leftChangesMsg{changes: changes, dir: dir, base: base, mode: mode, again: again}
	}
}

// changesLabel is the title of a tree of changes: what it compares and how
// many files.
func changesLabel(base string, files int) string {
	what := "not committed"
	if base != "" {
		what = "over " + base
	}
	if files == 1 {
		return fmt.Sprintf("1 file changed %s", what)
	}
	return fmt.Sprintf("%d files changed %s", files, what)
}

// leftChangesRead shows the changes read at the left, alone or below the
// files as the mode says; read again, it keeps the cursor and the file
// shown.
func (m model) leftChangesRead(msg leftChangesMsg) (tea.Model, tea.Cmd) {
	if msg.again && (m.leftChanges == nil || m.leftChanges.root != msg.dir || m.leftChangesBase != msg.base) {
		return m, nil
	}
	if msg.err != nil {
		if msg.again {
			return m, nil
		}
		return m.emit(m.printer.Note("no changes to show: " + msg.err.Error()))
	}
	tree := msg.changes.tree
	m.labelChanges(msg.changes)
	if msg.again {
		shown := m.leftChanges.tree
		keepFolded(tree, shown)
		tree.current, tree.top = shown.current, shown.top
		if row, ok := shown.selected(); ok {
			tree.cursorTo(row.path, m.panelRows()-2)
		}
		m.leftChanges = msg.changes
		return m, nil
	}
	m.leftChanges, m.leftChangesBase, m.leftMode = msg.changes, msg.base, msg.mode
	m.leftTreeFocus, m.leftOnChanges = true, msg.mode == leftChanged
	if msg.mode == leftBoth && m.leftTree == nil {
		m.leftTree = newFileTree(m.session.WorkDir)
	}
	note := msg.changes.title + " at the left · enter shows the diff · /tree files goes back to the files"
	if msg.mode == leftBoth {
		note = "files above, " + strings.ToLower(msg.changes.title) + " below · tab goes from one to the other · /tree files leaves the files alone"
	}
	if !m.leftTreeShown() {
		note = fmt.Sprintf("changes at the left from %d columns", m.treeWidth()+1+mainMinWidth)
	}
	read := m.readRepo()
	return m, tea.Batch(m.fitWidth(), read, printText(m.printer.Note(note)))
}

// keepFolded closes in a tree of changes read anew the folders the one it
// replaces had closed.
func keepFolded(tree, shown *fileTree) {
	for folder := range tree.expanded {
		if shown.knows(folder) && !shown.expanded[folder] {
			delete(tree.expanded, folder)
		}
	}
	tree.rebuild()
}

// readLeftChangesAgain reads the changes at the left anew, when they show.
func (m model) readLeftChangesAgain() tea.Cmd {
	if m.leftChanges == nil {
		return nil
	}
	return loadLeftChanges(m.leftChanges.root, m.leftChangesBase, m.leftMode, true)
}

// openLeftChange shows the diff of a file of the changes at the left, the
// same tree beside it.
func (m model) openLeftChange(path string) (tea.Model, tea.Cmd) {
	tree := m.leftChanges.tree
	m.openChange(m.leftChanges, path)
	m.viewer.tree = tree
	m.leftTreeFocus = false
	tree.show(m.treeHeight())
	return m, nil
}

// treeChangesCommand is /tree changes and /tree both: [repo] [branch], as
// /git takes them. Out of a repository, those below are offered.
func (m model) treeChangesCommand(mode, rest string) (tea.Model, tea.Cmd) {
	request := parseGitRequest(mode+" "+rest, m.isRepoArgument)
	if len(request.words) > 1 {
		return m.emit(m.printer.Note("/tree " + mode + " takes a repository, then a branch or commit to compare with; nothing for the changes not committed here"))
	}
	base := strings.Join(request.words, "")
	dir := m.session.WorkDir
	if request.repo != "" {
		dir = m.absolutePath(request.repo)
	}
	if _, err := gitops.Open(dir); errors.Is(err, gitops.ErrNotRepository) {
		return m.chooseRepo(dir, "/tree "+mode, func(m model, repo string) (tea.Model, tea.Cmd) {
			return m, loadLeftChanges(repo, base, mode, false)
		})
	}
	return m, loadLeftChanges(dir, base, mode, false)
}

// treeChangesSuggestions are the repositories /tree changes and /tree both
// take, and the branches of the repository here.
func (m model) treeChangesSuggestions(mode, typed string) []command {
	before := "tree " + mode + " "
	repo, branch, spaced := strings.Cut(typed, " ")
	if spaced {
		return m.branchSuggestions(m.absolutePath(repo), before+repo+" ", branch, true)
	}
	branches := m.branchSuggestions(m.session.WorkDir, before, typed, true)
	return append(branches, m.repoSuggestions(before, typed, false)...)
}

// labelChanges names the repository over the title of its changes when it
// is not the one of the work folder.
func (m model) labelChanges(changes *changeView) {
	relative, err := filepath.Rel(changes.root, m.session.WorkDir)
	if err == nil && !strings.HasPrefix(relative, "..") {
		return
	}
	changes.tree.label = filepath.Base(changes.root) + " · " + changes.tree.label
}
