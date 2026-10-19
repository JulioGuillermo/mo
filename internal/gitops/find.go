package gitops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const (
	// SearchDepth is how many levels of folders below a directory
	// ReposBelow looks into.
	SearchDepth = 2
	// SearchLimit is how many repositories ReposBelow lists at most.
	SearchLimit = 20
)

var skippedFolders = []string{"node_modules", "vendor"}

// opened are the roots of the repositories opened so far, kept while mo
// runs and never saved.
var opened = struct {
	sync.Mutex
	roots []string
}{}

func remember(root string) {
	opened.Lock()
	defer opened.Unlock()
	if !slices.Contains(opened.roots, root) {
		opened.roots = append(opened.roots, root)
	}
}

// KnownRepos are the roots of the repositories in and below dir, as
// ReposBelow finds them, with every one opened so far, sorted.
func KnownRepos(dir string) []string {
	var roots []string
	if repo, err := Open(dir); err == nil {
		roots = append(roots, repo.Root())
	}
	below, _ := ReposBelow(dir)
	for _, relative := range below {
		roots = append(roots, filepath.Join(dir, relative))
	}
	opened.Lock()
	roots = append(roots, opened.roots...)
	opened.Unlock()
	slices.Sort(roots)
	return slices.Compact(roots)
}

// IsRepo tells whether dir holds a repository of its own: a .git folder,
// or the .git file of a worktree or submodule.
func IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// ReposBelow lists the repositories in the folders under dir, the shallow
// ones first, as paths relative to it. It looks SearchDepth levels down,
// not inside a repository found, a hidden folder, node_modules or vendor.
// more tells that there were more than SearchLimit.
func ReposBelow(dir string) (repos []string, more bool) {
	level := []string{""}
	for depth := 0; depth < SearchDepth && len(level) > 0; depth++ {
		var next []string
		for _, relative := range level {
			for _, folder := range subfolders(filepath.Join(dir, relative)) {
				path := filepath.Join(relative, folder)
				if !IsRepo(filepath.Join(dir, path)) {
					next = append(next, path)
					continue
				}
				if len(repos) == SearchLimit {
					return repos, true
				}
				repos = append(repos, filepath.ToSlash(path))
			}
		}
		level = next
	}
	return repos, false
}

func subfolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") || slices.Contains(skippedFolders, name) {
			continue
		}
		names = append(names, name)
	}
	return names
}
