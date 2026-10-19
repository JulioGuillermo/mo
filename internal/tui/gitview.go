package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/gitops"
)

const (
	changeHelp = "↑↓ move · e E on ⋯ show lines · f whole file · c comment · a attach · / search · y copy · t tree · esc close"
)

var (
	changeAdded    = lipgloss.Color("#9ece6a")
	changeDeleted  = lipgloss.Color("#f7768e")
	changeModified = warning
)

// changeView is a comparison shown in the viewer: its files in the tree,
// the diff of the one picked beside it.
type changeView struct {
	title      string
	root       string
	comparison gitops.Comparison
	tree       *fileTree
}

// changeStyle is the colour of a file by what happened to it: new in
// green, changed in amber, gone in red; bold for the one shown.
func changeStyle(kind rune, current bool) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(changeModified)
	switch kind {
	case gitops.ChangeAdded, gitops.ChangeUntracked:
		style = style.Foreground(changeAdded)
	case gitops.ChangeDeleted:
		style = style.Foreground(changeDeleted)
	}
	return style.Bold(current)
}

// showComparison is /git show, of the changes not committed or of a
// commit, and /git compare, in the viewer: the files in a tree, the diff of
// the first beside it.
func (m model) showComparison(repo *gitops.Repo, request gitRequest) (tea.Model, tea.Cmd) {
	var comparison gitops.Comparison
	var err error
	var title, nothing string
	switch {
	case request.action == "show" && len(request.words) == 0:
		comparison, err = repo.Uncommitted()
		title, nothing = "Changes not committed", "nothing changed since the last commit"
	case request.action == "show" && len(request.words) == 1:
		revision := request.words[0]
		comparison, err = repo.CommitChanges(revision)
		title, nothing = "Commit "+revision, revision+" changes no file"
	case request.action == "compare" && len(request.words) == 1:
		branch := request.words[0]
		comparison, err = repo.Compare(branch)
		title = branch + " → " + orDefault(repo.CurrentBranch(), "HEAD")
		nothing = "this branch adds nothing over " + branch
	default:
		return m.emit(m.printer.Note(gitUsage()))
	}
	if err != nil {
		return m.emitError(err)
	}
	if len(comparison.Files) == 0 {
		return m.emit(m.printer.Note(nothing))
	}
	changes := &changeView{title: title, root: repo.Root(), comparison: comparison, tree: newChangeTree(repo.Root(), comparison.Files)}
	first := filepath.Join(changes.root, comparison.Files[0].Path)
	m.openChange(changes, first)
	changes.tree.reveal(first, m.treeHeight())
	m.viewer.tree, m.viewer.treeFocus = changes.tree, true
	if m.width < treeNarrow {
		m.viewer.tree, m.viewer.treeFocus = nil, false
	}
	return m, nil
}

// newChangeTree lists only the files of a comparison, every folder open.
func newChangeTree(root string, files []gitops.Change) *fileTree {
	tree := &fileTree{root: filepath.Clean(root), expanded: map[string]bool{}, kinds: map[string]rune{}}
	folders := map[string][]string{}
	names := map[string][]string{}
	for _, file := range files {
		full := filepath.Join(tree.root, file.Path)
		tree.kinds[full] = file.Kind
		tree.indexed = append(tree.indexed, file.Path)
		names[filepath.Dir(full)] = append(names[filepath.Dir(full)], filepath.Base(full))
		for dir := filepath.Dir(full); dir != tree.root && !tree.expanded[dir]; dir = filepath.Dir(dir) {
			tree.expanded[dir] = true
			parent := filepath.Dir(dir)
			folders[parent] = append(folders[parent], filepath.Base(dir))
		}
	}
	tree.listing = func(dir string) ([]string, []string) {
		return sortedNames(folders[dir]), sortedNames(names[dir])
	}
	tree.rebuild()
	return tree
}

// sortedNames are names in the order the tree lists them, capitals not
// mattering.
func sortedNames(names []string) []string {
	sorted := slices.Clone(names)
	slices.SortFunc(sorted, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return sorted
}

// openChange shows the diff of one file of a comparison in the viewer.
func (m *model) openChange(changes *changeView, path string) {
	relative, _ := filepath.Rel(changes.root, path)
	before, after := changes.comparison.Contents(filepath.ToSlash(relative))
	v := &outputViewer{changes: changes, diffFile: relative, pickFrom: -1}
	if isBinary(before) || isBinary(after) {
		v.lines = []string{"a binary file: its changes are not shown"}
	} else {
		v.diff = newDiffText(before, after, languageOf(path))
		v.lines = []string{"no changes in the text"}
		if v.diff != nil {
			v.lines = v.diff.lines()
		}
	}
	changes.tree.current = path
	if m.fileComments == nil {
		m.fileComments = map[string][]fileComment{}
	}
	m.viewer = v
}

func isBinary(text string) bool {
	return strings.IndexByte(text[:min(len(text), 8000)], 0) >= 0
}

// changeHeader is what the viewer says over a file of a comparison.
func (v *outputViewer) changeHeader(width int, comments string) string {
	about := fmt.Sprintf("%s · %d files", v.changes.title, len(v.changes.comparison.Files))
	if len(v.changes.comparison.Files) == 1 {
		about = v.changes.title + " · 1 file"
	}
	if v.diff != nil && v.diff.whole {
		about += " · whole file"
	}
	about += comments
	return toolName.Render(oneLine(v.diffFile, width/2)) + " " + dimStyle.Render(about+" · only you see it")
}
