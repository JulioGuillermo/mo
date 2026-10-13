package gitops

import (
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
// removes those the commit does not have. Untracked files stay.
func (r *Repo) hardReset(target plumbing.Hash) error {
	tracked := map[string]bool{}
	if index, err := r.repo.Storer.Index(); err == nil {
		for _, entry := range index.Entries {
			tracked[entry.Name] = true
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
	if err := r.tree.Reset(&git.ResetOptions{Commit: target, Mode: git.MixedReset}); err != nil {
		return err
	}
	wanted := map[string]*object.File{}
	if err := tree.Files().ForEach(func(file *object.File) error {
		wanted[file.Name] = file
		return nil
	}); err != nil {
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
		content := fileText(file)
		full := filepath.Join(r.root, path)
		permission := os.FileMode(0o644)
		if file.Mode == filemode.Executable {
			permission = 0o755
		}
		if current, err := os.ReadFile(full); err == nil && string(current) == content {
			if info, err := os.Stat(full); err == nil && info.Mode().Perm()&0o100 == permission&0o100 {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), permission); err != nil {
			return err
		}
		if err := os.Chmod(full, permission); err != nil {
			return err
		}
	}
	return nil
}

// checkout makes HEAD the branch and puts the tracked files at it, without
// go-git's checkout, which deletes untracked files.
func (r *Repo) checkout(branch plumbing.ReferenceName) error {
	ref, err := r.repo.Reference(branch, true)
	if err != nil {
		return err
	}
	if err := r.repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branch)); err != nil {
		return err
	}
	return r.hardReset(ref.Hash())
}
