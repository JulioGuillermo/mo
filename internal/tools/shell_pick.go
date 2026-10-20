package tools

import "strings"

// windowsSystem is what picking a shell on Windows looks at, apart so the
// choice can be tested anywhere.
type windowsSystem struct {
	lookPath func(name string) (string, error)
	getenv   func(name string) string
	exists   func(path string) bool
}

// pickWindowsShell prefers a POSIX shell, the one the models know best:
// bash in the PATH, else Git for Windows' bash, else sh or zsh. The bash of
// WSL does not count: it runs commands in Linux, apart from these files'
// Windows. Without any, PowerShell 7, else Windows PowerShell.
func pickWindowsShell(system windowsSystem) Shell {
	if bash, ok := system.inPath("bash"); ok {
		return bashShell(bash)
	}
	if bash, ok := system.gitBash(); ok {
		return bashShell(bash)
	}
	for _, name := range []string{"sh", "zsh"} {
		if program, ok := system.inPath(name); ok {
			return bashShell(program)
		}
	}
	if pwsh, ok := system.inPath("pwsh"); ok {
		return powershell(pwsh, false)
	}
	if pwsh := windowsPath(system.getenv("ProgramFiles"), `PowerShell\7\pwsh.exe`); system.exists(pwsh) {
		return powershell(pwsh, false)
	}
	if legacy, ok := system.inPath("powershell"); ok {
		return powershell(legacy, true)
	}
	if legacy := windowsPath(system.getenv("SystemRoot"), `System32\WindowsPowerShell\v1.0\powershell.exe`); legacy != "" {
		return powershell(legacy, true)
	}
	return powershell("powershell.exe", true)
}

// inPath finds a program in the PATH, leaving out WSL's launchers.
func (system windowsSystem) inPath(name string) (string, bool) {
	program, err := system.lookPath(name)
	if err != nil || system.isWSL(program) {
		return "", false
	}
	return program, true
}

func (system windowsSystem) isWSL(program string) bool {
	lower := strings.ToLower(program)
	systemDir := strings.ToLower(windowsPath(system.getenv("SystemRoot"), `System32\`))
	return strings.Contains(lower, `\windowsapps\`) || (systemDir != "" && strings.HasPrefix(lower, systemDir))
}

// gitBash is the bash of Git for Windows: next to the git in the PATH, or
// where its installer puts it. Its bin\bash.exe sets up the PATH of its
// tools, unlike usr\bin\bash.exe.
func (system windowsSystem) gitBash() (string, bool) {
	var roots []string
	if git, err := system.lookPath("git"); err == nil {
		folder := parentPath(git)
		roots = append(roots, parentPath(folder), parentPath(parentPath(folder)))
	}
	roots = append(roots,
		windowsPath(system.getenv("ProgramFiles"), "Git"),
		windowsPath(system.getenv("ProgramFiles(x86)"), "Git"),
		windowsPath(system.getenv("LocalAppData"), `Programs\Git`))
	for _, root := range roots {
		if root == "" {
			continue
		}
		if bash := windowsPath(root, `bin\bash.exe`); system.exists(bash) {
			return bash, true
		}
	}
	return "", false
}

// windowsPath joins with backslashes, whatever system builds it; nothing
// under an unknown root.
func windowsPath(root, rest string) string {
	if root == "" {
		return ""
	}
	return strings.TrimRight(root, `\`) + `\` + rest
}

func parentPath(path string) string {
	cut := strings.LastIndexAny(path, `\/`)
	if cut <= 0 {
		return ""
	}
	return path[:cut]
}
