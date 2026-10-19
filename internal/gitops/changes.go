package gitops

import (
	"slices"

	"github.com/go-git/go-git/v5"
)

// Change is a file that differs from HEAD, and how: one of the Change
// kinds, a letter as git status gives it.
type Change struct {
	Path string
	Kind rune
}

// Change kinds.
const (
	ChangeAdded      = '+'
	ChangeModified   = '~'
	ChangeDeleted    = '-'
	ChangeRenamed    = '»'
	ChangeUntracked  = '?'
	ChangeConflicted = '!'
)

// Changes are the files that differ from HEAD, staged or not, sorted by
// path: each once, by what happened to it, a conflict over anything else.
func (r *Repo) Changes() ([]Change, error) {
	status, err := r.status()
	if err != nil {
		return nil, err
	}
	conflicted := map[string]bool{}
	if op := r.inProgress(); op != nil {
		for _, path := range op.conflicts {
			conflicted[path] = true
		}
	}
	var changes []Change
	for path, file := range status {
		kind := rune(0)
		switch {
		case conflicted[path] || file.Staging == git.UpdatedButUnmerged || file.Worktree == git.UpdatedButUnmerged:
			kind = ChangeConflicted
		case file.Worktree == git.Untracked:
			kind = ChangeUntracked
		case file.Staging == git.Renamed || file.Worktree == git.Renamed:
			kind = ChangeRenamed
		case file.Staging == git.Deleted || file.Worktree == git.Deleted:
			kind = ChangeDeleted
		case file.Staging == git.Added:
			kind = ChangeAdded
		case file.Staging != git.Unmodified || file.Worktree != git.Unmodified:
			kind = ChangeModified
		}
		if kind != 0 {
			changes = append(changes, Change{Path: path, Kind: kind})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	return changes, nil
}
