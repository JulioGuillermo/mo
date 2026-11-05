package gitops

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// excludes are the patterns git reads besides .gitignore: the user's own
// file, then the repository's info/exclude. go-git reads info/exclude through
// the worktree, which refuses paths under .git, so it is read here.
func excludes(gitDir string) []gitignore.Pattern {
	patterns := patternsIn(userExcludesFile())
	return append(patterns, patternsIn(filepath.Join(gitDir, "info", "exclude"))...)
}

func patternsIn(file string) []gitignore.Pattern {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var patterns []gitignore.Pattern
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, gitignore.ParsePattern(line, nil))
	}
	return patterns
}

// userExcludesFile is the user's own ignore file, as git finds it:
// core.excludesFile of the global or system config, else
// $XDG_CONFIG_HOME/git/ignore.
func userExcludesFile() string {
	for _, scope := range []config.Scope{config.GlobalScope, config.SystemScope} {
		cfg, err := config.LoadConfig(scope)
		if err != nil || cfg.Raw == nil || !cfg.Raw.HasSection("core") {
			continue
		}
		if file := cfg.Raw.Section("core").Option("excludesfile"); file != "" {
			return expandHome(file)
		}
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = expandHome("~/.config")
	}
	return filepath.Join(configHome, "git", "ignore")
}

func expandHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(path, "~/") {
		return path
	}
	return filepath.Join(home, path[2:])
}

// status is the worktree's status without the untracked files git ignores.
// go-git's own check lets a pattern for a folder, as !/tmp/keep/, match the
// files in it too, so some ignored files show as untracked.
func (r *Repo) status() (git.Status, error) {
	status, err := r.tree.Status()
	if err != nil {
		return nil, err
	}
	var patterns []gitignore.Pattern
	for path, file := range status {
		if file.Worktree != git.Untracked {
			continue
		}
		if patterns == nil {
			patterns, _ = gitignore.ReadPatterns(r.tree.Filesystem, nil)
			patterns = append(patterns, r.tree.Excludes...)
		}
		if ignored(patterns, strings.Split(path, "/")) {
			delete(status, path)
		}
	}
	return status, nil
}

// ignored tells whether git ignores a file: one of its folders is ignored,
// or the file itself is.
func ignored(patterns []gitignore.Pattern, path []string) bool {
	for depth := 1; depth <= len(path); depth++ {
		isDir := depth < len(path)
		if lastMatch(patterns, path[:depth], isDir) == gitignore.Exclude {
			return true
		}
	}
	return false
}

// lastMatch is what the last pattern that matches path itself says, not
// one that matches only a folder above it.
func lastMatch(patterns []gitignore.Pattern, path []string, isDir bool) gitignore.MatchResult {
	for i := len(patterns) - 1; i >= 0; i-- {
		result := patterns[i].Match(path, isDir)
		if result == gitignore.NoMatch {
			continue
		}
		if len(path) > 1 && patterns[i].Match(path[:len(path)-1], true) != gitignore.NoMatch {
			continue
		}
		return result
	}
	return gitignore.NoMatch
}
