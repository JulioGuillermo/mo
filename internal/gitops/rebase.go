package gitops

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// rebasing is a rebase under way: the branch moved onto onto, where it was
// before, and the commits still to replay, oldest first.
type rebasing struct {
	branch string
	onto   plumbing.Hash
	before plumbing.Hash
	todo   []plumbing.Hash
	done   int
}

func (r *Repo) readRebase() (*rebasing, error) {
	state := &rebasing{}
	for _, line := range strings.Split(r.readState(rebaseFile), "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "branch":
			state.branch = value
		case "onto":
			state.onto = plumbing.NewHash(value)
		case "before":
			state.before = plumbing.NewHash(value)
		case "todo":
			state.todo = append(state.todo, plumbing.NewHash(value))
		case "done":
			_, _ = fmt.Sscan(value, &state.done)
		}
	}
	if state.branch == "" {
		return nil, errors.New("no rebase in progress")
	}
	return state, nil
}

func (r *Repo) writeRebase(state *rebasing) error {
	lines := []string{"branch " + state.branch, "onto " + state.onto.String(), "before " + state.before.String(), fmt.Sprintf("done %d", state.done)}
	for _, hash := range state.todo {
		lines = append(lines, "todo "+hash.String())
	}
	return os.WriteFile(r.stateFile(rebaseFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// Rebase replays the commits of the current branch that onto lacks on top
// of it, one by one, each with its author and message. A conflict stops it
// like a merge; continue goes on with the next commit, abort puts the
// branch back where it was.
func (r *Repo) Rebase(onto string) (string, error) {
	if err := r.requireClean(); err != nil {
		return "", err
	}
	ref, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	if !ref.Name().IsBranch() {
		return "", errors.New("rebase needs a branch checked out")
	}
	head, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return "", err
	}
	target, err := r.commitOf(onto)
	if err != nil {
		return "", err
	}
	bases, err := head.MergeBase(target)
	if err != nil || len(bases) == 0 {
		return "", fmt.Errorf("%s and %s share no history", ref.Name().Short(), onto)
	}
	base := bases[0]
	switch {
	case base.Hash == target.Hash:
		return "already up to date with " + onto, nil
	case base.Hash == head.Hash:
		return r.fastForward(target, onto)
	}
	var replay []plumbing.Hash
	for commit := head; commit.Hash != base.Hash; {
		if commit.NumParents() != 1 {
			return "", fmt.Errorf("commit %s is a merge: rebasing merges is not supported", commit.Hash.String()[:7])
		}
		replay = append(replay, commit.Hash)
		if commit, err = commit.Parent(0); err != nil {
			return "", err
		}
	}
	slices.Reverse(replay)
	state := &rebasing{branch: ref.Name().Short(), onto: target.Hash, before: head.Hash, todo: replay}
	if err := r.moveBranch(state.branch, target.Hash); err != nil {
		return "", err
	}
	if err := r.writeRebase(state); err != nil {
		return "", err
	}
	return r.replay(state)
}

func (r *Repo) moveBranch(branch string, to plumbing.Hash) error {
	if err := r.repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), to)); err != nil {
		return err
	}
	return r.hardReset(to)
}

// replay picks the commits left one by one, until all are on or one stops
// at conflicts.
func (r *Repo) replay(state *rebasing) (string, error) {
	var log []string
	for len(state.todo) > 0 {
		picked, err := r.repo.CommitObject(state.todo[0])
		if err != nil {
			return "", err
		}
		parent, err := picked.Parent(0)
		if err != nil {
			return "", err
		}
		head, branch, err := r.head()
		if err != nil {
			return "", err
		}
		op := &operation{kind: "rebase", theirs: picked.Hash, message: picked.Message}
		result, stopped, err := r.threeWay(op, parent, head, picked, branch, picked.Hash.String()[:7]+" "+subject(picked))
		if err != nil {
			return "", err
		}
		if stopped {
			return fmt.Sprintf("rebase stopped at %d of %d, %s %s\n%s", state.done+1, state.done+len(state.todo),
				picked.Hash.String()[:7], subject(picked), result), nil
		}
		log = append(log, result)
		state.todo, state.done = state.todo[1:], state.done+1
		if err := r.writeRebase(state); err != nil {
			return "", err
		}
	}
	r.clearState()
	return fmt.Sprintf("rebased %d commits onto %s\n%s", state.done, state.onto.String()[:7], strings.Join(log, "\n")), nil
}

func subject(commit *object.Commit) string {
	line, _, _ := strings.Cut(commit.Message, "\n")
	return line
}

// continueRebase commits the commit whose conflicts were resolved and
// replays the rest.
func (r *Repo) continueRebase(op *operation) (string, error) {
	state, err := r.readRebase()
	if err != nil {
		return "", err
	}
	if _, err := r.finish(op); err != nil {
		return "", err
	}
	for _, name := range []string{cherryPickHead, conflictsFile, writtenFile, mergeMessage} {
		_ = os.Remove(r.stateFile(name))
	}
	state.todo, state.done = state.todo[1:], state.done+1
	if err := r.writeRebase(state); err != nil {
		return "", err
	}
	return r.replay(state)
}

// abortRebase puts the branch back where it was before the rebase.
func (r *Repo) abortRebase(op *operation) (string, error) {
	state, err := r.readRebase()
	if err != nil {
		return "", err
	}
	head, _, err := r.head()
	if err != nil {
		return "", err
	}
	if err := r.moveBranch(state.branch, state.before); err != nil {
		return "", err
	}
	for _, path := range op.written {
		if _, err := head.File(path); err != nil {
			if commit, err := r.repo.CommitObject(state.before); err == nil {
				if _, err := commit.File(path); err != nil {
					_ = os.Remove(r.root + "/" + path)
				}
			}
		}
	}
	r.clearState()
	return fmt.Sprintf("rebase aborted; %s is back at %s", state.branch, state.before.String()[:7]), nil
}
