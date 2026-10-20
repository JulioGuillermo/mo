package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// shellCommand runs a credential helper written as a shell command with
// the sh of Git for Windows, as git does; nil without one.
func shellCommand(ctx context.Context, script string) *exec.Cmd {
	sh := gitSh()
	if sh == "" {
		return nil
	}
	return exec.CommandContext(ctx, sh, "-c", script)
}

// gitSh is the sh that comes with Git for Windows: in the PATH, next to
// the git in the PATH, or where its installer puts it.
func gitSh() string {
	if sh, err := exec.LookPath("sh"); err == nil {
		return sh
	}
	var roots []string
	if git, err := exec.LookPath("git"); err == nil {
		folder := filepath.Dir(git)
		roots = append(roots, filepath.Dir(folder), filepath.Dir(filepath.Dir(folder)))
	}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if dir := os.Getenv(variable); dir != "" {
			roots = append(roots, filepath.Join(dir, "Git"))
		}
	}
	if dir := os.Getenv("LocalAppData"); dir != "" {
		roots = append(roots, filepath.Join(dir, "Programs", "Git"))
	}
	for _, root := range roots {
		for _, sh := range []string{filepath.Join(root, "bin", "sh.exe"), filepath.Join(root, "usr", "bin", "sh.exe")} {
			if info, err := os.Stat(sh); err == nil && !info.IsDir() {
				return sh
			}
		}
	}
	return ""
}
