package gitops

import (
	"fmt"
	"regexp"

	"github.com/go-git/go-git/v5/plumbing"
)

var commitHash = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

// BranchRevision is the revision of a local branch, else of a remote one
// as origin/main; by its full name when a tag has the same.
func (r *Repo) BranchRevision(name string) (string, error) {
	local := plumbing.NewBranchReferenceName(name)
	if r.hasReference(local) {
		return r.unambiguous(name, local), nil
	}
	remote := plumbing.ReferenceName("refs/remotes/" + name)
	if r.hasReference(remote) {
		return r.unambiguous(name, remote), nil
	}
	return "", fmt.Errorf("no branch %q", name)
}

// TagRevision is the revision of a tag, by its full name when a branch has
// the same.
func (r *Repo) TagRevision(name string) (string, error) {
	tag := plumbing.NewTagReferenceName(name)
	if !r.hasReference(tag) {
		return "", fmt.Errorf("no tag %q", name)
	}
	return r.unambiguous(name, tag), nil
}

// CommitRevision checks that hash, whole or its start, is a commit here.
func (r *Repo) CommitRevision(hash string) (string, error) {
	if !commitHash.MatchString(hash) {
		return "", fmt.Errorf("%q is not a commit hash", hash)
	}
	if _, err := r.commitOf(hash); err != nil {
		return "", fmt.Errorf("no commit %s", hash)
	}
	return hash, nil
}

// Hash is the whole hash of the commit a revision names; "" when none.
func (r *Repo) Hash(revision string) string {
	commit, err := r.commitOf(revision)
	if err != nil {
		return ""
	}
	return commit.Hash.String()
}

func (r *Repo) hasReference(name plumbing.ReferenceName) bool {
	_, err := r.repo.Reference(name, false)
	return err == nil
}

// unambiguous is the short name when only ref goes by it, else the full one.
func (r *Repo) unambiguous(short string, ref plumbing.ReferenceName) string {
	others := []plumbing.ReferenceName{
		plumbing.NewBranchReferenceName(short),
		plumbing.NewTagReferenceName(short),
		plumbing.ReferenceName("refs/remotes/" + short),
	}
	for _, other := range others {
		if other != ref && r.hasReference(other) {
			return ref.String()
		}
	}
	return short
}
