package tools

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | SL-01 | Happy    | Without skills there are no skill tools at all                              | P0       |
// | SL-02 | Happy    | full lists each skill with its description in skill; compact only names     | P0       |
// | SL-03 | Happy    | search mode adds skill_list and skill_search, declared in any tool mode     | P0       |
// | SL-04 | Happy    | skill loads the instructions, the folder and its other files                | P0       |
// | SL-05 | Error    | An unknown skill is refused with the names there are                        | P1       |
// | SL-06 | Edge     | tool_list and tool_search never list the skill tools; compact keeps skill's list | P1  |

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/skills"
)

func someSkills(t *testing.T) []skills.Skill {
	t.Helper()
	dir := t.TempDir()
	for name, description := range map[string]string{"pdf": "Fill PDF forms", "deploy": "Ship the app to production"} {
		folder := filepath.Join(dir, name)
		os.MkdirAll(folder, 0o755)
		os.WriteFile(filepath.Join(folder, skills.FileName), []byte("---\nname: "+name+"\ndescription: "+description+"\n---\nDo the "+name+" steps.\n"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "pdf", "reference.md"), []byte("x"), 0o644)
	return skills.Find([]string{dir})
}

func specOf(specs []llm.ToolSpec, name string) (llm.ToolSpec, bool) {
	index := slices.IndexFunc(specs, func(spec llm.ToolSpec) bool { return spec.Name == name })
	if index < 0 {
		return llm.ToolSpec{}, false
	}
	return specs[index], true
}

func TestSkillTools(t *testing.T) {
	t.Run("SL-01 without skills there are no skill tools at all", func(t *testing.T) {
		names := specNames(NewRegistry(newEnv(t), Options{SkillMode: ModeSearch}).Specs(nil))

		if slices.ContainsFunc(names, isSkillTool) {
			t.Errorf("names = %v", names)
		}
	})

	t.Run("SL-02 full lists each skill with its description in skill; compact only names", func(t *testing.T) {
		found := someSkills(t)
		full, _ := specOf(NewRegistry(newEnv(t), Options{Skills: found, SkillMode: ModeFull}).Specs(nil), skillToolName)
		compact, _ := specOf(NewRegistry(newEnv(t), Options{Skills: found, SkillMode: ModeCompact}).Specs(nil), skillToolName)

		if !strings.Contains(full.Description, "- deploy: Ship the app to production") || !strings.Contains(full.Description, "- pdf: Fill PDF forms") {
			t.Errorf("full:\n%s", full.Description)
		}
		if !strings.Contains(compact.Description, "Skills: deploy, pdf") || strings.Contains(compact.Description, "Fill PDF") {
			t.Errorf("compact:\n%s", compact.Description)
		}
	})

	t.Run("SL-03 search mode adds skill_list and skill_search, declared in any tool mode", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Mode: ModeSearch, Skills: someSkills(t), SkillMode: ModeSearch})
		names := specNames(registry.Specs(nil))

		found, _ := registry.Run(context.Background(), llm.ToolCall{Name: skillSearchToolName, Args: args(t, map[string]any{"query": "production"})})
		listed, _ := registry.Run(context.Background(), llm.ToolCall{Name: skillListToolName, Args: args(t, map[string]any{})})

		for _, name := range []string{skillToolName, skillListToolName, skillSearchToolName} {
			if !slices.Contains(names, name) {
				t.Errorf("%s not declared: %v", name, names)
			}
		}
		if !strings.HasPrefix(found, "deploy: Ship the app") || strings.Contains(found, "pdf") || listed != "deploy, pdf" {
			t.Errorf("search %q list %q", found, listed)
		}
	})

	t.Run("SL-04 skill loads the instructions, the folder and its other files", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Skills: someSkills(t)})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: skillToolName, Args: args(t, map[string]any{"name": "pdf"})})

		if isError || !strings.Contains(output, "Do the pdf steps.") || !strings.Contains(output, "reference.md") || strings.Contains(output, "description:") {
			t.Errorf("output:\n%s", output)
		}
	})

	t.Run("SL-05 an unknown skill is refused with the names there are", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Skills: someSkills(t)})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: skillToolName, Args: args(t, map[string]any{"name": "fax"})})

		if !isError || !strings.Contains(output, "deploy, pdf") {
			t.Errorf("output %q error %v", output, isError)
		}
	})

	t.Run("SL-06 tool_list and tool_search never list the skill tools; compact keeps skill's list", func(t *testing.T) {
		search := NewRegistry(newEnv(t), Options{Mode: ModeSearch, Skills: someSkills(t)})
		compact := NewRegistry(newEnv(t), Options{Mode: ModeCompact, Skills: someSkills(t)})

		listed := search.list("")
		found := runSearch(t, search, "skill")
		spec, _ := specOf(compact.Specs(nil), skillToolName)

		if strings.Contains(listed, "skill") || strings.Contains(found, "skill:") || !strings.Contains(spec.Description, "pdf") {
			t.Errorf("list %q search %q compact %q", listed, found, spec.Description)
		}
	})
}
