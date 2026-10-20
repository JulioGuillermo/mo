package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/skills"
	"github.com/jg/mo/internal/tools"
)

// withSkill adds to a prompt that starts with $name a note asking the model
// to load that skill with its skill tool, or, when the model does not get
// the skill, the skill itself. Text starting with $ that names no skill,
// such as $HOME or $5, goes as it is.
func (m model) withSkill(prompt string) (string, error) {
	rest, ok := strings.CutPrefix(prompt, "$")
	if !ok {
		return prompt, nil
	}
	name, _, _ := strings.Cut(rest, " ")
	name, _, _ = strings.Cut(name, "\n")
	skill, found := skills.Named(m.session.Found, name)
	if !found {
		if strings.TrimSpace(rest) == name && name != "" && !strings.ContainsAny(name, "0123456789") && strings.ToUpper(name) != name {
			return "", fmt.Errorf("no skill %q · /skill lists them", name)
		}
		return prompt, nil
	}
	if !slices.Contains(m.session.Config.Skills.Disabled, name) {
		return prompt + "\n\n" + agent.SkillRequest(name), nil
	}
	loaded, err := skill.Load(tools.SkillFilesShown)
	if err != nil {
		return "", fmt.Errorf("skill %s: %w", name, err)
	}
	return prompt + "\n\n" + agent.SkillNote + "\n\n" + loaded, nil
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
	lines := []string{fmt.Sprintf("%d skills · $name or /skill name loads one with your message", len(m.session.Found))}
	for _, skill := range m.session.Found {
		line := skill.Name
		if slices.Contains(m.session.Config.Skills.Disabled, skill.Name) {
			line += " (off for the model)"
		}
		lines = append(lines, "  "+line+": "+oneLine(skill.Description, 90))
	}
	return strings.Join(lines, "\n")
}
