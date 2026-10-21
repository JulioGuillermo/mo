package main

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SD-01 | Happy    | Only the servers not in full mode, by their own or the default, keep their description | P0 |

import (
	"maps"
	"slices"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/tools"
)

func TestServerDescriptions(t *testing.T) {
	t.Run("SD-01 only the servers not in full mode, by their own or the default, keep their description", func(t *testing.T) {
		descriptions := map[string]string{"grouped": "g", "full": "f", "default": "d"}
		settings := config.Tools{Mode: tools.ModeCompact, MCPModes: map[string]tools.Mode{"grouped": tools.ModeGroup, "full": tools.ModeFull}}
		allFull := config.Tools{Mode: tools.ModeFull}

		kept := notFullServers(descriptions, settings)
		none := notFullServers(descriptions, allFull)

		if names := slices.Sorted(maps.Keys(kept)); !slices.Equal(names, []string{"default", "grouped"}) || len(none) != 0 {
			t.Errorf("kept = %q with all full = %q", names, none)
		}
	})
}
