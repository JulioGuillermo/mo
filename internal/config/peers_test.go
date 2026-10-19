package config

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PS-01 | Happy    | Peers are off by default; incoming asks, outgoing follows the mode, wake on | P0     |
// | PS-02 | Happy    | The user file turns peers on and sets their name and approvals           | P0       |
// | PS-03 | Security | No project file turns peers on or loosens what asks, trusted or not      | P0       |
// | PS-04 | Error    | An approval that is not tools, ask, auto or allow is refused             | P1       |
// | PS-05 | Happy    | The network, port, addresses and secret come from the user file; the secret from a variable too | P0 |
// | PS-06 | Happy    | A folder's secret in the user file applies there alone                   | P0       |
// | PS-07 | Security | No project file sets the network or the secret                           | P0       |
// | PS-08 | Error    | A short secret, a wrong network or port, or an address without a port is refused | P1 |

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

func TestPeersNetworkSettings(t *testing.T) {
	t.Run("PS-05 the network, port, addresses and secret come from the user file; the secret from a variable too", func(t *testing.T) {
		configHome := isolatedHome(t)
		t.Setenv("MO_TEAM_SECRET", "secret-from-the-environment")
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "[peers]\nnetwork = \"visible\"\nport = 7420\naddresses = [\"10.0.0.2:7420\"]\nsecret_env = \"MO_TEAM_SECRET\"\n")

		cfg, err := Load(t.TempDir())

		peers := cfg.Peers
		if err != nil || peers.Network != "visible" || peers.Port != 7420 || len(peers.Addresses) != 1 || peers.Key() != "secret-from-the-environment" {
			t.Errorf("peers %+v err %v", peers, err)
		}
	})

	t.Run("PS-06 a folder's secret in the user file applies there alone", func(t *testing.T) {
		configHome := isolatedHome(t)
		here, elsewhere := t.TempDir(), t.TempDir()
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "[peers]\nsecret = \"the-global-team-secret\"\n[peers.projects.\""+ProjectKey(here)+"\"]\nsecret = \"this-project-team-secret\"\n")

		inside, _ := Load(here)
		outside, _ := Load(elsewhere)

		if inside.Peers.Key() != "this-project-team-secret" || outside.Peers.Key() != "the-global-team-secret" || inside.Peers.Projects != nil {
			t.Errorf("inside %q outside %q", inside.Peers.Key(), outside.Peers.Key())
		}
	})

	t.Run("PS-07 no project file sets the network or the secret", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		layer := Config{Peers: Peers{Network: "visible", Secret: "a-secret-in-the-repo", Addresses: []string{"evil:1"}}}

		cfg, _ := LoadLayers(workDir, Config{}, layer)

		if cfg.Peers.Network != "off" || cfg.Peers.Key() != "" || cfg.Peers.Addresses != nil {
			t.Errorf("peers %+v", cfg.Peers)
		}
	})

	t.Run("PS-08 a short secret, a wrong network or port, or an address without a port is refused", func(t *testing.T) {
		for _, file := range []string{"secret = \"short\"", "network = \"public\"", "port = 70000", "addresses = [\"no-port\"]"} {
			configHome := isolatedHome(t)
			writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "[peers]\n"+file+"\n")

			if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "peers.") {
				t.Errorf("%s: err %v", file, err)
			}
		}
	})
}
