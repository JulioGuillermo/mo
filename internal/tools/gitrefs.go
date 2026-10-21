package tools

import (
	"fmt"
	"strings"

	"github.com/jg/mo/internal/gitops"
)

// gitRefs are the ways a git call names what it works on: revision is any
// of them as git takes it; branch, tag and commit say which it is.
type gitRefs struct {
	Revision, Name, Branch, Tag, Commit string
}

// target is the revision an action works on, from whichever field names
// it; name counts too where the action asks for no other name. Branch, tag
// and commit fail when they are not one.
func (refs gitRefs) target(repo *gitops.Repo, nameCounts bool) (string, error) {
	var given []string
	var revision string
	var err error
	if refs.Revision != "" {
		given, revision = append(given, "revision"), refs.Revision
	}
	if nameCounts && refs.Name != "" {
		given, revision = append(given, "name"), refs.Name
	}
	if refs.Branch != "" {
		given = append(given, "branch")
		revision, err = repo.BranchRevision(refs.Branch)
	}
	if refs.Tag != "" && nameCounts {
		given = append(given, "tag")
		revision, err = repo.TagRevision(refs.Tag)
	}
	if refs.Commit != "" {
		given = append(given, "commit")
		revision, err = repo.CommitRevision(refs.Commit)
	}
	if len(given) > 1 {
		return "", fmt.Errorf("give only one of %s", strings.Join(given, ", "))
	}
	return revision, err
}

// branchName is the branch a switch or a pull names, as name or branch.
func (refs gitRefs) branchName() (string, error) {
	return oneName("name", refs.Name, "branch", refs.Branch)
}

// tagName is the tag a tag action makes, as name or tag.
func (refs gitRefs) tagName() (string, error) {
	return oneName("name", refs.Name, "tag", refs.Tag)
}

func oneName(field, value, otherField, other string) (string, error) {
	if value != "" && other != "" && value != other {
		return "", fmt.Errorf("give only one of %s, %s", field, otherField)
	}
	return firstNonEmpty(value, other), nil
}
