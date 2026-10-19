package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/skills"
)

const (
	skillToolName       = "skill"
	skillListToolName   = "skill_list"
	skillSearchToolName = "skill_search"
	// SkillFilesShown is how many of a skill's other files loading it lists.
	SkillFilesShown = 30
)

func isSkillTool(name string) bool {
	return name == skillToolName || name == skillListToolName || name == skillSearchToolName
}

// isMetaTool tells the tools that are always declared, whatever the mode,
// and never listed or found among the others: the search tools and the
// skill tools.
func isMetaTool(name string) bool {
	return isSearchTool(name) || isSkillTool(name)
}

// skillTools are the tools of the skills, none without skills: skill loads
// one, and its description names each in full mode with what it is for,
// and each in compact mode by name; skills in search mode are left for
// skill_list and skill_search, which come with them.
func skillTools(found []skills.Skill, mode Mode, modes map[string]Mode) []Tool {
	if len(found) == 0 {
		return nil
	}
	var full, compact []string
	searched := false
	for _, skill := range found {
		switch skillMode(skill.Name, mode, modes) {
		case ModeCompact:
			compact = append(compact, skill.Name)
			searched = true
		case ModeSearch:
			searched = true
		default:
			full = append(full, "- "+skill.Name+": "+oneLine(skill.Description))
		}
	}
	description := "Load a skill: instructions for a kind of task, with the folder of its other files. " +
		"Load the one that fits before you start such a task."
	if len(full) > 0 {
		description += "\nSkills:\n" + strings.Join(full, "\n")
	}
	if len(compact) > 0 {
		label := "Skills: "
		if len(full) > 0 {
			label = "Also: "
		}
		description += "\n" + label + strings.Join(compact, ", ")
	}
	if searched {
		description += "\nskill_search tells what the others are for, and finds more."
	}
	tools := []Tool{{
		Name:        skillToolName,
		Description: description,
		Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
			return loadSkill(found, argField(raw, "name"))
		},
		Summary: func(args json.RawMessage) string { return argField(args, "name") },
	}}
	if !searched {
		return tools
	}
	return append(tools,
		Tool{
			Name:        skillListToolName,
			Description: "List the names of the skills. filter: words to match in a skill's name or description.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"filter":{"type":"string"}}}`),
			Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
				return listSkills(found, argField(raw, "filter")), nil
			},
			Summary: func(args json.RawMessage) string { return argField(args, "filter") },
		},
		Tool{
			Name:        skillSearchToolName,
			Description: "Find skills for a task by keywords, or one by its name: their names and what they are for.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
				return searchSkills(found, argField(raw, "query")), nil
			},
			Summary: func(args json.RawMessage) string { return argField(args, "query") },
		},
	)
}

// skillMode is a skill's own mode, else the default.
func skillMode(name string, mode Mode, modes map[string]Mode) Mode {
	if own := modes[name]; own == ModeFull || own == ModeCompact || own == ModeSearch {
		return own
	}
	return mode
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func loadSkill(found []skills.Skill, name string) (string, error) {
	skill, ok := skills.Named(found, name)
	if !ok {
		return "", fmt.Errorf("no skill %q; the skills are: %s", name, strings.Join(skills.Names(found), ", "))
	}
	return skill.Load(SkillFilesShown)
}

func listSkills(found []skills.Skill, filter string) string {
	words := strings.Fields(strings.ToLower(filter))
	var names []string
	for _, skill := range found {
		if containsAll(strings.ToLower(skill.Name+" "+skill.Description), words) {
			names = append(names, skill.Name)
		}
	}
	if len(names) == 0 {
		return "no skills match; skill_list without a filter names them all"
	}
	return strings.Join(names, ", ")
}

func searchSkills(found []skills.Skill, query string) string {
	words := strings.Fields(strings.ToLower(query))
	type scored struct {
		skill skills.Skill
		score int
	}
	var ranked []scored
	for _, skill := range found {
		text := strings.ToLower(skill.Name + " " + skill.Description)
		score := 0
		// A skill named exactly comes first.
		if strings.EqualFold(skill.Name, strings.TrimSpace(query)) {
			score = len(words) + 1
		}
		for _, word := range words {
			if strings.Contains(text, word) {
				score++
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{skill, score})
		}
	}
	if len(ranked) == 0 {
		return "no skills match; skill_list names them"
	}
	slices.SortStableFunc(ranked, func(a, b scored) int { return b.score - a.score })
	var lines []string
	for _, entry := range ranked[:min(len(ranked), searchMaxResults)] {
		lines = append(lines, entry.skill.Name+": "+oneLine(entry.skill.Description))
	}
	return strings.Join(lines, "\n") + "\n\nLoad one with skill."
}
