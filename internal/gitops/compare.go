package gitops

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// Comparison is what differs between two states of the repository: the
// files, each by what happened to it, and what each side holds of them.
type Comparison struct {
	Files  []Change
	before func(path string) string
	after  func(path string) string
}

// Contents are what a file holds before and after; empty on the side it
// does not exist.
func (c Comparison) Contents(path string) (before, after string) {
	return c.before(path), c.after(path)
}

// Uncommitted compares HEAD with the files on disk, staged or not.
func (r *Repo) Uncommitted() (Comparison, error) {
	files, err := r.Changes()
	if err != nil {
		return Comparison{}, err
	}
	head, _, _ := r.head()
	before := func(path string) string { return r.commitContent(head, path) }
	return Comparison{Files: files, before: before, after: r.diskContent}, nil
}

// Compare is what HEAD brings over revision, as a pull request shows it:
// the changes from where the two parted to HEAD, both as committed.
func (r *Repo) Compare(revision string) (Comparison, error) {
	other, err := r.commitOf(revision)
	if err != nil {
		return Comparison{}, err
	}
	head, _, err := r.head()
	if err != nil {
		return Comparison{}, err
	}
	bases, err := head.MergeBase(other)
	if err != nil {
		return Comparison{}, err
	}
	if len(bases) == 0 {
		return Comparison{}, fmt.Errorf("%s and this branch share no history", revision)
	}
	comparison, err := r.between(bases[0], head)
	if err != nil {
		return Comparison{}, fmt.Errorf("comparing with %s: %w", revision, err)
	}
	return comparison, nil
}

// CommitChanges is what a commit changed over its first parent, or all
// its files for the first commit.
func (r *Repo) CommitChanges(revision string) (Comparison, error) {
	commit, err := r.commitOf(revision)
	if err != nil {
		return Comparison{}, err
	}
	var parent *object.Commit
	if commit.NumParents() > 0 {
		if parent, err = commit.Parent(0); err != nil {
			return Comparison{}, err
		}
	}
	return r.between(parent, commit)
}

// between compares two commits; a nil from is a commit with no files.
func (r *Repo) between(from, to *object.Commit) (Comparison, error) {
	fromTree := &object.Tree{}
	if from != nil {
		tree, err := from.Tree()
		if err != nil {
			return Comparison{}, err
		}
		fromTree = tree
	}
	toTree, err := to.Tree()
	if err != nil {
		return Comparison{}, err
	}
	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return Comparison{}, err
	}
	var files []Change
	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return Comparison{}, err
		}
		switch action {
		case merkletrie.Insert:
			files = append(files, Change{Path: change.To.Name, Kind: ChangeAdded})
		case merkletrie.Delete:
			files = append(files, Change{Path: change.From.Name, Kind: ChangeDeleted})
		default:
			files = append(files, Change{Path: change.To.Name, Kind: ChangeModified})
		}
	}
	slices.SortFunc(files, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	before := func(path string) string { return r.commitContent(from, path) }
	after := func(path string) string { return r.commitContent(to, path) }
	return Comparison{Files: files, before: before, after: after}, nil
}

// BranchNames are the local branches, sorted.
func (r *Repo) BranchNames() []string {
	branches, err := r.repo.Branches()
	if err != nil {
		return nil
	}
	var names []string
	_ = branches.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	})
	slices.Sort(names)
	return names
}

// BranchesLine is the current branch, then the others, up to
// shownBranches of them: "main · also dev, feature".
func (r *Repo) BranchesLine() string {
	current := r.CurrentBranch()
	line := current
	if current == "" {
		line = "no branch"
	}
	var others []string
	for _, name := range r.BranchNames() {
		if name != current {
			others = append(others, name)
		}
	}
	if len(others) == 0 {
		return line
	}
	if len(others) > shownBranches {
		return fmt.Sprintf("%s · also %s and %d more", line, strings.Join(others[:shownBranches], ", "), len(others)-shownBranches)
	}
	return line + " · also " + strings.Join(others, ", ")
}
