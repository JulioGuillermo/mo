package gitops

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// hardReset puts the current branch, the index and the tracked files at a
// commit, as git's reset --hard does. go-git's hard reset and checkout also
// delete untracked files, so mo never uses them: it resets the index and
// the branch, then writes back only the tracked files that differ and
// removes those the commit does not have. Untracked files and submodules
// stay.
func (r *Repo) hardReset(target plumbing.Hash) error {
	tracked := map[string]bool{}
	if index, err := r.repo.Storer.Index(); err == nil {
		for _, entry := range index.Entries {
			if entry.Mode != filemode.Submodule {
				tracked[entry.Name] = true
			}
		}
	}
	commit, err := r.repo.CommitObject(target)
	if err != nil {
		return err
	}
	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	wanted := map[string]*object.File{}
	if err := tree.Files().ForEach(func(file *object.File) error {
		wanted[file.Name] = file
		return nil
	}); err != nil {
		return err
	}
	if err := r.tree.Reset(&git.ResetOptions{Commit: target, Mode: git.MixedReset}); err != nil {
		return err
	}
	for path := range tracked {
		if _, kept := wanted[path]; !kept {
			if err := os.Remove(filepath.Join(r.root, path)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	for path, file := range wanted {
		if err := r.writeTracked(path, file); err != nil {
			return err
		}
	}
	return nil
}

// writeTracked puts a file of a commit on disk, unless it is already
// there as the commit has it: a link as a link, the others with their
// executable bit.
func (r *Repo) writeTracked(path string, file *object.File) error {
	full := filepath.Join(r.root, path)
	content := fileText(file)
	if file.Mode == filemode.Symlink {
		if current, err := os.Readlink(full); err == nil && current == content {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Symlink(content, full)
	}
	permission := os.FileMode(0o644)
	if file.Mode == filemode.Executable {
		permission = 0o755
	}
	if info, err := os.Lstat(full); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o100 == permission&0o100 {
		if current, err := os.ReadFile(full); err == nil && string(current) == content {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(full); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(full); err != nil {
			return err
		}
	}
	if err := os.WriteFile(full, []byte(content), permission); err != nil {
		return err
	}
	return os.Chmod(full, permission)
}

// checkout makes HEAD the branch and puts the tracked files at it, without
// go-git's checkout, which deletes untracked files. When the files cannot
// all be written, HEAD and the files go back to the branch before.
func (r *Repo) checkout(branch plumbing.ReferenceName) error {
	ref, err := r.repo.Reference(branch, true)
	if err != nil {
		return err
	}
	before, err := r.repo.Storer.Reference(plumbing.HEAD)
	if err != nil {
		return err
	}
	beforeHash, err := r.repo.ResolveRevision(plumbing.Revision(plumbing.HEAD))
	if err != nil {
		return err
	}
	if err := r.repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branch)); err != nil {
		return err
	}
	switchErr := r.hardReset(ref.Hash())
	if switchErr == nil {
		return nil
	}
	if err := r.repo.Storer.SetReference(before); err != nil {
		return fmt.Errorf("%w; going back failed too: %v", switchErr, err)
	}
	if err := r.hardReset(*beforeHash); err != nil {
		return fmt.Errorf("%w; going back failed too: %v", switchErr, err)
	}
	return fmt.Errorf("switch undone: %w", switchErr)
}
