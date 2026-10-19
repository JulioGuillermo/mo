package config

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PS-01 | Happy    | Peers are off by default; incoming asks, outgoing follows the mode, wake on | P0     |
// | PS-02 | Happy    | The user file turns peers on and sets their name and approvals           | P0       |
// | PS-03 | Security | No project file turns peers on or loosens what asks, trusted or not      | P0       |
// | PS-04 | Error    | An approval that is not tools, ask, auto or allow is refused             | P1       |

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPeersSettings(t *testing.T) {
	t.Run("PS-01 peers are off by default; incoming asks, outgoing follows the mode, wake on", func(t *testing.T) {
		isolatedHome(t)

		cfg, err := Load(t.TempDir())

		peers := cfg.Peers
		if err != nil || *peers.Enabled || peers.Incoming != "ask" || peers.Outgoing != "tools" || !*peers.Wake || peers.Name != "" {
			t.Errorf("peers %+v err %v", peers, err)
		}
	})

	t.Run("PS-02 the user file turns peers on and sets their name and approvals", func(t *testing.T) {
		configHome := isolatedHome(t)
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "[peers]\nenabled = true\nname = \"auto\"\nincoming = \"auto\"\noutgoing = \"allow\"\nwake = false\n")

		cfg, err := Load(t.TempDir())

		peers := cfg.Peers
		if err != nil || !*peers.Enabled || peers.Name != "auto" || peers.Incoming != "auto" || peers.Outgoing != "allow" || *peers.Wake {
			t.Errorf("peers %+v err %v", peers, err)
		}
	})

	t.Run("PS-03 no project file turns peers on or loosens what asks, trusted or not", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		layer := Config{Peers: Peers{Enabled: ptr(true), Incoming: "allow"}}
		writeConfig(t, ProjectFile(workDir), "[peers]\nenabled = true\nincoming = \"allow\"\n")

		untrusted, _ := Load(workDir)
		trusted, _ := LoadLayers(workDir, Config{}, layer)

		for _, cfg := range []Config{untrusted, trusted} {
			if *cfg.Peers.Enabled || cfg.Peers.Incoming != "ask" {
				t.Errorf("peers %+v", cfg.Peers)
			}
		}
	})

	t.Run("PS-04 an approval that is not tools, ask, auto or allow is refused", func(t *testing.T) {
		configHome := isolatedHome(t)
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "[peers]\noutgoing = \"sometimes\"\n")

		_, err := Load(t.TempDir())

		if err == nil || !strings.Contains(err.Error(), "peers.outgoing") {
			t.Errorf("err = %v", err)
		}
	})
}
