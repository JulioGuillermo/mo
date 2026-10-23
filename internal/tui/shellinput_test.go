package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SI-01 | Happy    | ! and !! lead a shell command; anything else is not one                   | P0       |
// | SI-02 | Happy    | Each part of a command has its colour: programs, flags short and long, numbers, strings, variables, operators, paths | P0 |
// | SI-03 | Happy    | The input draws a command coloured, its text and cursor where they were  | P0       |
// | SI-04 | Happy    | The PATH's programs are found, each once, executables only               | P1       |
// | SI-05 | Happy    | After the program, files and folders are offered; tab puts one in place of the word | P0 |
// | SI-06 | Edge     | Enter runs the command even with suggestions open                        | P0       |
// | SI-07 | Edge     | A name with spaces or quotes is quoted for the shell                     | P1       |
// | SI-08 | Happy    | ctrl+c clears what is typed and the model goes on; with nothing typed it stops it | P0 |
// | SI-09 | Happy    | PowerShell has its cmdlets, -Params, operator words, $env:vars, [Types], strings and <# comments #> apart | P0 |
// | SI-10 | Happy    | Code in bash or PowerShell is coloured by mo, as the shell's commands are | P1 |

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestShellInput(t *testing.T) {
	t.Run("SI-01 ! and !! lead a shell command; anything else is not one", func(t *testing.T) {
		marks, command, ok := typedShell("!ls -la")
		twice, told, toldOK := typedShell("!!go test")
		_, _, plain := typedShell("hello !x")

		if marks != "!" || command != "ls -la" || !ok || twice != "!!" || told != "go test" || !toldOK || plain {
			t.Errorf("! %q %q %v · !! %q %q %v · plain %v", marks, command, ok, twice, told, toldOK, plain)
		}
	})

	t.Run("SI-02 each part of a command has its colour: programs, flags short and long, numbers, strings, variables, operators, paths", func(t *testing.T) {
		command := `go build -o mo --tags="a b" ./cmd/mo | grep -c 'ok' 2>/dev/null && FOO=1 echo "$HOME" ${X} 42 notes >> out.log; ls README.md plain # done`
		parts := shellParts(command, func(word string) bool { return word == "README.md" })
		partOf := func(word string) shellPart { return parts[len([]rune(command[:strings.Index(command, word)]))] }

		want := map[string]shellPart{
			"go": partProgram, "build": partText, "-o": partShortFlag, "--tags": partLongFlag, `"a b"`: partString, "./cmd/mo": partPath,
			"| grep": partOperator, "grep": partProgram, "-c": partShortFlag, "'ok'": partString, "2>": partOperator, "/dev/null": partPath,
			"&&": partOperator, "FOO": partVariable, "echo": partProgram, `"$HOME"`: partString, "$HOME": partVariable, "${X}": partVariable,
			"42": partNumber, "notes": partText, ">>": partOperator, "out.log": partPath, "; ls": partOperator, "ls ": partProgram,
			"README.md": partPath, "plain": partText, "# done": partComment,
		}
		for word, part := range want {
			if got := partOf(word); got != part {
				t.Errorf("%q is %v, want %v", word, got, part)
			}
		}
	})

	t.Run("SI-03 the input draws a command coloured, its text and cursor where they were", func(t *testing.T) {
		m := conversationModel()
		m.input.SetValue("!go build ./cmd/mo")

		plain := m.input.View()
		drawn := m.shellInputView(plain)

		if ansi.Strip(drawn) != ansi.Strip(plain) {
			t.Errorf("text changed:\n%q\n%q", ansi.Strip(plain), ansi.Strip(drawn))
		}
		if drawn == plain || !strings.Contains(drawn, "\x1b[7;48;2;36;40;59m ") {
			t.Errorf("not coloured, or no cursor at the end:\n%q", drawn)
		}
		m.input.SetValue("hello")
		if m.shellInputView(m.input.View()) != m.input.View() {
			t.Error("a message was coloured")
		}
	})

	t.Run("SI-04 the PATH's programs are found, each once, executables only", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows finds programs by PATHEXT")
		}
		first, second := t.TempDir(), t.TempDir()
		for _, each := range []struct {
			dir, name string
			mode      os.FileMode
		}{{first, "mytool", 0o755}, {second, "mytool", 0o755}, {second, "other", 0o755}, {second, "notes.txt", 0o644}} {
			os.WriteFile(filepath.Join(each.dir, each.name), []byte("x"), each.mode)
		}
		t.Setenv("PATH", first+string(os.PathListSeparator)+second)

		if found := findPrograms(); !slices.Equal(found, []string{"mytool", "other"}) {
			t.Errorf("programs = %q", found)
		}
	})

	t.Run("SI-05 after the program, files and folders are offered; tab puts one in place of the word", func(t *testing.T) {
		m := searchModel(t)
		m.input.SetValue("!cat s")

		offered := m.suggestions()
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = updated.(model)
		folder := m.input.Value()
		m.input.SetValue("!cat src/pkg/rea")
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

		if len(offered) != 1 || offered[0].name != "src"+string(filepath.Separator) || offered[0].description != "folder" {
			t.Fatalf("offered %+v", offered)
		}
		if folder != "!cat src"+string(filepath.Separator) || updated.(model).input.Value() != "!cat src/pkg/readme.md " {
			t.Errorf("folder %q file %q", folder, updated.(model).input.Value())
		}
	})

	t.Run("SI-06 enter runs the command even with suggestions open", func(t *testing.T) {
		m := searchModel(t)
		m.input.SetValue("!cat s")

		taken, _, _ := m.handleSuggestionKey("enter", m.suggestions())

		if taken {
			t.Error("enter took the suggestion instead of running the command")
		}
	})

	t.Run("SI-07 a name with spaces or quotes is quoted for the shell", func(t *testing.T) {
		if shellQuoted("plain.go") != "plain.go" || shellQuoted("my file.md") != "'my file.md'" || shellQuoted("it's") != `'it'\''s'` {
			t.Errorf("%q %q %q", shellQuoted("plain.go"), shellQuoted("my file.md"), shellQuoted("it's"))
		}
	})

	t.Run("SI-08 ctrl+c clears what is typed and the model goes on; with nothing typed it stops it", func(t *testing.T) {
		m := conversationModel()
		stopped := false
		m.running, m.cancel = true, func() { stopped = true }
		m.input.SetValue("!go test ./...")
		ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

		cleared, _ := m.Update(ctrlC)
		clearedStopped := stopped
		_, _ = cleared.(model).Update(ctrlC)

		if cleared.(model).input.Value() != "" || clearedStopped {
			t.Errorf("input %q stopped %v", cleared.(model).input.Value(), clearedStopped)
		}
		if !stopped {
			t.Error("ctrl+c on an empty input did not stop the model")
		}
	})

	t.Run("SI-09 PowerShell has its cmdlets, -Params, operator words, $env:vars, [Types], strings and <# comments #> apart", func(t *testing.T) {
		command := "Get-ChildItem -Path $env:USERPROFILE -Recurse | Where-Object { $_.Length -gt 1024 } <# big #>\n[int]$n = 42; Write-Host \"n: `\"$n\" *> out.log; git --version 2>&1"
		parts := powershellDialect.parts(command, func(string) bool { return false })
		partOf := func(word string) shellPart { return parts[len([]rune(command[:strings.Index(command, word)]))] }

		want := map[string]shellPart{
			"Get-ChildItem": partProgram, "-Path": partShortFlag, "$env:USERPROFILE": partVariable, "-Recurse": partShortFlag,
			"| Where": partOperator, "Where-Object": partProgram, "{ $_": partOperator, "$_": partVariable, "-gt": partOperator,
			"1024": partNumber, "<# big #>": partComment, "[int]": partType, "$n =": partVariable, "= 42": partOperator, "42": partNumber,
			"Write-Host": partProgram, `"n: `: partString, "$n\"": partVariable, "*>": partOperator, "out.log": partPath,
			"git": partProgram, "--version": partLongFlag, "2>&1": partOperator,
		}
		for word, part := range want {
			if got := partOf(word); got != part {
				t.Errorf("%q is %v, want %v", word, got, part)
			}
		}
		if end := strings.Index(command, `" *>`); parts[len([]rune(command[:end]))] != partString {
			t.Error("the backtick did not keep the string open")
		}
	})

	t.Run("SI-10 code in bash or PowerShell is coloured by mo, as the shell's commands are", func(t *testing.T) {
		colors := syntaxPalette(true)
		program := sgrOf(colors.program.color)

		bash := strings.Join(highlightLines("ls -la", "bash", 80, changePalette(true).block, true), "")
		pwsh := strings.Join(highlightLines("Get-Item -Path x", "powershell", 80, changePalette(true).block, true), "")
		rows := lexRows("PowerShell", "Get-Item -Path x", 1)

		if !strings.Contains(bash, program) || !strings.Contains(pwsh, program) || len(rows) != 1 || rows[0][0].kind != kindProgram {
			t.Errorf("bash %q pwsh %q rows %+v", bash, pwsh, rows)
		}
	})
}
