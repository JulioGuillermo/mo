package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SK-01 | Happy    | $name anywhere becomes [SKILL REF: name], with one note asking to load them with the skill tool; shown as typed | P0 |
// | SK-06 | Edge     | $name of a skill the model does not get keeps its reference and sends the skill whole in a [MO SKILL] block, hidden from the user | P0 |
// | SK-07 | Happy    | The conversation and the queue show the message as typed, without the skill | P0 |
// | SK-02 | Edge     | $ that names no skill, as $HOME or $5, a$b, or $name inside `code` goes as it is | P0 |
// | SK-03 | Error    | $name alone that names no skill is refused                               | P1       |
// | SK-04 | Happy    | $ and /skill offer the skills that match; $ also mid-text, not inside code | P1     |
// | SK-08 | Happy    | Picking a $ suggestion mid-text replaces only the word being typed       | P1       |
// | SK-05 | Happy    | A skill's row turns it off, and on again with a mode                     | P0       |

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/skills"
)

func skillModel(t *testing.T) model {
	t.Helper()
	dir := t.TempDir()
	for name, description := range map[string]string{"pdf": "Fill PDF forms", "deploy": "Ship the app"} {
		folder := filepath.Join(dir, name)
		os.MkdirAll(folder, 0o755)
		os.WriteFile(filepath.Join(folder, skills.FileName), []byte("---\nname: "+name+"\ndescription: "+description+"\n---\nDo the "+name+" steps.\n"), 0o644)
	}
	m := conversationModel()
	m.session.Found = skills.Find([]string{dir})
	m.session.Skills = skills.Names(m.session.Found)
	return m
}

func TestManualSkills(t *testing.T) {
	t.Run("SK-01 $name anywhere becomes [SKILL REF: name], with one note asking to load them with the skill tool; shown as typed", func(t *testing.T) {
		typed := "use $pdf for form.pdf, then $deploy, and $pdf again."

		sent, err := skillModel(t).withSkill(typed)

		want := "use [SKILL REF: pdf] for form.pdf, then [SKILL REF: deploy], and [SKILL REF: pdf] again.\n\n" + agent.SkillRequest([]string{"pdf", "deploy"})
		shown := agent.PromptText(llm.Message{Role: llm.RoleUser, Text: sent})
		if err != nil || sent != want || shown != typed || strings.Contains(sent, "Do the pdf steps.") {
			t.Errorf("sent %q shown %q err %v", sent, shown, err)
		}
	})

	t.Run("SK-06 $name of a skill the model does not get keeps its reference and sends the skill whole in a [MO SKILL] block, hidden from the user", func(t *testing.T) {
		m := skillModel(t)
		m.session.Config.Skills.Disabled = []string{"pdf"}
		typed := "fill form.pdf with $pdf and $deploy"

		sent, err := m.withSkill(typed)

		prompt, notes, _ := strings.Cut(sent, "\n\n")
		shown := agent.PromptText(llm.Message{Role: llm.RoleUser, Text: sent})
		if err != nil || prompt != "fill form.pdf with [SKILL REF: pdf] and [SKILL REF: deploy]" || shown != typed {
			t.Errorf("prompt %q shown %q err %v", prompt, shown, err)
		}
		for _, want := range []string{agent.SkillRequest([]string{"deploy"}), "[MO SKILL: pdf]\nSkill pdf, in ", "Do the pdf steps.\n[END MO SKILL: pdf]"} {
			if !strings.Contains(notes, want) {
				t.Errorf("notes miss %q:\n%s", want, notes)
			}
		}
		if strings.Contains(notes, "Do the deploy steps.") {
			t.Error("a skill the model can load went whole")
		}
	})

	t.Run("SK-07 the conversation and the queue show the message as typed, without the skill", func(t *testing.T) {
		m := skillModel(t)
		m.session.Config.Skills.Disabled = []string{"pdf"}
		m.running, m.cancel = true, func() {}

		queued, _ := m.submitted("$pdf fill form.pdf")

		if got := queued.(model).queued; len(got) != 1 || got[0] != "$pdf fill form.pdf" {
			t.Errorf("queued %q", got)
		}
	})

	t.Run("SK-02 $ that names no skill, as $HOME or $5, a$b, or $name inside `code` goes as it is", func(t *testing.T) {
		for _, prompt := range []string{"$HOME is wrong", "$5 more", "$PATH", "run `echo $pdf`", "a$pdf b", "```\nexport X=$deploy\n```", "`unclosed $pdf"} {
			if sent, err := skillModel(t).withSkill(prompt); err != nil || sent != prompt {
				t.Errorf("%q: sent %q err %v", prompt, sent, err)
			}
		}
	})

	t.Run("SK-03 $name alone that names no skill is refused", func(t *testing.T) {
		if _, err := skillModel(t).withSkill("$fax"); err == nil {
			t.Error("no error")
		}
	})

	t.Run("SK-04 $ and /skill offer the skills that match; $ also mid-text, not inside code", func(t *testing.T) {
		m := skillModel(t)
		suggested := func(input string) []command {
			m.input.SetValue(input)
			return m.suggestions()
		}

		dollar := suggested("$d")
		slash := suggested("/skill p")
		middle := suggested("then use $p")
		code := suggested("run `echo $p")

		if len(dollar) == 0 || dollar[0].name != "$deploy" || len(slash) == 0 || slash[0].name != "/skill pdf" {
			t.Errorf("dollar %+v slash %+v", dollar, slash)
		}
		if len(middle) == 0 || middle[0].name != "$pdf" || len(code) != 0 {
			t.Errorf("middle %+v code %+v", middle, code)
		}
	})

	t.Run("SK-08 picking a $ suggestion mid-text replaces only the word being typed", func(t *testing.T) {
		m := skillModel(t)
		m.input.SetValue("first line\nthen use $p")

		m.completeCommand(m.suggestions()[0])

		if m.input.Value() != "first line\nthen use $pdf " {
			t.Errorf("input %q", m.input.Value())
		}
	})

	t.Run("SK-05 a skill's row turns it off, and on again with a mode", func(t *testing.T) {
		draft := newSettingsDraft(config.Config{Provider: "llama-server"})
		choice := &skillChoice{draft: draft, name: "pdf"}

		choice.Set(skillOff)
		off := slices.Clone(draft.skillsDisabled)
		choice.Set("compact")

		if !slices.Equal(off, []string{"pdf"}) || len(draft.skillsDisabled) != 0 || choice.Get() != "compact" {
			t.Errorf("off %v then %v mode %q", off, draft.skillsDisabled, choice.Get())
		}
	})
}
