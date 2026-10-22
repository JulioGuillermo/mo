package gitops

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// FileCommit is a commit that changed a file.
type FileCommit struct {
	Hash    string
	Author  string
	When    time.Time
	Subject string
}

// Short is the commit's hash as git shortens it.
func (c FileCommit) Short() string {
	return c.Hash[:min(len(c.Hash), 7)]
}

// FileHistory are the commits before HEAD that changed path, the newest
// first: those where it differs from every parent, so a merge that took
// one side's version is left out. A rename is not followed.
func (r *Repo) FileHistory(path string) ([]FileCommit, error) {
	head, _, err := r.head()
	if err != nil {
		return nil, err
	}
	commits, err := r.repo.Log(&git.LogOptions{From: head.Hash, Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	versions := map[plumbing.Hash]plumbing.Hash{}
	version := func(commit *object.Commit) plumbing.Hash {
		if known, ok := versions[commit.Hash]; ok {
			return known
		}
		var blob plumbing.Hash
		if tree, err := commit.Tree(); err == nil {
			if entry, err := tree.FindEntry(path); err == nil {
				blob = entry.Hash
			}
		}
		versions[commit.Hash] = blob
		return blob
	}
	var history []FileCommit
	err = commits.ForEach(func(commit *object.Commit) error {
		mine := version(commit)
		same := commit.NumParents() == 0 && mine.IsZero()
		_ = commit.Parents().ForEach(func(parent *object.Commit) error {
			if version(parent) == mine {
				same = true
				return io.EOF
			}
			return nil
		})
		if !same {
			subject, _, _ := strings.Cut(commit.Message, "\n")
			history = append(history, FileCommit{Hash: commit.Hash.String(), Author: commit.Author.Name, When: commit.Author.When, Subject: subject})
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return history, nil
}

// FileChange is what a commit did to one file, from its first parent.
func (r *Repo) FileChange(hash, path string) (Comparison, error) {
	commit, err := r.commitOf(hash)
	if err != nil {
		return Comparison{}, err
	}
	var parent *object.Commit
	if commit.NumParents() > 0 {
		if parent, err = commit.Parent(0); err != nil {
			return Comparison{}, err
		}
	}
	before, after := r.commitContent(parent, path), r.commitContent(commit, path)
	return oneFile(path, before, after), nil
}

// UncommittedFile is how the file on disk differs from HEAD; ok is false
// when it does not.
func (r *Repo) UncommittedFile(path string) (Comparison, bool) {
	head, _, _ := r.head()
	before, after := r.commitContent(head, path), r.diskContent(path)
	if before == after {
		return Comparison{}, false
	}
	return oneFile(path, before, after), true
}

func oneFile(path, before, after string) Comparison {
	kind := ChangeModified
	switch {
	case before == "":
		kind = ChangeAdded
	case after == "":
		kind = ChangeDeleted
	}
	return Comparison{
		Files:  []Change{{Path: path, Kind: kind}},
		before: func(string) string { return before },
		after:  func(string) string { return after },
	}
}
