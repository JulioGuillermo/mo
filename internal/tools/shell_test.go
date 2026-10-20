package tools

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | SH-01 | Happy    | Windows: bash in the PATH is the shell, named bash                           | P0       |
// | SH-02 | Security | Windows: WSL's bash (System32, WindowsApps) is left out; Git's next to git wins | P0    |
// | SH-03 | Happy    | Windows: Git for Windows' bash where its installer puts it                   | P1       |
// | SH-04 | Happy    | Windows: sh or zsh when there is no bash                                     | P1       |
// | SH-05 | Happy    | Windows: PowerShell 7 without a POSIX shell, from the PATH or Program Files  | P0       |
// | SH-06 | Happy    | Windows: Windows PowerShell 5.1 when nothing else, marked legacy             | P1       |
// | SH-07 | Happy    | With PowerShell the model sees a powershell tool, and calls by it run the shell tool | P0 |
// | SH-08 | Happy    | The description names PowerShell; 5.1's warns there is no && or ||           | P0       |
// | SH-09 | Happy    | The command line: bash -c, or pwsh without profile with the UTF-8 prelude    | P1       |
// | SH-10 | Happy    | With bash nothing is renamed: powershell names no tool                       | P1       |
// | SH-11 | Happy    | Hints in read's errors and the job errors name the shell in use              | P2       |

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func fakeWindows(inPath map[string]string, files ...string) windowsSystem {
	env := map[string]string{
		"SystemRoot":   `C:\Windows`,
		"ProgramFiles": `C:\Program Files`,
		"LocalAppData": `C:\Users\me\AppData\Local`,
	}
	return windowsSystem{
		lookPath: func(name string) (string, error) {
			if path, ok := inPath[name]; ok {
				return path, nil
			}
			return "", errors.New("not found")
		},
		getenv: func(name string) string { return env[name] },
		exists: func(path string) bool { return slices.Contains(files, path) },
	}
}

func TestPickWindowsShell(t *testing.T) {
	t.Run("SH-01 Windows: bash in the PATH is the shell, named bash", func(t *testing.T) {
		shell := pickWindowsShell(fakeWindows(map[string]string{"bash": `C:\msys64\usr\bin\bash.exe`, "pwsh": `C:\pwsh.exe`}))

		if shell.Program != `C:\msys64\usr\bin\bash.exe` || shell.Name != "bash" || shell.PowerShell() {
			t.Errorf("shell = %+v", shell)
		}
	})

	t.Run("SH-02 Windows: WSL's bash (System32, WindowsApps) is left out; Git's next to git wins", func(t *testing.T) {
		for _, wsl := range []string{`C:\WINDOWS\system32\bash.exe`, `C:\Users\me\AppData\Local\Microsoft\WindowsApps\bash.exe`} {
			gitBash := `C:\Tools\Git\bin\bash.exe`
			shell := pickWindowsShell(fakeWindows(map[string]string{"bash": wsl, "git": `C:\Tools\Git\cmd\git.exe`}, gitBash))

			if shell.Program != gitBash || shell.Name != "bash" {
				t.Errorf("with %s: shell = %+v", wsl, shell)
			}
		}
	})

	t.Run("SH-03 Windows: Git for Windows' bash where its installer puts it", func(t *testing.T) {
		installed := `C:\Program Files\Git\bin\bash.exe`
		perUser := `C:\Users\me\AppData\Local\Programs\Git\bin\bash.exe`

		machine := pickWindowsShell(fakeWindows(nil, installed))
		user := pickWindowsShell(fakeWindows(nil, perUser))

		if machine.Program != installed || user.Program != perUser {
			t.Errorf("machine = %+v user = %+v", machine, user)
		}
	})

	t.Run("SH-04 Windows: sh or zsh when there is no bash", func(t *testing.T) {
		sh := pickWindowsShell(fakeWindows(map[string]string{"sh": `C:\bin\sh.exe`, "pwsh": `C:\pwsh.exe`}))
		zsh := pickWindowsShell(fakeWindows(map[string]string{"zsh": `C:\bin\zsh.exe`}))

		if sh.Program != `C:\bin\sh.exe` || zsh.Program != `C:\bin\zsh.exe` || sh.Name != "bash" || zsh.PowerShell() {
			t.Errorf("sh = %+v zsh = %+v", sh, zsh)
		}
	})

	t.Run("SH-05 Windows: PowerShell 7 without a POSIX shell, from the PATH or Program Files", func(t *testing.T) {
		inPath := pickWindowsShell(fakeWindows(map[string]string{"pwsh": `C:\pwsh\pwsh.exe`, "powershell": `C:\ps.exe`}))
		installed := `C:\Program Files\PowerShell\7\pwsh.exe`
		outOfPath := pickWindowsShell(fakeWindows(map[string]string{"powershell": `C:\ps.exe`}, installed))

		if inPath.Program != `C:\pwsh\pwsh.exe` || inPath.Name != "powershell" || inPath.Legacy || inPath.Language != "powershell" {
			t.Errorf("in the PATH = %+v", inPath)
		}
		if outOfPath.Program != installed || outOfPath.Legacy {
			t.Errorf("Program Files = %+v", outOfPath)
		}
	})

	t.Run("SH-06 Windows: Windows PowerShell 5.1 when nothing else, marked legacy", func(t *testing.T) {
		inPath := pickWindowsShell(fakeWindows(map[string]string{"powershell": `C:\ps.exe`}))
		nothing := pickWindowsShell(fakeWindows(nil))

		if inPath.Program != `C:\ps.exe` || !inPath.Legacy || !inPath.PowerShell() {
			t.Errorf("in the PATH = %+v", inPath)
		}
		if nothing.Program != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` || !nothing.Legacy {
			t.Errorf("nothing = %+v", nothing)
		}
	})
}

func TestShellNames(t *testing.T) {
	t.Run("SH-07 With PowerShell the model sees a powershell tool, and calls by it run the shell tool", func(t *testing.T) {
		defer UseShell(PowerShellForTests(false))()
		registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{MaxOutput: 1000})

		var names []string
		for _, spec := range registry.Specs(nil) {
			names = append(names, spec.Name)
		}
		tool, found := registry.find("powershell")

		if !slices.Contains(names, "powershell") || slices.Contains(names, "bash") {
			t.Errorf("names = %v", names)
		}
		if !found || tool.Name != "bash" || Canonical("powershell") != "bash" || !IsShell("powershell") || Shown("bash") != "powershell" {
			t.Errorf("found = %v tool = %q", found, tool.Name)
		}
	})

	t.Run("SH-08 The description names PowerShell; 5.1's warns there is no && or ||", func(t *testing.T) {
		modern := shellDescription(PowerShellForTests(false))
		legacy := shellDescription(PowerShellForTests(true))
		bash := shellDescription(bashShell("bash"))

		if !strings.Contains(modern, "PowerShell 7") || !strings.Contains(modern, "not bash") || strings.Contains(modern, "&&") {
			t.Errorf("modern = %q", modern)
		}
		if !strings.Contains(legacy, "PowerShell 5.1") || !strings.Contains(legacy, "no && or ||") {
			t.Errorf("legacy = %q", legacy)
		}
		if strings.Contains(bash, "PowerShell") {
			t.Errorf("bash = %q", bash)
		}
	})

	t.Run("SH-09 The command line: bash -c, or pwsh without profile with the UTF-8 prelude", func(t *testing.T) {
		bash := bashShell("bash").Command(context.Background(), "ls")
		pwsh := PowerShellForTests(false).Command(context.Background(), "Get-ChildItem")

		if strings.Join(bash.Args, " ") != "bash -c ls" {
			t.Errorf("bash = %q", bash.Args)
		}
		last := pwsh.Args[len(pwsh.Args)-1]
		if !slices.Contains(pwsh.Args, "-NoProfile") || !slices.Contains(pwsh.Args, "-NonInteractive") ||
			pwsh.Args[len(pwsh.Args)-2] != "-Command" || !strings.HasPrefix(last, powershellPrelude) || !strings.HasSuffix(last, "Get-ChildItem") {
			t.Errorf("pwsh = %q", pwsh.Args)
		}
	})

	t.Run("SH-10 With bash nothing is renamed: powershell names no tool", func(t *testing.T) {
		defer UseShell(bashShell("bash"))()
		registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{MaxOutput: 1000})

		_, found := registry.find("powershell")

		if found || Canonical("powershell") != "powershell" || Shown("bash") != "bash" || IsShell("powershell") {
			t.Error("powershell named the shell tool with bash in use")
		}
	})

	t.Run("SH-11 Hints in read's errors and the job errors name the shell in use", func(t *testing.T) {
		defer UseShell(PowerShellForTests(false))()
		env := Env{WorkDir: t.TempDir(), Jobs: NewJobs(t.TempDir())}
		_, jobErr := runBash(context.Background(), env, []byte(`{"action":"kill","id":99}`))

		if !strings.Contains(shellHint(hintBinary), "Format-Hex") || jobErr == nil || !strings.Contains(jobErr.Error(), "powershell action output") {
			t.Errorf("hint = %q err = %v", shellHint(hintBinary), jobErr)
		}
	})
}
