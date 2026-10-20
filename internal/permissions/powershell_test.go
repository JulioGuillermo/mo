package permissions

// Test Traceability Table
// | ID    | Category | Description                                                                   | Priority |
// | PW-01 | Happy    | Pipelines, ; && and || split into their commands, aliases named as their cmdlets | P0     |
// | PW-02 | Happy    | Commands inside $(...), strings, script blocks, if and foreach are parts of their own | P0 |
// | PW-03 | Security | A redirection to a file writes; to $null or 2>&1 it does not                 | P0       |
// | PW-04 | Security | Invoke-Expression, Start-Process, & $variable and .NET calls are hidden: no rule allows them | P0 |
// | PW-05 | Happy    | pwsh -Command, cmd /c and bash -c written out are split into their commands  | P0       |
// | PW-06 | Security | pwsh -EncodedCommand, or -Command from a variable, is hidden                 | P0       |
// | PW-07 | Error    | A script that does not parse is an error, so it asks                         | P1       |
// | PW-08 | Happy    | Quotes are taken off plain words; expanded ones stay as written              | P1       |
// | PW-09 | Happy    | Rules match PowerShell in any case, and an alias in a rule or a command as its cmdlet | P0 |
// | PW-10 | Edge     | & { ... } is not a part itself, its commands are; a script run with . or & 'path' is | P1 |
// | PW-11 | Security | Setting a drive's variable, ${C:\file}, writes; $env: and scopes do not      | P1       |
// | PW-12 | Edge     | Method calls are hidden, property reads are not parts                        | P2       |
// | PW-13 | Happy    | Bash rules stay case-sensitive and know no aliases                           | P1       |

import (
	"slices"
	"testing"
)

func powershellTexts(t *testing.T, script string) []string {
	t.Helper()
	commands, err := PowerShellCommands(script)
	if err != nil {
		t.Fatalf("%q: %v", script, err)
	}
	var texts []string
	for _, command := range commands {
		texts = append(texts, command.Text)
	}
	return texts
}

func onlyCommand(t *testing.T, script string) Command {
	t.Helper()
	commands, err := PowerShellCommands(script)
	if err != nil || len(commands) != 1 {
		t.Fatalf("%q: %+v %v", script, commands, err)
	}
	return commands[0]
}

func TestPowerShellCommands(t *testing.T) {
	t.Run("PW-01 pipelines, ; && and || split into their commands, aliases named as their cmdlets", func(t *testing.T) {
		texts := powershellTexts(t, "ls -Force | select Name; git status && go test ./... || echo failed")

		want := []string{"Get-ChildItem -Force", "Select-Object Name", "git status", "go test ./...", "Write-Output failed"}
		if !slices.Equal(texts, want) {
			t.Errorf("got %q", texts)
		}
	})

	t.Run("PW-02 commands inside $(...), strings, script blocks, if and foreach are parts of their own", func(t *testing.T) {
		texts := powershellTexts(t, `$x = $(Get-Date); Write-Host "now: $(whoami)"; ls | % { rm $_ }; if ($?) { npm test }; foreach ($f in ls *.go) { gofmt -l $f }`)

		for _, want := range []string{"Get-Date", "whoami", "Remove-Item $_", "npm test", "gofmt -l $f", "Get-ChildItem *.go"} {
			if !slices.Contains(texts, want) {
				t.Errorf("no %q in %q", want, texts)
			}
		}
	})

	t.Run("PW-03 a redirection to a file writes; to $null or 2>&1 it does not", func(t *testing.T) {
		for script, writes := range map[string]bool{
			"Get-Date > now.txt":      true,
			"Get-Date >> now.txt":     true,
			"ls *> all.txt":           true,
			"ls 3> warnings.txt":      true,
			"ls 2> $null":             false,
			"ls > $NULL":              false,
			"git status 2>&1":         false,
			"Set-Content notes.md hi": false,
		} {
			if got := onlyCommand(t, script); got.Writes != writes {
				t.Errorf("%q: writes = %v", script, got.Writes)
			}
		}
	})

	t.Run("PW-04 Invoke-Expression, Start-Process, & $variable and .NET calls are hidden: no rule allows them", func(t *testing.T) {
		for _, script := range []string{
			`Invoke-Expression "rm x"`, `iex 'ls'`, `Start-Process notepad`, `& $tool -r x`,
			`[IO.File]::Delete('x')`, `Add-Type -TypeDefinition $code`, `Invoke-Command { rm x }`,
		} {
			commands, err := PowerShellCommands(script)
			if err != nil || !slices.ContainsFunc(commands, func(c Command) bool { return c.Hidden && c.HeldBack() }) {
				t.Errorf("%q: %+v %v", script, commands, err)
			}
		}
	})

	t.Run("PW-05 pwsh -Command, cmd /c and bash -c written out are split into their commands", func(t *testing.T) {
		pwsh := powershellTexts(t, `pwsh -NoProfile -Command 'ls; rm build'`)
		cmd, _ := PowerShellCommands(`cmd /c "dir && del x.txt & echo hi > out.txt"`)
		bash := powershellTexts(t, `bash -c 'ls -la && make test'`)

		if !slices.Equal(pwsh, []string{"Get-ChildItem", "Remove-Item build"}) {
			t.Errorf("pwsh %q", pwsh)
		}
		if len(cmd) != 3 || cmd[0].Text != "dir" || cmd[1].Text != "del x.txt" || cmd[1].Writes || !cmd[2].Writes {
			t.Errorf("cmd %+v", cmd)
		}
		if !slices.Equal(bash, []string{"ls -la", "make test"}) {
			t.Errorf("bash %q", bash)
		}
	})

	t.Run("PW-06 pwsh -EncodedCommand, or -Command from a variable, is hidden", func(t *testing.T) {
		for _, script := range []string{`pwsh -EncodedCommand ZQBjAGgAbwA=`, `powershell -e ZQBj`, `pwsh -c $script`} {
			if got := onlyCommand(t, script); !got.Hidden {
				t.Errorf("%q: %+v", script, got)
			}
		}
	})

	t.Run("PW-07 a script that does not parse is an error, so it asks", func(t *testing.T) {
		for _, script := range []string{`echo 'unclosed`, `ls | | sort`, `if ($x {`} {
			if _, err := PowerShellCommands(script); err == nil {
				t.Errorf("%q parsed", script)
			}
		}
	})

	t.Run("PW-08 quotes are taken off plain words; expanded ones stay as written", func(t *testing.T) {
		got := onlyCommand(t, `Get-Content 'a b.txt' "c d.txt" "$HOME\e.txt" 'it''s'`)

		if got.Text != `Get-Content a b.txt c d.txt "$HOME\e.txt" it's` || got.Source != `Get-Content 'a b.txt' "c d.txt" "$HOME\e.txt" 'it''s'` {
			t.Errorf("text %q source %q", got.Text, got.Source)
		}
	})

	t.Run("PW-09 rules match PowerShell in any case, and an alias in a rule or a command as its cmdlet", func(t *testing.T) {
		policy, _ := New(ModeEdits, []string{"Get-ChildItem *", "cat *", "git status *", "!!rm *"}, nil)
		check := func(script string) Verdict {
			verdict, _ := policy.CheckCommand(onlyCommand(t, script))
			return verdict
		}

		for script, want := range map[string]Verdict{
			"get-childitem -force": Allowed, "dir": Allowed, "GCI": Allowed,
			"Get-Content x": Allowed, "type x": Allowed,
			"GIT status":    Allowed,
			"Remove-Item x": Denied, "del x": Denied,
			"Copy-Item a b": Unknown,
		} {
			if got := check(script); got != want {
				t.Errorf("%q: %v want %v", script, got, want)
			}
		}
	})

	t.Run("PW-10 & { ... } is not a part itself, its commands are; a script run with . or & 'path' is", func(t *testing.T) {
		block := powershellTexts(t, `& { del x; ls }`)
		dotted := powershellTexts(t, `. .\build.ps1 -Release; & 'C:\tools\x.exe' a`)

		if !slices.Equal(block, []string{"Remove-Item x", "Get-ChildItem"}) || !slices.Equal(dotted, []string{`.\build.ps1 -Release`, `C:\tools\x.exe a`}) {
			t.Errorf("block %q dotted %q", block, dotted)
		}
	})

	t.Run("PW-11 setting a drive's variable, ${C:\\file}, writes; $env: and scopes do not", func(t *testing.T) {
		drive, _ := PowerShellCommands(`${C:\notes.txt} = 'x'`)
		harmless, _ := PowerShellCommands(`$env:GOOS = 'linux'; ${env:A} = 1; $global:x = 2`)

		if len(drive) != 1 || !drive[0].Writes || len(harmless) != 0 {
			t.Errorf("drive %+v harmless %+v", drive, harmless)
		}
	})

	t.Run("PW-12 method calls are hidden, property reads are not parts", func(t *testing.T) {
		call, _ := PowerShellCommands(`$f.Delete()`)
		read, _ := PowerShellCommands(`$f.Name; (Get-Item x).Length`)

		if len(call) != 1 || !call[0].Hidden || !slices.Equal(texts(read), []string{"Get-Item x"}) {
			t.Errorf("call %+v read %+v", call, read)
		}
	})

	t.Run("PW-13 bash rules stay case-sensitive and know no aliases", func(t *testing.T) {
		policy, _ := New(ModeEdits, []string{"ls *"}, nil)

		upper, _ := policy.Check("LS -la")
		cmdlet, _ := policy.Check("Get-ChildItem")
		plain, _ := policy.Check("ls -la")

		if upper != Unknown || cmdlet != Unknown || plain != Allowed {
			t.Errorf("upper %v cmdlet %v plain %v", upper, cmdlet, plain)
		}
	})
}

func texts(commands []Command) []string {
	var all []string
	for _, command := range commands {
		all = append(all, command.Text)
	}
	return all
}
