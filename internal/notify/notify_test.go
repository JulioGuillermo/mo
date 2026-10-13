package notify

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | NT-01 | Happy    | Auto sends Ghostty, kitty and iTerm2 their own notification sequence  | P0       |
// | NT-02 | Security | Text from the model cannot end the sequence or start another         | P0       |
// | NT-03 | Edge     | Inside tmux the sequence is wrapped to reach the terminal            | P1       |
// | NT-04 | Happy    | Bell and off do what they say                                        | P2       |

import (
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestBuild(t *testing.T) {
	t.Run("NT-01 auto sends Ghostty, kitty and iTerm2 their own notification sequence", func(t *testing.T) {
		ghostty := Build(MethodAuto, "mo", "done", env(map[string]string{"TERM_PROGRAM": "ghostty"}))
		kitty := Build(MethodAuto, "mo", "done", env(map[string]string{"KITTY_WINDOW_ID": "1"}))
		iterm := Build(MethodAuto, "mo", "done", env(map[string]string{"TERM_PROGRAM": "iTerm.app"}))

		if ghostty.Escape != "\x1b]777;notify;mo;done\a" || !strings.HasPrefix(kitty.Escape, "\x1b]99;") || iterm.Escape != "\x1b]9;mo: done\a" {
			t.Errorf("ghostty = %q kitty = %q iterm = %q", ghostty.Escape, kitty.Escape, iterm.Escape)
		}
	})

	t.Run("NT-02 text from the model cannot end the sequence or start another", func(t *testing.T) {
		sent := Build(MethodTerminal, "mo; x", "evil\a\x1b]0;title\x07\nnext\x9c", env(map[string]string{"TERM_PROGRAM": "ghostty"}))

		inner := strings.TrimSuffix(strings.TrimPrefix(sent.Escape, "\x1b]777;notify;"), "\a")
		if strings.ContainsAny(inner, "\a\x1b\n\x9c") || inner != "mo, x;evil]0;title next" {
			t.Errorf("escape = %q", sent.Escape)
		}
	})

	t.Run("NT-03 inside tmux the sequence is wrapped to reach the terminal", func(t *testing.T) {
		sent := Build(MethodTerminal, "mo", "done", env(map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/tmux"}))

		if sent.Escape != "\x1bPtmux;\x1b\x1b]777;notify;mo;done\a\x1b\\" {
			t.Errorf("escape = %q", sent.Escape)
		}
	})

	t.Run("NT-04 bell and off do what they say", func(t *testing.T) {
		bell := Build(MethodBell, "mo", "done", env(nil))
		off := Build(MethodOff, "mo", "done", env(nil))

		if bell.Escape != "\a" || off.Escape != "" || off.Desktop != nil {
			t.Errorf("bell = %+v off = %+v", bell, off)
		}
	})
}
