//go:build !windows

package gitops

import (
	"context"
	"os/exec"
)

// shellCommand runs a credential helper written as a shell command, as git
// does, with sh.
func shellCommand(ctx context.Context, script string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", script)
}
