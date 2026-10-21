package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GR-01 | Happy    | A branch, local or remote, and a tag are taken by their short names  | P0       |
// | GR-02 | Edge     | A branch and a tag with the same name each get their full name       | P0       |
// | GR-03 | Error    | What is not a branch, tag or commit is refused, naming it            | P1       |

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestRevisions(t *testing.T) {
	t.Run("GR-01 a branch, local or remote, and a tag are taken by their short names", func(t *testing.T) {
		tr := newRepo(t)
		head, _ := tr.repo.Head()
		tr.repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/origin/main", head.Hash()))
		tr.repo.CreateTag("v1", head.Hash(), nil)

		local, localErr := tr.r.BranchRevision("master")
		remote, remoteErr := tr.r.BranchRevision("origin/main")
		tag, tagErr := tr.r.TagRevision("v1")
		commit, commitErr := tr.r.CommitRevision(head.Hash().String()[:7])

		if local != "master" || remote != "origin/main" || tag != "v1" || commit != head.Hash().String()[:7] {
			t.Errorf("local %q remote %q tag %q commit %q", local, remote, tag, commit)
		}
		if localErr != nil || remoteErr != nil || tagErr != nil || commitErr != nil {
			t.Errorf("errors: %v %v %v %v", localErr, remoteErr, tagErr, commitErr)
		}
	})

	t.Run("GR-02 a branch and a tag with the same name each get their full name", func(t *testing.T) {
		tr := newRepo(t)
		head, _ := tr.repo.Head()
		tr.repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("dup"), head.Hash()))
		tr.repo.CreateTag("dup", head.Hash(), nil)

		branch, _ := tr.r.BranchRevision("dup")
		tag, _ := tr.r.TagRevision("dup")

		if branch != "refs/heads/dup" || tag != "refs/tags/dup" {
			t.Errorf("branch %q tag %q", branch, tag)
		}
	})

	t.Run("GR-03 what is not a branch, tag or commit is refused, naming it", func(t *testing.T) {
		tr := newRepo(t)

		_, branchErr := tr.r.BranchRevision("nope")
		_, tagErr := tr.r.TagRevision("master")
		_, hashErr := tr.r.CommitRevision("master")
		_, commitErr := tr.r.CommitRevision("abcdef1")

		cases := []struct {
			err  error
			want string
		}{{branchErr, `no branch "nope"`}, {tagErr, `no tag "master"`}, {hashErr, "not a commit hash"}, {commitErr, "no commit abcdef1"}}
		for _, each := range cases {
			if each.err == nil || !strings.Contains(each.err.Error(), each.want) {
				t.Errorf("err = %v, want %q", each.err, each.want)
			}
		}
	})
}
