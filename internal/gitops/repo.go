package gitops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// State files, named as git names them so the git command understands a
// merge or cherry-pick mo left half done, and the other way round; the
// list of conflicted paths is mo's own.
const (
	mergeHead      = "MERGE_HEAD"
	mergeMessage   = "MERGE_MSG"
	cherryPickHead = "CHERRY_PICK_HEAD"
	conflictsFile  = "MO_CONFLICTS"
	writtenFile    = "MO_WRITTEN"
	rebaseFile     = "MO_REBASE"
	shownLog       = 20
	shownBranches  = 8
	fallbackName   = "mo"
	fallbackEmail  = "mo@localhost"
)

// ErrNotRepository is returned outside a git repository.
var ErrNotRepository = errors.New("not a git repository")

// Repo is a repository and its working tree.
type Repo struct {
	repo   *git.Repository
	tree   *git.Worktree
	root   string
	gitDir string
}

// Open finds the repository dir is in.
func Open(dir string) (*Repo, error) {
	serveLocalRemotes()
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, ErrNotRepository
	}
	if err != nil {
		return nil, err
	}
	tree, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	gitDir := filepath.Join(tree.Filesystem.Root(), ".git")
	if storage, ok := repo.Storer.(*filesystem.Storage); ok {
		gitDir = storage.Filesystem().Root()
	}
	remember(tree.Filesystem.Root())
	return &Repo{repo: repo, tree: tree, root: tree.Filesystem.Root(), gitDir: gitDir}, nil
}

// operation is what is half done: a merge or a cherry-pick stopped at
// conflicts.
type operation struct {
	kind      string
	theirs    plumbing.Hash
	message   string
	conflicts []string
	written   []string
}

func (r *Repo) stateFile(name string) string {
	return filepath.Join(r.gitDir, name)
}

func (r *Repo) readState(name string) string {
	data, err := os.ReadFile(r.stateFile(name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (r *Repo) inProgress() *operation {
	op := &operation{message: r.readState(mergeMessage)}
	switch {
	case r.readState(mergeHead) != "":
		op.kind, op.theirs = "merge", plumbing.NewHash(r.readState(mergeHead))
	case r.readState(cherryPickHead) != "" && r.readState(rebaseFile) != "":
		op.kind, op.theirs = "rebase", plumbing.NewHash(r.readState(cherryPickHead))
	case r.readState(cherryPickHead) != "":
		op.kind, op.theirs = "cherry-pick", plumbing.NewHash(r.readState(cherryPickHead))
	default:
		return nil
	}
	op.conflicts = nonEmpty(strings.Split(r.readState(conflictsFile), "\n"))
	op.written = nonEmpty(strings.Split(r.readState(writtenFile), "\n"))
	return op
}

func nonEmpty(items []string) []string {
	return slices.DeleteFunc(items, func(item string) bool { return strings.TrimSpace(item) == "" })
}

func (r *Repo) clearState() {
	for _, name := range []string{mergeHead, mergeMessage, cherryPickHead, conflictsFile, writtenFile, rebaseFile} {
		_ = os.Remove(r.stateFile(name))
	}
}

func (r *Repo) head() (*object.Commit, string, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return nil, "", fmt.Errorf("no commit yet: %w", err)
	}
	commit, err := r.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, "", err
	}
	name := ref.Name().Short()
	if !ref.Name().IsBranch() {
		name = "detached at " + ref.Hash().String()[:7]
	}
	return commit, name, nil
}

// Root is the folder of the working tree.
func (r *Repo) Root() string {
	return r.root
}

// CurrentBranch is the branch checked out, even with no commit yet, or ""
// with HEAD detached.
func (r *Repo) CurrentBranch() string {
	head, err := r.repo.Storer.Reference(plumbing.HEAD)
	if err != nil || head.Type() != plumbing.SymbolicReference || !head.Target().IsBranch() {
		return ""
	}
	return head.Target().Short()
}

func (r *Repo) commitOf(revision string) (*object.Commit, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return nil, fmt.Errorf("unknown revision %q", revision)
	}
	return r.repo.CommitObject(*hash)
}

// Status says the branch, what is half done, and the files staged, not
// staged and untracked.
func (r *Repo) Status() (string, error) {
	status, err := r.tree.Status()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if _, branch, err := r.head(); err == nil {
		fmt.Fprintf(&out, "branch %s\n", branch)
	}
	if op := r.inProgress(); op != nil {
		fmt.Fprintf(&out, "%s of %s in progress: %d files had conflicts; git check shows what is left, then git continue or git abort\n",
			op.kind, op.theirs.String()[:7], len(op.conflicts))
	}
	var staged, changed, untracked []string
	for _, path := range slices.Sorted(func(yield func(string) bool) {
		for path := range status {
			if !yield(path) {
				return
			}
		}
	}) {
		file := status[path]
		switch {
		case file.Worktree == git.Untracked:
			untracked = append(untracked, path)
			continue
		case file.Staging != git.Unmodified:
			staged = append(staged, string(file.Staging)+" "+path)
		}
		if file.Worktree != git.Unmodified {
			changed = append(changed, string(file.Worktree)+" "+path)
		}
	}
	for _, group := range []struct {
		title string
		files []string
	}{{"staged", staged}, {"not staged", changed}, {"untracked", untracked}} {
		if len(group.files) > 0 {
			fmt.Fprintf(&out, "%s: %s\n", group.title, strings.Join(group.files, ", "))
		}
	}
	if len(staged)+len(changed)+len(untracked) == 0 {
		out.WriteString("clean\n")
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Diff shows the changes not staged, or the staged ones, of every file or of
// those named.
func (r *Repo) Diff(staged bool, paths []string) (string, error) {
	status, err := r.tree.Status()
	if err != nil {
		return "", err
	}
	head, _, _ := r.head()
	var out strings.Builder
	for _, path := range slices.Sorted(func(yield func(string) bool) {
		for path := range status {
			if !yield(path) {
				return
			}
		}
	}) {
		file := status[path]
		if len(paths) > 0 && !slices.Contains(paths, path) {
			continue
		}
		var before, after string
		if staged {
			if file.Staging == git.Unmodified || file.Staging == git.Untracked {
				continue
			}
			before, after = r.commitContent(head, path), r.indexContent(path)
		} else {
			if file.Worktree == git.Unmodified || file.Worktree == git.Untracked {
				continue
			}
			before, after = r.indexContent(path), r.diskContent(path)
		}
		out.WriteString(Unified(path, before, after))
	}
	if out.Len() == 0 {
		return "no changes", nil
	}
	return out.String(), nil
}

func (r *Repo) commitContent(commit *object.Commit, path string) string {
	if commit == nil {
		return ""
	}
	file, err := commit.File(path)
	if err != nil {
		return ""
	}
	content, _ := file.Contents()
	return content
}

func (r *Repo) indexContent(path string) string {
	index, err := r.repo.Storer.Index()
	if err != nil {
		return ""
	}
	entry, err := index.Entry(path)
	if err != nil {
		return ""
	}
	return r.blob(entry.Hash)
}

func (r *Repo) blob(hash plumbing.Hash) string {
	blob, err := r.repo.BlobObject(hash)
	if err != nil {
		return ""
	}
	reader, err := blob.Reader()
	if err != nil {
		return ""
	}
	defer reader.Close()
	data, _ := io.ReadAll(reader)
	return string(data)
}

func (r *Repo) diskContent(path string) string {
	data, _ := os.ReadFile(filepath.Join(r.root, path))
	return string(data)
}

// Log lists commits from HEAD, or those that touched a path, newest first.
func (r *Repo) Log(limit int, path string) (string, error) {
	if limit <= 0 {
		limit = shownLog
	}
	options := &git.LogOptions{}
	if path != "" {
		options.FileName = &path
	}
	commits, err := r.repo.Log(options)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	count := 0
	err = commits.ForEach(func(commit *object.Commit) error {
		if count == limit {
			return io.EOF
		}
		count++
		subject, _, _ := strings.Cut(commit.Message, "\n")
		fmt.Fprintf(&out, "%s %s %s  %s\n", commit.Hash.String()[:7], commit.Author.When.Format("2006-01-02"), commit.Author.Name, subject)
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Show gives a commit's message and its changes from its first parent.
func (r *Repo) Show(revision string) (string, error) {
	if revision == "" {
		revision = "HEAD"
	}
	commit, err := r.commitOf(revision)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "commit %s\nAuthor: %s <%s>\nDate:   %s\n\n%s\n", commit.Hash, commit.Author.Name, commit.Author.Email,
		commit.Author.When.Format(time.RFC1123Z), indent(strings.TrimRight(commit.Message, "\n")))
	parentTree := &object.Tree{}
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return "", err
		}
		if parentTree, err = parent.Tree(); err != nil {
			return "", err
		}
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	changes, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return "", err
	}
	for _, change := range changes {
		from, to, err := change.Files()
		if err != nil {
			return "", err
		}
		name := change.To.Name
		if name == "" {
			name = change.From.Name
		}
		out.WriteString("\n" + Unified(name, fileText(from), fileText(to)))
	}
	return out.String(), nil
}

func fileText(file *object.File) string {
	if file == nil {
		return ""
	}
	content, _ := file.Contents()
	return content
}

func indent(text string) string {
	return "    " + strings.ReplaceAll(text, "\n", "\n    ")
}

// Blame says which commit last changed each line of a file at HEAD.
func (r *Repo) Blame(path string) (string, error) {
	head, _, err := r.head()
	if err != nil {
		return "", err
	}
	result, err := git.Blame(head, path)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for i, line := range result.Lines {
		fmt.Fprintf(&out, "%s %-12s %4d  %s\n", line.Hash.String()[:7], cutName(line.AuthorName), i+1, line.Text)
	}
	return out.String(), nil
}

func cutName(name string) string {
	if runes := []rune(name); len(runes) > 12 {
		return string(runes[:12])
	}
	return name
}

// Branches lists the local branches, the current one marked.
func (r *Repo) Branches() (string, error) {
	_, current, _ := r.head()
	branches, err := r.repo.Branches()
	if err != nil {
		return "", err
	}
	var names []string
	_ = branches.ForEach(func(ref *plumbing.Reference) error {
		mark := "  "
		if ref.Name().Short() == current {
			mark = "* "
		}
		names = append(names, mark+ref.Name().Short())
		return nil
	})
	slices.Sort(names)
	return strings.Join(names, "\n"), nil
}

// Check says what is half done and where conflict markers are left, in the
// files that had conflicts and in every changed or staged file.
func (r *Repo) Check() (string, error) {
	left, err := r.markersLeft()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if op := r.inProgress(); op != nil {
		fmt.Fprintf(&out, "%s of %s in progress\n", op.kind, op.theirs.String()[:7])
	}
	if len(left) == 0 {
		out.WriteString("no conflict markers left")
		if r.inProgress() != nil {
			out.WriteString("; git continue finishes it")
		}
		return out.String(), nil
	}
	out.WriteString("conflict markers left, resolve them before going on:\n")
	out.WriteString(strings.Join(left, "\n"))
	return out.String(), nil
}

// markersLeft lists "path: lines" for every file still holding conflicts.
func (r *Repo) markersLeft() ([]string, error) {
	paths := map[string]bool{}
	if op := r.inProgress(); op != nil {
		for _, path := range op.conflicts {
			paths[path] = true
		}
	}
	status, err := r.tree.Status()
	if err != nil {
		return nil, err
	}
	for path, file := range status {
		if file.Worktree != git.Unmodified || file.Staging != git.Unmodified {
			paths[path] = true
		}
	}
	var left []string
	for _, path := range slices.Sorted(func(yield func(string) bool) {
		for path := range paths {
			if !yield(path) {
				return
			}
		}
	}) {
		found := Markers(r.diskContent(path))
		if len(found) == 0 {
			continue
		}
		var at []string
		for _, marker := range found {
			at = append(at, fmt.Sprint(marker.Line))
		}
		left = append(left, fmt.Sprintf("%s: line %s", path, strings.Join(at, ", ")))
	}
	return left, nil
}

// Add stages the named paths, or every change when none are named.
func (r *Repo) Add(paths []string) (string, error) {
	if len(paths) == 0 {
		if err := r.tree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			return "", err
		}
		return "staged every change", nil
	}
	for _, path := range paths {
		if _, err := r.tree.Add(path); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
	}
	return "staged " + strings.Join(paths, ", "), nil
}

// Commit records what is staged. It refuses while conflict markers are
// left, and finishes a merge or cherry-pick in progress.
func (r *Repo) Commit(message string) (string, error) {
	if op := r.inProgress(); op != nil {
		if strings.TrimSpace(message) != "" {
			op.message = message
		}
		return r.finish(op)
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("commit needs a message")
	}
	if left, err := r.markersLeft(); err != nil || len(left) > 0 {
		return "", refuseMarkers(left, err)
	}
	hash, err := r.tree.Commit(message, &git.CommitOptions{Author: r.signature()})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("committed %s; hooks do not run here", hash.String()[:7]), nil
}

func refuseMarkers(left []string, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("conflict markers are left; resolve them first:\n%s", strings.Join(left, "\n"))
}

// signature is the user's name and email from git's config, or mo's.
func (r *Repo) signature() *object.Signature {
	signature := &object.Signature{Name: fallbackName, Email: fallbackEmail, When: time.Now()}
	if cfg, err := r.repo.ConfigScoped(config.GlobalScope); err == nil {
		if cfg.User.Name != "" {
			signature.Name = cfg.User.Name
		}
		if cfg.User.Email != "" {
			signature.Email = cfg.User.Email
		}
	}
	return signature
}

// Switch goes to a branch, made first when create is set. Changes to
// tracked files must be committed first: go-git would carry the old
// branch's index over, so it is refused rather than half done.
func (r *Repo) Switch(branch string, create bool) (string, error) {
	if err := r.requireClean(); err != nil {
		return "", err
	}
	name := plumbing.NewBranchReferenceName(branch)
	if create {
		if _, err := r.Branch(branch); err != nil {
			return "", err
		}
	} else if _, err := r.repo.Reference(name, true); err != nil {
		return "", fmt.Errorf("no branch %s", branch)
	}
	if err := r.checkout(name); err != nil {
		return "", err
	}
	if create {
		return "made and switched to " + branch, nil
	}
	return "switched to " + branch, nil
}

// Branch makes a branch at HEAD without switching to it.
func (r *Repo) Branch(name string) (string, error) {
	head, _, err := r.head()
	if err != nil {
		return "", err
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), head.Hash)
	if _, err := r.repo.Reference(ref.Name(), false); err == nil {
		return "", fmt.Errorf("branch %s already exists", name)
	}
	if err := r.repo.Storer.SetReference(ref); err != nil {
		return "", err
	}
	return "made branch " + name + " at " + head.Hash.String()[:7], nil
}

// requireClean refuses to start a merge or cherry-pick over changes to
// tracked files, which it could write over.
func (r *Repo) requireClean() error {
	if r.inProgress() != nil {
		return errors.New("a merge or cherry-pick is in progress: git continue or git abort first")
	}
	status, err := r.tree.Status()
	if err != nil {
		return err
	}
	var dirty []string
	for path, file := range status {
		if file.Worktree != git.Untracked && (file.Worktree != git.Unmodified || file.Staging != git.Unmodified) {
			dirty = append(dirty, path)
		}
	}
	if len(dirty) > 0 {
		slices.Sort(dirty)
		return fmt.Errorf("commit or drop the changes first: %s", strings.Join(dirty, ", "))
	}
	return nil
}

// Merge merges a branch or commit into HEAD: fast-forward when HEAD is
// behind, else three-way. Without conflicts it commits; with them it stops
// with the markers in the files.
func (r *Repo) Merge(revision string) (string, error) {
	if err := r.requireClean(); err != nil {
		return "", err
	}
	ours, oursName, err := r.head()
	if err != nil {
		return "", err
	}
	theirs, err := r.commitOf(revision)
	if err != nil {
		return "", err
	}
	bases, err := ours.MergeBase(theirs)
	if err != nil {
		return "", err
	}
	if len(bases) == 0 {
		return "", fmt.Errorf("%s and %s share no history", oursName, revision)
	}
	base := bases[0]
	switch {
	case base.Hash == theirs.Hash:
		return "already up to date with " + revision, nil
	case base.Hash == ours.Hash:
		return r.fastForward(theirs, revision)
	}
	message := fmt.Sprintf("Merge %s into %s", revision, oursName)
	op := &operation{kind: "merge", theirs: theirs.Hash, message: message}
	result, _, err := r.threeWay(op, base, ours, theirs, oursName, revision)
	return result, err
}

func (r *Repo) fastForward(theirs *object.Commit, revision string) (string, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	if ref.Name().IsBranch() {
		if err := r.repo.Storer.SetReference(plumbing.NewHashReference(ref.Name(), theirs.Hash)); err != nil {
			return "", err
		}
	}
	if err := r.hardReset(theirs.Hash); err != nil {
		return "", err
	}
	return fmt.Sprintf("fast-forwarded to %s (%s)", revision, theirs.Hash.String()[:7]), nil
}

// CherryPick applies the changes a commit made onto HEAD, as a new commit
// with its message and author; with conflicts it stops like a merge.
func (r *Repo) CherryPick(revision string) (string, error) {
	if err := r.requireClean(); err != nil {
		return "", err
	}
	ours, oursName, err := r.head()
	if err != nil {
		return "", err
	}
	picked, err := r.commitOf(revision)
	if err != nil {
		return "", err
	}
	if picked.NumParents() != 1 {
		return "", fmt.Errorf("%s has %d parents; only a commit with one can be picked", revision, picked.NumParents())
	}
	parent, err := picked.Parent(0)
	if err != nil {
		return "", err
	}
	message := strings.TrimRight(picked.Message, "\n") + fmt.Sprintf("\n\n(cherry picked from commit %s)\n", picked.Hash)
	op := &operation{kind: "cherry-pick", theirs: picked.Hash, message: message}
	result, _, err := r.threeWay(op, parent, ours, picked, oursName, picked.Hash.String()[:7])
	return result, err
}

// threeWay merges theirs into ours from base, file by file, writes the
// result to the working tree, stages what merged cleanly, and commits when
// nothing conflicted.
func (r *Repo) threeWay(op *operation, base, ours, theirs *object.Commit, oursName, theirsName string) (string, bool, error) {
	merged, err := r.mergeTrees(base, ours, theirs, oursName, theirsName, true)
	if err != nil {
		return "", false, err
	}
	op.written = append(op.written, merged.written...)
	conflicts, notes := merged.conflicts, merged.notes
	op.conflicts = conflicts
	if len(conflicts) == 0 {
		result, err := r.finish(op)
		return result, false, err
	}
	if err := r.saveState(op); err != nil {
		return "", false, err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s stopped at conflicts in %d files: %s\n", op.kind, len(conflicts), strings.Join(conflicts, ", "))
	for _, note := range notes {
		out.WriteString(note + "\n")
	}
	for _, path := range conflicts {
		out.WriteString(conflictExcerpt(path, r.diskContent(path)))
	}
	out.WriteString("Edit each file to what it should be, without the markers, then git continue; git check shows what is left; git abort drops the " + op.kind + ".")
	return out.String(), true, nil
}

// treeMerge is what merging trees did to the working tree: the paths it
// wrote, those left with conflicts, and notes on conflicts without markers.
type treeMerge struct {
	written, conflicts, notes []string
}

// mergeTrees merges theirs into ours from base, file by file, into the
// working tree, staging what merged cleanly when stage is set.
func (r *Repo) mergeTrees(base, ours, theirs *object.Commit, oursName, theirsName string, stage bool) (treeMerge, error) {
	var merged treeMerge
	files := map[string][3]*object.File{}
	for side, commit := range []*object.Commit{base, ours, theirs} {
		tree, err := commit.Tree()
		if err != nil {
			return merged, err
		}
		err = tree.Files().ForEach(func(file *object.File) error {
			entry := files[file.Name]
			entry[side] = file
			files[file.Name] = entry
			return nil
		})
		if err != nil {
			return merged, err
		}
	}
	for _, unit := range mergeUnits(files) {
		if err := r.mergeUnit(&merged, unit, oursName, theirsName, stage); err != nil {
			return merged, err
		}
	}
	return merged, nil
}

// mergeUnit merges one file's three versions into its target path, and
// removes the old path a rename on their side left.
func (r *Repo) mergeUnit(merged *treeMerge, unit mergeUnit, oursName, theirsName string, stage bool) error {
	b, o, t := unit.base, unit.ours, unit.theirs
	if unit.note != "" {
		merged.notes = append(merged.notes, unit.note)
		merged.conflicts = append(merged.conflicts, unit.target)
	}
	moved := len(unit.gone) > 0
	var content string
	var conflicted bool
	switch {
	case (sameFile(o, t) || sameFile(t, b)) && !moved:
		return nil
	case sameFile(o, t) || sameFile(t, b):
		content = fileText(o)
	case sameFile(o, b) && t == nil:
		merged.written = append(merged.written, unit.target)
		return r.takeTheirs(unit.target, nil)
	case sameFile(o, b):
		content = fileText(t)
	case o == nil || t == nil:
		kept := o
		if kept == nil {
			kept = t
		}
		content, conflicted = fileText(kept), true
		merged.notes = append(merged.notes, unit.target+": deleted on one side, changed on the other; the changed one is kept")
	case isBinary(o) || isBinary(t):
		content, conflicted = fileText(o), true
		merged.notes = append(merged.notes, unit.target+": binary, changed on both sides; ours is kept")
	default:
		content, conflicted = Merge3(fileText(b), fileText(o), fileText(t), oursName, "base", theirsName)
	}
	merged.written = append(merged.written, unit.target)
	if err := r.writeFile(unit.target, content, 0); err != nil {
		return err
	}
	for _, gone := range unit.gone {
		merged.written = append(merged.written, gone)
		if _, err := r.tree.Remove(gone); err != nil {
			_ = os.Remove(filepath.Join(r.root, gone))
		}
	}
	if conflicted {
		if !slices.Contains(merged.conflicts, unit.target) {
			merged.conflicts = append(merged.conflicts, unit.target)
		}
		return nil
	}
	if stage {
		if _, err := r.tree.Add(unit.target); err != nil {
			return err
		}
	}
	return nil
}

// conflictExcerpt shows each conflict of a file with the line it starts on.
func conflictExcerpt(path, text string) string {
	var out strings.Builder
	all := strings.Split(text, "\n")
	for _, marker := range Markers(text) {
		fmt.Fprintf(&out, "\n%s, line %d:\n", path, marker.Line)
		for i := marker.Line - 1; i < len(all); i++ {
			out.WriteString(all[i] + "\n")
			if strings.HasPrefix(all[i], markerTheirs) {
				break
			}
		}
	}
	return out.String()
}

func sameFile(a, b *object.File) bool {
	switch {
	case a == nil || b == nil:
		return a == nil && b == nil
	}
	return a.Hash == b.Hash && a.Mode == b.Mode
}

func isBinary(file *object.File) bool {
	if file == nil {
		return false
	}
	binary, err := file.IsBinary()
	return err == nil && binary
}

func (r *Repo) takeTheirs(path string, theirs *object.File) error {
	if theirs == nil {
		if _, err := r.tree.Remove(path); err != nil {
			return os.Remove(filepath.Join(r.root, path))
		}
		return nil
	}
	if err := r.writeFile(path, fileText(theirs), theirs.Mode); err != nil {
		return err
	}
	_, err := r.tree.Add(path)
	return err
}

func (r *Repo) writeFile(path, content string, _ filemode.FileMode) error {
	full := filepath.Join(r.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	permission := os.FileMode(0o644)
	if info, err := os.Stat(full); err == nil {
		permission = info.Mode().Perm()
	}
	return os.WriteFile(full, []byte(content), permission)
}

func (r *Repo) saveState(op *operation) error {
	head := mergeHead
	if op.kind == "cherry-pick" || op.kind == "rebase" {
		head = cherryPickHead
	}
	for name, content := range map[string]string{
		head: op.theirs.String() + "\n", mergeMessage: op.message + "\n",
		conflictsFile: strings.Join(op.conflicts, "\n") + "\n", writtenFile: strings.Join(op.written, "\n") + "\n",
	} {
		if err := os.WriteFile(r.stateFile(name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Continue finishes a merge or cherry-pick once no marker is left.
func (r *Repo) Continue() (string, error) {
	op := r.inProgress()
	if op == nil {
		return "", errors.New("nothing in progress to continue")
	}
	if op.kind == "rebase" {
		return r.continueRebase(op)
	}
	return r.finish(op)
}

// finish stages what the operation wrote and commits it: a merge with both
// parents, a cherry-pick with the picked commit's author. Markers left
// stop it.
func (r *Repo) finish(op *operation) (string, error) {
	if left, err := r.markersLeft(); err != nil || len(left) > 0 {
		return "", refuseMarkers(left, err)
	}
	for _, path := range op.written {
		if _, err := os.Stat(filepath.Join(r.root, path)); err == nil {
			if _, err := r.tree.Add(path); err != nil {
				return "", err
			}
		} else if _, err := r.tree.Remove(path); err != nil && !errors.Is(err, index.ErrEntryNotFound) {
			return "", err
		}
	}
	head, _, err := r.head()
	if err != nil {
		return "", err
	}
	options := &git.CommitOptions{Author: r.signature(), AllowEmptyCommits: true}
	if op.kind == "merge" {
		options.Parents = []plumbing.Hash{head.Hash, op.theirs}
	} else {
		if empty, err := r.nothingStaged(); err == nil && empty {
			if op.kind != "rebase" {
				r.clearState()
			}
			return fmt.Sprintf("%s of %s left nothing to commit: its changes are already here", op.kind, op.theirs.String()[:7]), nil
		}
		if picked, err := r.repo.CommitObject(op.theirs); err == nil {
			author := picked.Author
			options.Author, options.Committer = &author, r.signature()
		}
	}
	hash, err := r.tree.Commit(op.message, options)
	if err != nil {
		return "", err
	}
	if op.kind != "rebase" {
		r.clearState()
	}
	return fmt.Sprintf("%s committed as %s; hooks do not run here", op.kind, hash.String()[:7]), nil
}

// nothingStaged tells whether the index matches HEAD.
func (r *Repo) nothingStaged() (bool, error) {
	status, err := r.tree.Status()
	if err != nil {
		return false, err
	}
	for _, file := range status {
		if file.Staging != git.Unmodified && file.Staging != git.Untracked {
			return false, nil
		}
	}
	return true, nil
}

// Abort drops a merge or cherry-pick in progress: back to HEAD, and the
// files it added that HEAD does not have are removed.
func (r *Repo) Abort() (string, error) {
	op := r.inProgress()
	if op == nil {
		return "", errors.New("nothing in progress to abort")
	}
	if op.kind == "rebase" {
		return r.abortRebase(op)
	}
	head, _, err := r.head()
	if err != nil {
		return "", err
	}
	if err := r.hardReset(head.Hash); err != nil {
		return "", err
	}
	for _, path := range op.written {
		if _, err := head.File(path); err != nil {
			_ = os.Remove(filepath.Join(r.root, path))
		}
	}
	r.clearState()
	return op.kind + " aborted; back at " + head.Hash.String()[:7], nil
}
