package tools

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// AgentKind is a kind of subagent the model may pick, as the settings
// set it up.
type AgentKind struct {
	Name        string
	Description string
	Model       string
	Effort      string
	Tools       []string
	Skills      []string
	CanStart    bool
	Permissions string
	MaxSteps    int
}

// agentDescription is the agent tool's description: the kinds of subagent
// are named only when listed is set, and found with action types.
func agentDescription(kinds []AgentKind, listed bool) string {
	description := "Hand tasks to subagents: each starts with only its task and an empty conversation, and you get only its report. " +
		`{"action":"help"} explains each action.`
	if len(kinds) == 0 {
		return description
	}
	if listed {
		var names []string
		for _, kind := range kinds {
			names = append(names, kind.Name)
		}
		return description + "\ntype: " + strings.Join(names, ", ") + `; {"action":"types"} says what each is for. Without type, a default one.`
	}
	return description + "\n" + `type picks a kind of subagent, with its own model and tools: {"action":"types"} lists them. Without type, a default one.`
}

// agentHelpFor is the agent tool's help, with types only when there are
// kinds of subagent.
func agentHelpFor(kinds []AgentKind) []helpTopic {
	if len(kinds) == 0 {
		return agentHelp
	}
	return append(slices.Clone(agentHelp), typesHelp)
}

// agentActions are the agent tool's actions, types only when there are
// kinds of subagent.
func agentActions(kinds []AgentKind) string {
	actions := `"run","send","wait","log","status","stop",`
	if len(kinds) > 0 {
		actions += `"types",`
	}
	return actions + `"help"`
}

// typeProperty is the schema of type: one of the names when they are
// listed, else any name, which a run checks.
func typeProperty(kinds []AgentKind, listed bool) string {
	switch {
	case len(kinds) == 0:
		return ""
	case !listed:
		return `"type":{"type":"string"},`
	}
	var names []string
	for _, kind := range kinds {
		quoted, _ := json.Marshal(kind.Name)
		names = append(names, string(quoted))
	}
	return `"type":{"type":"string","enum":[` + strings.Join(names, ",") + `]},`
}

// describeKinds lists the kinds of subagent, one a line, or tells one in
// detail.
func describeKinds(kinds []AgentKind, name string) (string, error) {
	if name == "" {
		var lines []string
		for _, kind := range kinds {
			lines = append(lines, kind.Name+": "+orDefaultString(kind.Description, "no description"))
		}
		return strings.Join(lines, "\n") + "\nWith type, one in detail.", nil
	}
	for _, kind := range kinds {
		if kind.Name == name {
			return kindDetail(kind), nil
		}
	}
	return "", fmt.Errorf("no kind of subagent %q; types without type lists them", name)
}

func kindDetail(kind AgentKind) string {
	lines := []string{
		kind.Name + ": " + orDefaultString(kind.Description, "no description"),
		"model: " + orDefaultString(kind.Model, "the subagents'") + effortNote(kind.Effort),
		"tools: " + allOr(kind.Tools, "yours"),
		"skills: " + allOr(kind.Skills, "yours"),
		"starts subagents: " + yesNo(kind.CanStart),
		"permissions: yours" + stricterNote(kind.Permissions),
	}
	if kind.MaxSteps > 0 {
		lines = append(lines, fmt.Sprintf("steps: at most %d", kind.MaxSteps))
	}
	return strings.Join(lines, "\n")
}

func effortNote(effort string) string {
	if effort == "" {
		return ""
	}
	return " · effort " + effort
}

func stricterNote(mode string) string {
	if mode == "" {
		return ""
	}
	return ", or " + mode + " when stricter"
}

func allOr(names []string, fallback string) string {
	if len(names) == 0 {
		return fallback
	}
	return strings.Join(names, ", ")
}

func yesNo(yes bool) string {
	if yes {
		return "yes"
	}
	return "no"
}
