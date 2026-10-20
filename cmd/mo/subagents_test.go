package main

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SA-01 | Happy    | The agent tool is there only with subagents on                            | P0       |
// | SA-02 | Happy    | A subagent has its task in its system prompt, no title tool, and the agent tool only above the deepest level | P0 |
// | SA-03 | Happy    | Only subagents have tell_parent                                         | P1       |

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
)

func toolNamesOf(registry *tools.Registry) []string {
	var names []string
	for _, spec := range registry.Specs(nil) {
		names = append(names, spec.Name)
	}
	return names
}

func TestSubagents(t *testing.T) {
	t.Run("SA-01 the agent tool is there only with subagents on", func(t *testing.T) {
		workDir := isolated(t, providerFile(modelServer(t)))
		cfg, _ := config.Load(workDir)
		loaded := testExtensions(t, workDir)
		loaded.team = team.New()
		t.Cleanup(loaded.team.Close)

		off, err := buildSession(context.Background(), workDir, cfg, sessionChoice{}, loaded)
		on := true
		cfg.Agents.Enabled = &on
		enabled, err2 := buildSession(context.Background(), workDir, cfg, sessionChoice{}, loaded)

		if err != nil || err2 != nil || slices.Contains(toolNamesOf(off.Agent.Tools), tools.AgentName) || !slices.Contains(toolNamesOf(enabled.Agent.Tools), tools.AgentName) {
			t.Errorf("err %v %v off %v on %v", err, err2, toolNamesOf(off.Agent.Tools), toolNamesOf(enabled.Agent.Tools))
		}
	})

	t.Run("SA-02 a subagent has its task in its system prompt, no title tool, and the agent tool only above the deepest level", func(t *testing.T) {
		cfg := config.Defaults()
		policy, _ := permissions.New(permissions.ModeEdits, nil, nil)
		env := tools.Env{WorkDir: t.TempDir(), Titles: tools.NewTitles()}
		build, err := subagentBuilder(context.Background(), cfg, chosenProvider{model: "m"}, env, tools.Options{}, policy, nil, "BASE", nil)
		if err != nil {
			t.Fatal(err)
		}
		owner := team.New()
		t.Cleanup(owner.Close)

		deepest, _ := build(team.Info{ID: 4, Depth: 2, Name: "scout"}, nil, nil)
		above, _ := build(team.Info{ID: 3, Depth: 1, Name: "lead"}, owner.Main(), nil)

		names := toolNamesOf(deepest.Tools)
		if slices.Contains(names, tools.AgentName) || slices.Contains(names, "title") || !slices.Contains(toolNamesOf(above.Tools), tools.AgentName) {
			t.Errorf("deepest %v above %v", names, toolNamesOf(above.Tools))
		}
		if !strings.HasPrefix(deepest.System, "BASE") || !strings.Contains(deepest.System, `"scout" (agent 4, level 2)`) || deepest.MaxSteps != cfg.Agents.MaxSteps {
			t.Errorf("system %q steps %d", deepest.System, deepest.MaxSteps)
		}
	})

	t.Run("SA-03 only subagents have tell_parent", func(t *testing.T) {
		workDir := isolated(t, providerFile(modelServer(t)))
		cfg, _ := config.Load(workDir)
		on := true
		cfg.Agents.Enabled = &on
		loaded := testExtensions(t, workDir)
		loaded.team = team.New()
		t.Cleanup(loaded.team.Close)
		session, err := buildSession(context.Background(), workDir, cfg, sessionChoice{}, loaded)
		policy, _ := permissions.New(permissions.ModeEdits, nil, nil)
		build, _ := subagentBuilder(context.Background(), cfg, chosenProvider{model: "m"}, tools.Env{WorkDir: workDir}, tools.Options{}, policy, nil, "", nil)
		subagent, _ := build(team.Info{ID: 1, Depth: 1, Name: "s"}, nil, parentStub{})

		if err != nil || slices.Contains(toolNamesOf(session.Agent.Tools), tools.TellParentName) || !slices.Contains(toolNamesOf(subagent.Tools), tools.TellParentName) {
			t.Errorf("err %v main %v subagent %v", err, toolNamesOf(session.Agent.Tools), toolNamesOf(subagent.Tools))
		}
	})
}

type parentStub struct{}

func (parentStub) Tell(string) (string, error) { return "", nil }
