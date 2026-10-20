package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/skills"
	"github.com/jg/mo/internal/tools"
)

// withSkill turns each $name of a skill in a prompt into a reference to
// it, with a note after the prompt asking the model to load them with its
// skill tool. The skills the model does not get go whole after the note
// instead. $name alone that names no skill is refused; any other $ goes
// as it is.
func (m model) withSkill(prompt string) (string, error) {
	refs := skillRefs(prompt, m.session.Found)
	if len(refs) == 0 {
		return prompt, unknownSkill(prompt)
	}
	var text strings.Builder
	var requested, off []string
	from := 0
	for _, ref := range refs {
		text.WriteString(prompt[from:ref.start])
		text.WriteString(agent.SkillRef(ref.name))
		from = ref.end
		if slices.Contains(requested, ref.name) || slices.Contains(off, ref.name) {
			continue
		}
		if slices.Contains(m.session.Config.Skills.Disabled, ref.name) {
			off = append(off, ref.name)
		} else {
			requested = append(requested, ref.name)
		}
	}
	text.WriteString(prompt[from:])
	if len(requested) > 0 {
		text.WriteString("\n\n" + agent.SkillRequest(requested))
	}
	for _, name := range off {
		skill, _ := skills.Named(m.session.Found, name)
		loaded, err := skill.Load(tools.SkillFilesShown)
		if err != nil {
			return "", fmt.Errorf("skill %s: %w", name, err)
		}
		text.WriteString("\n\n" + agent.SkillBlock(name, loaded))
	}
	return text.String(), nil
}

// unknownSkill refuses a prompt that is only $name when that looks meant as
// a skill but names none: not $HOME, $PATH or $5.
func unknownSkill(prompt string) error {
	name, alone := strings.CutPrefix(strings.TrimSpace(prompt), "$")
	if !alone || name == "" || strings.ContainsAny(name, " \n0123456789") || strings.ToUpper(name) == name {
		return nil
	}
	return fmt.Errorf("no skill %q · /skill lists them", name)
}

// typedSkill is the $word being typed at the end of the input, outside
// code between backticks.
func (m model) typedSkill() (string, bool) {
	value := m.input.Value()
	if strings.HasPrefix(value, "/") {
		return "", false
	}
	start := strings.LastIndexAny(value, " \n\t") + 1
	word, isSkill := strings.CutPrefix(value[start:], "$")
	if !isSkill || startsMidWord(value, start) || insideAny(start, codeSpans(value)) {
		return "", false
	}
	return word, true
}

// skillSuggestions are the skills whose name starts with what was typed,
// then those holding it, written after prefix.
func (m model) skillSuggestions(typed, prefix string) []command {
	var first, then []command
	for _, skill := range m.session.Found {
		candidate := command{name: prefix + skill.Name, description: skill.Description, skill: true}
		switch {
		case strings.HasPrefix(skill.Name, typed):
			first = append(first, candidate)
		case strings.Contains(skill.Name, typed):
			then = append(then, candidate)
		}
	}
	return append(first, then...)
}

// skillReport lists the skills, saying which the model does not get.
func (m model) skillReport() string {
	if len(m.session.Found) == 0 {
		return "no skills found · add folders with a SKILL.md to ~/.config/mo/skills or the project's .mo/skills"
	}
	lines := []string{fmt.Sprintf("%d skills · $name anywhere in a message, or /skill name, has the model load one", len(m.session.Found))}
	for _, skill := range m.session.Found {
		line := skill.Name
		if slices.Contains(m.session.Config.Skills.Disabled, skill.Name) {
			line += " (off for the model)"
		}
		lines = append(lines, "  "+line+": "+oneLine(skill.Description, 90))
	}
	return strings.Join(lines, "\n")
}
