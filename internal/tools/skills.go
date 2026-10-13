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
	skillFilesShown     = 30
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
// one, and its description names them with what they are for, or only
// their names; in search mode skill_list and skill_search find them.
func skillTools(found []skills.Skill, mode Mode) []Tool {
	if len(found) == 0 {
		return nil
	}
	description := "Load a skill: instructions for a kind of task, with the folder of its other files. " +
		"Load the one that fits before you start such a task."
	switch mode {
	case ModeCompact:
		description += "\nSkills: " + strings.Join(skills.Names(found), ", ")
	case ModeSearch:
		description += " Find them with skill_list or skill_search."
	default:
		var lines []string
		for _, skill := range found {
			lines = append(lines, "- "+skill.Name+": "+oneLine(skill.Description))
		}
		description += "\nSkills:\n" + strings.Join(lines, "\n")
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
	if mode != ModeSearch {
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
			Description: "Find skills for a task by keywords: their names and what they are for.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
				return searchSkills(found, argField(raw, "query")), nil
			},
			Summary: func(args json.RawMessage) string { return argField(args, "query") },
		},
	)
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func loadSkill(found []skills.Skill, name string) (string, error) {
	index := slices.IndexFunc(found, func(skill skills.Skill) bool { return skill.Name == strings.TrimSpace(name) })
	if index < 0 {
		return "", fmt.Errorf("no skill %q; the skills are: %s", name, strings.Join(skills.Names(found), ", "))
	}
	skill := found[index]
	body, err := skill.Body()
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("Skill %s, in %s\n\n%s", skill.Name, skill.Dir, body)
	if files := skill.Files(skillFilesShown); len(files) > 0 {
		out += "\n\nIts other files, relative to its folder: " + strings.Join(files, ", ")
	}
	return out, nil
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
