package gitops

import (
	"errors"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

const fallbackDefaultBranch = "main"

// Init makes a repository in dir, made first when missing, on the branch
// init.defaultBranch of git's config names, else main. It refuses when dir
// is already a repository's own folder.
func Init(dir string) (string, error) {
	if IsRepo(dir) {
		return "", errors.New("already a git repository: " + dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	branch := initialBranch()
	options := &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(branch)}}
	if _, err := git.PlainInitWithOptions(dir, options); err != nil {
		return "", err
	}
	remember(dir)
	return "made a repository in " + dir + " on branch " + branch, nil
}

func initialBranch() string {
	global, err := config.LoadConfig(config.GlobalScope)
	if err != nil || global.Raw == nil || !global.Raw.HasSection("init") {
		return fallbackDefaultBranch
	}
	return orDefault(global.Raw.Section("init").Option("defaultBranch"), fallbackDefaultBranch)
}
