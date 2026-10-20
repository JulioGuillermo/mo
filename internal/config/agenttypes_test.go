package config

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AT-01 | Happy    | The user file sets types of subagent with every field                    | P0       |
// | AT-02 | Security | No project file sets types, trusted or not                               | P0       |
// | AT-03 | Error    | A bad name, model, effort, mode or deny rule is refused, naming the type | P1       |
// | AT-04 | Happy    | The tools of a type are named as now, the config passed in left alone    | P2       |

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAgentTypes(t *testing.T) {
	t.Run("AT-01 the user file sets types of subagent with every field", func(t *testing.T) {
		configHome := isolatedHome(t)
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), `[agents.types.reviewer]
description = "reviews a diff"
model = "anthropic/claude-sonnet-4-5"
effort = "high"
prompt = "Be strict."
can_start = true
tools = ["read", "mcp__gh__*"]
skills = ["go-*"]
permissions = "ask"
deny = ["git push *"]
max_steps = 9
`)

		cfg, err := Load(t.TempDir())

		reviewer := cfg.Agents.Types["reviewer"]
		if err != nil || reviewer.Description != "reviews a diff" || reviewer.Model != "anthropic/claude-sonnet-4-5" || reviewer.Effort != "high" ||
			reviewer.Prompt != "Be strict." || !reviewer.CanStart || !slices.Equal(reviewer.Tools, []string{"read", "mcp__gh__*"}) ||
			!slices.Equal(reviewer.Skills, []string{"go-*"}) || reviewer.Permissions != "ask" || !slices.Equal(reviewer.Deny, []string{"git push *"}) || reviewer.MaxSteps != 9 {
			t.Errorf("reviewer %+v err %v", reviewer, err)
		}
	})

	t.Run("AT-02 no project file sets types, trusted or not", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		writeConfig(t, ProjectFile(workDir), "[agents.types.paid]\nmodel = \"anthropic/opus\"\n")
		layer := Config{Agents: Agents{Types: map[string]AgentType{"paid": {Model: "anthropic/opus"}}}}

		untrusted, _ := Load(workDir)
		trusted, _ := LoadLayers(workDir, Config{}, layer)

		if len(untrusted.Agents.Types) != 0 || len(trusted.Agents.Types) != 0 {
			t.Errorf("untrusted %v trusted %v", untrusted.Agents.Types, trusted.Agents.Types)
		}
	})

	t.Run("AT-03 a bad name, model, effort, mode or deny rule is refused, naming the type", func(t *testing.T) {
		cases := map[string]string{
			"[agents.types.Bad_Name]\n":                  "agents.types.Bad_Name",
			"[agents.types.r]\nmodel = \"opus\"\n":       "agents.types.r: model",
			"[agents.types.r]\neffort = \"huge\"\n":      "agents.types.r: effort",
			"[agents.types.r]\npermissions = \"some\"\n": "agents.types.r: permissions",
			"[agents.types.r]\ndeny = [\"re:(\"]\n":      "agents.types.r: deny",
		}
		for file, want := range cases {
			configHome := isolatedHome(t)
			writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), file)

			_, err := Load(t.TempDir())

			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%q: err = %v", file, err)
			}
		}
	})

	t.Run("AT-04 the tools of a type are named as now, the config passed in left alone", func(t *testing.T) {
		cfg := Config{Agents: Agents{Types: map[string]AgentType{"r": {Tools: []string{"fetch", "read"}}}}}

		normalized := normalizeToolNames(cfg)

		if !slices.Equal(normalized.Agents.Types["r"].Tools, []string{"web_fetch", "read"}) || cfg.Agents.Types["r"].Tools[0] != "fetch" {
			t.Errorf("normalized %v original %v", normalized.Agents.Types["r"].Tools, cfg.Agents.Types["r"].Tools)
		}
	})
}
