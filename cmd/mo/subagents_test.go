package main

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SA-01 | Happy    | The agent tool is there only with subagents on                            | P0       |
// | SA-02 | Happy    | A subagent has its task in its system prompt, no title tool, and the agent tool only above the deepest level | P0 |
// | SA-03 | Happy    | Only subagents have tell_parent                                         | P1       |
// | SA-04 | Happy    | A type runs on its model and effort, with its prompt and steps           | P0       |
// | SA-05 | Happy    | A type has only the tools and skills it names, * for many; tell_parent stays | P0   |
// | SA-06 | Happy    | A type starts subagents only with can_start, and while the levels allow  | P0       |
// | SA-07 | Security | A type's permissions are the stricter of its own and the session's; its deny adds | P0 |
// | SA-08 | Error    | An unknown type is refused, naming the types                             | P1       |
// | SA-09 | Happy    | The agent tool of the main agent takes type and points to action types   | P0       |

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/skills"
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

	typed := func(t *testing.T, types map[string]config.AgentType, options tools.Options, policy *permissions.Policy) team.Builder {
		t.Helper()
		workDir := isolated(t, providerFile(modelServer(t)))
		cfg, _ := config.Load(workDir)
		cfg.Agents.Types = types
		loaded := testExtensions(t, workDir)
		if policy == nil {
			policy, _ = permissions.New(permissions.ModeEdits, nil, nil)
		}
		build, err := subagentBuilder(context.Background(), cfg, chosenProvider{name: "main", model: "big"}, tools.Env{WorkDir: workDir}, options, policy, nil, "BASE", loaded.plugins)
		if err != nil {
			t.Fatal(err)
		}
		return build
	}

	t.Run("SA-04 a type runs on its model and effort, with its prompt and steps", func(t *testing.T) {
		build := typed(t, map[string]config.AgentType{
			"reviewer": {Model: "lab/m1", Effort: "high", Prompt: "Review strictly.", MaxSteps: 7},
		}, tools.Options{}, nil)

		reviewer, err := build(team.Info{ID: 1, Depth: 1, Name: "r", Type: "reviewer"}, nil, nil)
		plain, _ := build(team.Info{ID: 2, Depth: 1, Name: "p"}, nil, nil)

		if err != nil || reviewer.ProviderName != "lab" || reviewer.Model != "m1" || reviewer.Effort != "high" || reviewer.MaxSteps != 7 {
			t.Fatalf("err = %v reviewer = %s/%s effort %q steps %d", err, reviewer.ProviderName, reviewer.Model, reviewer.Effort, reviewer.MaxSteps)
		}
		if !strings.HasSuffix(reviewer.System, "Review strictly.") || plain.Model != "big" || strings.Contains(plain.System, "Review strictly.") {
			t.Errorf("reviewer system %q plain model %q", reviewer.System, plain.Model)
		}
	})

	t.Run("SA-05 a type has only the tools and skills it names, * for many; tell_parent stays", func(t *testing.T) {
		options := tools.Options{
			Extra:  []tools.Tool{{Name: "mcp__gh__issues"}, {Name: "mcp__gh__prs"}, {Name: "mcp__db__query"}},
			Skills: []skills.Skill{{Name: "go-style", Description: "x"}, {Name: "sql", Description: "y"}},
		}
		build := typed(t, map[string]config.AgentType{
			"reader": {Tools: []string{"read", "grep", "mcp__gh__*"}, Skills: []string{"go-*"}},
		}, options, nil)

		reader, _ := build(team.Info{ID: 1, Depth: 1, Name: "r", Type: "reader"}, nil, parentStub{})

		names := toolNamesOf(reader.Tools)
		for _, want := range []string{"read", "grep", "mcp__gh__issues", "mcp__gh__prs", tools.TellParentName, "skill"} {
			if !slices.Contains(names, want) {
				t.Errorf("no %s in %v", want, names)
			}
		}
		for _, unwanted := range []string{"bash", "write", "mcp__db__query"} {
			if slices.Contains(names, unwanted) {
				t.Errorf("%s in %v", unwanted, names)
			}
		}
		var skillsShown string
		for _, spec := range reader.Tools.Specs(nil) {
			if spec.Name == "skill" {
				skillsShown = spec.Description
			}
		}
		if strings.Contains(skillsShown, "sql") || !strings.Contains(skillsShown, "go-style") {
			t.Errorf("skills = %q", skillsShown)
		}
	})

	t.Run("SA-06 a type starts subagents only with can_start, and while the levels allow", func(t *testing.T) {
		build := typed(t, map[string]config.AgentType{"lead": {CanStart: true}, "worker": {}}, tools.Options{}, nil)
		owner := team.New()
		t.Cleanup(owner.Close)

		lead, _ := build(team.Info{ID: 1, Depth: 1, Name: "l", Type: "lead"}, owner.Main(), nil)
		worker, _ := build(team.Info{ID: 2, Depth: 1, Name: "w", Type: "worker"}, owner.Main(), nil)
		deepestLead, _ := build(team.Info{ID: 3, Depth: 2, Name: "d", Type: "lead"}, nil, nil)

		if !slices.Contains(toolNamesOf(lead.Tools), tools.AgentName) || slices.Contains(toolNamesOf(worker.Tools), tools.AgentName) ||
			slices.Contains(toolNamesOf(deepestLead.Tools), tools.AgentName) {
			t.Errorf("lead %v worker %v deepest %v", toolNamesOf(lead.Tools), toolNamesOf(worker.Tools), toolNamesOf(deepestLead.Tools))
		}
		if strings.Contains(worker.System, "subagents of your own") || !strings.Contains(lead.System, "subagents of your own") {
			t.Error("the prompt does not follow can_start")
		}
	})

	t.Run("SA-07 a type's permissions are the stricter of its own and the session's; its deny adds", func(t *testing.T) {
		session, _ := permissions.New(permissions.ModeAuto, []string{"git *"}, []string{"rm *"})
		build := typed(t, map[string]config.AgentType{
			"careful": {Permissions: "ask", Deny: []string{"git push *"}},
			"loose":   {Permissions: "all"},
		}, tools.Options{}, session)

		careful, _ := build(team.Info{ID: 1, Depth: 1, Name: "c", Type: "careful"}, nil, nil)
		loose, _ := build(team.Info{ID: 2, Depth: 1, Name: "l", Type: "loose"}, nil, nil)

		pushed, _ := careful.Permissions.Check("git push origin")
		status, _ := careful.Permissions.Check("git status")
		removed, _ := loose.Permissions.Check("rm -rf x")
		if careful.Permissions.Mode() != permissions.ModeAsk || loose.Permissions.Mode() != permissions.ModeAuto {
			t.Errorf("careful %s loose %s", careful.Permissions.Mode(), loose.Permissions.Mode())
		}
		if pushed != permissions.Denied || status != permissions.Allowed || removed != permissions.Denied {
			t.Errorf("push %v status %v rm %v", pushed, status, removed)
		}
		if p, _ := session.Check("git push origin"); p != permissions.Allowed {
			t.Error("the type's deny reached the session")
		}
	})

	t.Run("SA-08 an unknown type is refused, naming the types", func(t *testing.T) {
		build := typed(t, map[string]config.AgentType{"reviewer": {}, "builder": {}}, tools.Options{}, nil)

		_, err := build(team.Info{ID: 1, Depth: 1, Name: "x", Type: "tester"}, nil, nil)

		if err == nil || !strings.Contains(err.Error(), "builder, reviewer") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("SA-09 the agent tool of the main agent takes type and points to action types", func(t *testing.T) {
		workDir := isolated(t, providerFile(modelServer(t)))
		cfg, _ := config.Load(workDir)
		on := true
		cfg.Agents.Enabled = &on
		cfg.Agents.Types = map[string]config.AgentType{"reviewer": {Description: "reviews a diff"}}
		loaded := testExtensions(t, workDir)
		loaded.team = team.New()
		t.Cleanup(loaded.team.Close)

		session, err := buildSession(context.Background(), workDir, cfg, sessionChoice{}, loaded)

		var spec string
		for _, each := range session.Agent.Tools.Specs(nil) {
			if each.Name == tools.AgentName {
				spec = each.Description + string(each.Schema)
			}
		}
		if err != nil || strings.Contains(spec, "reviews a diff") || !strings.Contains(spec, `{"action":"types"}`) {
			t.Errorf("err %v spec %s", err, spec)
		}
	})
}

type parentStub struct{}

func (parentStub) Tell(string) (string, error) { return "", nil }
