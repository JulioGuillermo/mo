package gitops

import (
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// LineBlame is who last changed a line of a file as it is on disk, and in
// which commit; Hash is empty for a line not committed yet.
type LineBlame struct {
	FileCommit
}

// Committed tells whether the line is as some commit left it.
func (b LineBlame) Committed() bool {
	return b.Hash != ""
}

// BlameFile is who last changed each line of the file on disk: git's blame
// of it in HEAD, and the lines changed since, not committed.
func (r *Repo) BlameFile(path string) ([]LineBlame, error) {
	disk := r.diskContent(path)
	head, _, err := r.head()
	if err != nil {
		return make([]LineBlame, len(lines(disk))), nil
	}
	committed := r.commitContent(head, path)
	var blamed []*git.Line
	if committed != "" {
		result, err := git.Blame(head, path)
		if err != nil {
			return nil, err
		}
		blamed = result.Lines
	}
	subjects := map[plumbing.Hash]string{}
	subjectOf := func(hash plumbing.Hash) string {
		if subject, ok := subjects[hash]; ok {
			return subject
		}
		var subject string
		if commit, err := r.repo.CommitObject(hash); err == nil {
			subject, _, _ = strings.Cut(commit.Message, "\n")
		}
		subjects[hash] = subject
		return subject
	}
	var out []LineBlame
	for _, edit := range Lines(committed, disk) {
		switch {
		case edit.Kind == '-':
			continue
		case edit.Kind == ' ' && edit.Old < len(blamed):
			line := blamed[edit.Old]
			out = append(out, LineBlame{FileCommit{Hash: line.Hash.String(), Author: line.AuthorName, When: line.Date, Subject: subjectOf(line.Hash)}})
		default:
			out = append(out, LineBlame{})
		}
	}
	return out, nil
}
