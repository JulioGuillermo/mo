package permissions

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | PM-01 | Happy    | A lone * is any words or none, wherever it stands                    | P0       |
// | PM-02 | Happy    | A * inside a word is any characters of that word only                 | P1       |
// | PM-03 | Happy    | re: rules are regular expressions; a broken one is refused           | P1       |
// | PM-04 | Security | A deny rule wins over an allow rule                                  | P0       |
// | PM-05 | Happy    | A script splits into every simple command, also inside $(...)        | P0       |
// | PM-06 | Security | Writing to a file is marked, to /dev/null or 2>&1 is not             | P0       |
// | PM-07 | Edge     | Quotes are removed from plain words; expansions stay as written      | P1       |
// | PM-08 | Edge     | Modes go round in order and the stricter of two is kept              | P2       |

import (
	"testing"
)

func check(t *testing.T, allow []string, command string) Verdict {
	t.Helper()
	policy, err := New(ModeAsk, allow, nil)
	if err != nil {
		t.Fatal(err)
	}
	verdict, _ := policy.Check(command)
	return verdict
}

func TestRules(t *testing.T) {
	t.Run("PM-01 a lone * is any words or none, wherever it stands", func(t *testing.T) {
		cases := []struct {
			rule, command string
			allowed       bool
		}{
			{"ls *", "ls", true},
			{"ls *", "ls -la src", true},
			{"ls *", "lsblk", false},
			{"ls -la *", "ls -la /tmp", true},
			{"ls -la *", "ls /tmp", false},
			{"ls * /home", "ls /home", true},
			{"ls * /home", "ls -la /home", true},
			{"ls * /home", "ls -la /etc", false},
			{"* --help", "go build --help", true},
			{"git status", "git status --short", false},
		}
		for _, c := range cases {
			if got := check(t, []string{c.rule}, c.command) == Allowed; got != c.allowed {
				t.Errorf("%q on %q: allowed = %v", c.rule, c.command, got)
			}
		}
	})

	t.Run("PM-02 a * inside a word is any characters of that word only", func(t *testing.T) {
		if check(t, []string{"git log*"}, "git logs") != Allowed || check(t, []string{"git log*"}, "git log -p") == Allowed {
			t.Error("a * inside a word went past the word")
		}
		if check(t, []string{"fetch https://github.com/*"}, "fetch https://github.com/a/b") != Allowed {
			t.Error("a URL rule did not match")
		}
	})

	t.Run("PM-03 re: rules are regular expressions; a broken one is refused", func(t *testing.T) {
		if check(t, []string{"re:^npm (test|run lint)$"}, "npm run lint") != Allowed || check(t, []string{"re:^npm (test|run lint)$"}, "npm run build") == Allowed {
			t.Error("the regex did not decide")
		}
		if Compile("re:(") == nil {
			t.Error("a broken regex was accepted")
		}
	})

	t.Run("PM-04 a deny rule wins over an allow rule", func(t *testing.T) {
		policy, _ := New(ModeAll, []string{"git *"}, []string{"git push *"})

		verdict, rule := policy.Check("git  push origin main")

		if verdict != Denied || rule != "git push *" {
			t.Errorf("verdict = %v rule = %q", verdict, rule)
		}
	})
}

func TestCommands(t *testing.T) {
	texts := func(commands []Command) []string {
		var out []string
		for _, command := range commands {
			out = append(out, command.Text)
		}
		return out
	}

	t.Run("PM-05 a script splits into every simple command, also inside $(...)", func(t *testing.T) {
		commands, err := Commands("cd src && ls -la | grep x; for f in *.go; do wc -l $f; done\necho $(rm -rf /tmp/x)")

		want := []string{"cd src", "ls -la", "grep x", "wc -l $f", "echo $(…)", "rm -rf /tmp/x"}
		got := texts(commands)
		if err != nil || len(got) != len(want) {
			t.Fatalf("commands = %q err = %v", got, err)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("command %d = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("PM-06 writing to a file is marked, to /dev/null or 2>&1 is not", func(t *testing.T) {
		commands, _ := Commands("ls > out.txt; ls 2>/dev/null; go build 2>&1; echo x >> ~/.bashrc")

		if !commands[0].Writes || commands[1].Writes || commands[2].Writes || !commands[3].Writes {
			t.Errorf("commands = %+v", commands)
		}
	})

	t.Run("PM-07 quotes are removed from plain words; expansions stay as written", func(t *testing.T) {
		commands, _ := Commands(`grep "a b" 'c' d\ e "$HOME/x" FOO=1 env`)
		assigned, _ := Commands(`FOO=1 make`)

		if commands[0].Text != `grep a b c d e "$HOME/x" FOO=1 env` || assigned[0].Text != "FOO=1 make" {
			t.Errorf("commands = %q %q", commands[0].Text, assigned[0].Text)
		}
		if _, err := Commands("echo 'unclosed"); err == nil {
			t.Error("a broken script parsed")
		}
	})

	t.Run("PM-08 modes go round in order and the stricter of two is kept", func(t *testing.T) {
		if ModeAll.Next() != ModeAsk || ModeAsk.Next() != ModeEdits || Stricter(ModeAuto, ModeEdits) != ModeEdits || Stricter(ModeAll, "") != ModeAll {
			t.Error("mode order is wrong")
		}
	})
}
