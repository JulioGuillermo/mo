package main

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | BT-01 | Happy    | /btw has the tools its setting names, by default those that read; /btw! all | P0     |
// | BT-02 | Security | An empty list leaves /btw with no tools at all                            | P1       |

import (
	"slices"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func specNames(specs []llm.ToolSpec) []string {
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func TestBtwTools(t *testing.T) {
	env := tools.Env{WorkDir: t.TempDir()}

	t.Run("BT-01 /btw has the tools its setting names, by default those that read; /btw! all", func(t *testing.T) {
		registries := btwTools(env, tools.Options{}, config.Defaults().Tools)

		reading := specNames(registries(false).Specs(nil))
		everything := specNames(registries(true).Specs(nil))

		for _, name := range []string{"read", "grep", "ls", "git"} {
			if !slices.Contains(reading, name) {
				t.Errorf("/btw lacks %s: %v", name, reading)
			}
		}
		for _, name := range []string{"bash", "write", "edit", "rm"} {
			if slices.Contains(reading, name) || !slices.Contains(everything, name) {
				t.Errorf("%s: /btw %v, /btw! %v", name, reading, everything)
			}
		}
	})

	t.Run("BT-02 an empty list leaves /btw with no tools at all", func(t *testing.T) {
		registries := btwTools(env, tools.Options{}, config.Tools{Btw: []string{}})

		if names := specNames(registries(false).Specs(nil)); len(names) != 0 {
			t.Errorf("tools %v", names)
		}
	})
}
