package tools

import (
	"context"
	"os/exec"
	"slices"
	"sync"
)

// Shell is the shell the bash tool runs commands in. Where only PowerShell
// is found, as on a Windows without Git, the tool is shown to the model and
// the user as powershell, though mo still calls it bash inside: in the
// config, the permission rules and the code.
type Shell struct {
	// Name is the tool's name as the model and the user see it.
	Name string
	// Language names the shell's code, to highlight it.
	Language string
	Program  string
	// Args come before the command.
	Args []string
	// Legacy is Windows PowerShell 5.1, which lacks && and ||.
	Legacy bool
}

const (
	shellToolName      = "bash"
	powershellToolName = "powershell"
)

// powershellPrelude keeps progress bars out of the output and makes it
// UTF-8, as mo reads it.
const powershellPrelude = "$ProgressPreference = 'SilentlyContinue'; [Console]::OutputEncoding = [Text.Encoding]::UTF8; "

func bashShell(program string) Shell {
	return Shell{Name: shellToolName, Language: "bash", Program: program, Args: []string{"-c"}}
}

func powershell(program string, legacy bool) Shell {
	return Shell{Name: powershellToolName, Language: "powershell", Program: program,
		Args: []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command"}, Legacy: legacy}
}

// PowerShell tells whether commands run in PowerShell rather than a POSIX
// shell.
func (s Shell) PowerShell() bool {
	return s.Name == powershellToolName
}

// Command is the command line that runs command in the shell.
func (s Shell) Command(ctx context.Context, command string) *exec.Cmd {
	if s.PowerShell() {
		command = powershellPrelude + command
	}
	args := append(slices.Clone(s.Args), command)
	return exec.CommandContext(ctx, s.Program, args...)
}

var currentShell = sync.OnceValue(detectShell)

// CurrentShell is the shell commands run in here, found once.
func CurrentShell() Shell {
	return currentShell()
}

// UseShell makes commands run in shell until restore is called, for tests
// of what a shell other than this system's changes.
func UseShell(shell Shell) (restore func()) {
	previous := currentShell
	currentShell = func() Shell { return shell }
	return func() { currentShell = previous }
}

// PowerShellForTests is PowerShell 7 or, legacy, Windows PowerShell 5.1.
func PowerShellForTests(legacy bool) Shell {
	return powershell("pwsh", legacy)
}

// Shown is a tool's name as the model and the user see it: the shell's for
// bash, the same for any other.
func Shown(name string) string {
	if name == shellToolName {
		return CurrentShell().Name
	}
	return name
}

// IsShell tells whether a name, as mo or the model writes it, is the shell
// tool's.
func IsShell(name string) bool {
	return name == shellToolName || name == CurrentShell().Name
}
