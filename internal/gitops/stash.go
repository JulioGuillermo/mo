package gitops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const stashRef = "refs/stash"

var zeroHash = plumbing.ZeroHash.String()

// stashEntry is one stash: its commit and the message it was saved with.
type stashEntry struct {
	hash    plumbing.Hash
	message string
}

func (r *Repo) stashLog() string {
	return filepath.Join(r.gitDir, "logs", stashRef)
}

// stashes reads the stash list from its log, as git keeps it: one line per
// stash, oldest first. The newest is stash 0.
func (r *Repo) stashes() []stashEntry {
	data, err := os.ReadFile(r.stashLog())
	if err != nil {
		return nil
	}
	var entries []stashEntry
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		fields, message, _ := strings.Cut(line, "\t")
		parts := strings.Fields(fields)
		if len(parts) < 2 {
			continue
		}
		entries = append([]stashEntry{{hash: plumbing.NewHash(parts[1]), message: message}}, entries...)
	}
	return entries
}

// writeStashes writes the list back, newest first in, and points refs/stash
// at the newest, or removes it when none is left.
func (r *Repo) writeStashes(entries []stashEntry) error {
	signature := r.signature()
	var lines []string
	previous := zeroHash
	for i := len(entries) - 1; i >= 0; i-- {
		lines = append(lines, fmt.Sprintf("%s %s %s <%s> %d %s\t%s", previous, entries[i].hash, signature.Name, signature.Email,
			signature.When.Unix(), signature.When.Format("-0700"), entries[i].message))
		previous = entries[i].hash.String()
	}
	name := plumbing.ReferenceName(stashRef)
	if len(entries) == 0 {
		_ = os.Remove(r.stashLog())
		return r.repo.Storer.RemoveReference(name)
	}
	if err := os.MkdirAll(filepath.Dir(r.stashLog()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(r.stashLog(), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return r.repo.Storer.SetReference(plumbing.NewHashReference(name, entries[0].hash))
}

// trackedChanges lists the tracked files changed or staged.
func (r *Repo) trackedChanges() (staged, changed []string, err error) {
	status, err := r.tree.Status()
	if err != nil {
		return nil, nil, err
	}
	for path, file := range status {
		if file.Worktree == git.Untracked {
			continue
		}
		if file.Staging != git.Unmodified {
			staged = append(staged, path)
		}
		if file.Worktree != git.Unmodified {
			changed = append(changed, path)
		}
	}
	return staged, changed, nil
}

// Stash saves the changes to tracked files, staged or not, and puts the
// working tree back to HEAD. It is stored as git stores it, a commit of the
// index and one of the working tree on top of HEAD, so git sees it too.
func (r *Repo) Stash(message string) (string, error) {
	if r.inProgress() != nil {
		return "", errors.New("finish or abort what is in progress first")
	}
	staged, changed, err := r.trackedChanges()
	if err != nil {
		return "", err
	}
	if len(staged)+len(changed) == 0 {
		return "no changes to tracked files to stash", nil
	}
	ref, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	head, branch, err := r.head()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("%s %s", head.Hash.String()[:7], subject(head))
	}
	signature := r.signature()
	index, err := r.tree.Commit("index on "+branch+": "+message, &git.CommitOptions{
		Author: signature, Parents: []plumbing.Hash{head.Hash}, AllowEmptyCommits: true,
	})
	if err != nil {
		return "", err
	}
	for _, path := range changed {
		if _, err := os.Stat(filepath.Join(r.root, path)); err == nil {
			_, err = r.tree.Add(path)
		} else {
			_, err = r.tree.Remove(path)
		}
		if err != nil {
			return "", r.restoreHead(ref, err)
		}
	}
	saved, err := r.tree.Commit("WIP on "+branch+": "+message, &git.CommitOptions{
		Author: signature, Parents: []plumbing.Hash{head.Hash, index}, AllowEmptyCommits: true,
	})
	if err != nil {
		return "", r.restoreHead(ref, err)
	}
	if err := r.restoreHead(ref, nil); err != nil {
		return "", err
	}
	if err := r.hardReset(head.Hash); err != nil {
		return "", err
	}
	entries := append([]stashEntry{{hash: saved, message: "WIP on " + branch + ": " + message}}, r.stashes()...)
	if err := r.writeStashes(entries); err != nil {
		return "", err
	}
	return fmt.Sprintf("stashed %d files as stash 0; the working tree is back at %s", len(unique(append(staged, changed...))), head.Hash.String()[:7]), nil
}

// restoreHead puts HEAD back where it was: committing the stash moved it.
func (r *Repo) restoreHead(ref *plumbing.Reference, cause error) error {
	if err := r.repo.Storer.SetReference(ref); err != nil {
		return err
	}
	return cause
}

func unique(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// Stashes lists the stashes, the newest first.
func (r *Repo) Stashes() (string, error) {
	entries := r.stashes()
	if len(entries) == 0 {
		return "no stashes", nil
	}
	var lines []string
	for i, entry := range entries {
		lines = append(lines, fmt.Sprintf("stash %d: %s", i, entry.message))
	}
	return strings.Join(lines, "\n"), nil
}

func (r *Repo) stashAt(index int) (stashEntry, error) {
	entries := r.stashes()
	if index < 0 || index >= len(entries) {
		return stashEntry{}, fmt.Errorf("no stash %d; stash_list shows them", index)
	}
	return entries[index], nil
}

// ApplyStash merges a stash's changes into the working tree, unstaged.
// Conflicts are left as markers; pop keeps the stash when there are any.
func (r *Repo) ApplyStash(index int, drop bool) (string, error) {
	if err := r.requireClean(); err != nil {
		return "", err
	}
	entry, err := r.stashAt(index)
	if err != nil {
		return "", err
	}
	saved, err := r.repo.CommitObject(entry.hash)
	if err != nil {
		return "", err
	}
	base, err := saved.Parent(0)
	if err != nil {
		return "", err
	}
	head, branch, err := r.head()
	if err != nil {
		return "", err
	}
	merged, err := r.mergeTrees(base, head, saved, branch, fmt.Sprintf("stash %d", index), false)
	if err != nil {
		return "", err
	}
	if err := r.tree.Reset(&git.ResetOptions{Commit: head.Hash, Mode: git.MixedReset}); err != nil {
		return "", err
	}
	if len(merged.conflicts) > 0 {
		var out strings.Builder
		fmt.Fprintf(&out, "stash %d applied with conflicts in %s; it is kept\n", index, strings.Join(merged.conflicts, ", "))
		for _, note := range merged.notes {
			out.WriteString(note + "\n")
		}
		for _, path := range merged.conflicts {
			out.WriteString(conflictExcerpt(path, r.diskContent(path)))
		}
		out.WriteString("Edit the files to what they should be, without the markers; git check shows what is left.")
		return out.String(), nil
	}
	result := fmt.Sprintf("stash %d applied to %d files", index, len(merged.written))
	if drop {
		if _, err := r.DropStash(index); err != nil {
			return "", err
		}
		result += " and dropped"
	}
	return result, nil
}

// DropStash removes a stash for good.
func (r *Repo) DropStash(index int) (string, error) {
	entries := r.stashes()
	if index < 0 || index >= len(entries) {
		return "", fmt.Errorf("no stash %d; stash_list shows them", index)
	}
	dropped := entries[index]
	if err := r.writeStashes(append(entries[:index:index], entries[index+1:]...)); err != nil {
		return "", err
	}
	return fmt.Sprintf("dropped stash %d (%s)", index, dropped.message), nil
}

// Tags lists the tags with the commit each names.
func (r *Repo) Tags() (string, error) {
	tags, err := r.repo.Tags()
	if err != nil {
		return "", err
	}
	var lines []string
	_ = tags.ForEach(func(ref *plumbing.Reference) error {
		line := ref.Name().Short()
		if annotated, err := r.repo.TagObject(ref.Hash()); err == nil {
			line += fmt.Sprintf(" → %s  %s", annotated.Target.String()[:7], strings.TrimSpace(annotated.Message))
		} else {
			line += " → " + ref.Hash().String()[:7]
		}
		lines = append(lines, line)
		return nil
	})
	if len(lines) == 0 {
		return "no tags", nil
	}
	return strings.Join(lines, "\n"), nil
}

// Tag names HEAD, or a revision: annotated with a message, else light.
func (r *Repo) Tag(name, revision, message string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("tag needs a name")
	}
	commit, err := r.commitOf(orDefault(revision, "HEAD"))
	if err != nil {
		return "", err
	}
	var options *git.CreateTagOptions
	if strings.TrimSpace(message) != "" {
		options = &git.CreateTagOptions{Message: message, Tagger: r.signature()}
	}
	if _, err := r.repo.CreateTag(name, commit.Hash, options); err != nil {
		return "", err
	}
	return fmt.Sprintf("tagged %s as %s", commit.Hash.String()[:7], name), nil
}

// Reset moves the current branch to a revision: soft keeps the index and
// the files, mixed keeps the files, hard drops what is not committed.
func (r *Repo) Reset(revision, mode string) (string, error) {
	if r.inProgress() != nil {
		return "", errors.New("finish or abort what is in progress first")
	}
	commit, err := r.commitOf(orDefault(revision, "HEAD"))
	if err != nil {
		return "", err
	}
	switch mode {
	case "hard":
		err = r.hardReset(commit.Hash)
	case "soft":
		err = r.tree.Reset(&git.ResetOptions{Commit: commit.Hash, Mode: git.SoftReset})
	case "", "mixed":
		err = r.tree.Reset(&git.ResetOptions{Commit: commit.Hash, Mode: git.MixedReset})
	default:
		return "", fmt.Errorf("mode %q: use soft, mixed or hard", mode)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("reset (%s) to %s %s", orDefault(mode, "mixed"), commit.Hash.String()[:7], subject(commit)), nil
}
