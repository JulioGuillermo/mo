package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SK-01 | Happy    | $name sends the message with the skill loaded after it, as typed shown   | P0       |
// | SK-02 | Edge     | $ that names no skill, as $HOME or $5, goes as it is                     | P0       |
// | SK-03 | Error    | $name alone that names no skill is refused                               | P1       |
// | SK-04 | Happy    | $ and /skill offer the skills that match                                 | P1       |
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
	t.Run("SK-01 $name sends the message with the skill loaded after it, as typed shown", func(t *testing.T) {
		sent, err := skillModel(t).withSkill("$pdf fill form.pdf")

		shown := agent.PromptText(llm.Message{Role: llm.RoleUser, Text: sent})
		if err != nil || !strings.Contains(sent, "Do the pdf steps.") || !strings.Contains(sent, agent.SkillNote) || shown != "$pdf fill form.pdf" {
			t.Errorf("sent %q shown %q err %v", sent, shown, err)
		}
	})

	t.Run("SK-02 $ that names no skill, as $HOME or $5, goes as it is", func(t *testing.T) {
		for _, prompt := range []string{"$HOME is wrong", "$5 more", "$PATH"} {
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

	t.Run("SK-04 $ and /skill offer the skills that match, those starting so first", func(t *testing.T) {
		m := skillModel(t)
		m.input.SetValue("$d")
		dollar := m.suggestions()
		m.input.SetValue("/skill p")
		slash := m.suggestions()

		if len(dollar) == 0 || dollar[0].name != "$deploy" || len(slash) == 0 || slash[0].name != "/skill pdf" {
			t.Errorf("dollar %+v slash %+v", dollar, slash)
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
