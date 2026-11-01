//go:build !windows && !darwin

package speaker

// Test Traceability Table
// | ID    | Category | Description                                                         | Priority |
// | SP-02 | Happy    | Recordings play one after another, each once, through the player found | P0     |
// | SP-03 | Happy    | Stop ends the one playing and drops the ones waiting                 | P0       |
// | SP-04 | Error    | Without a player, Play says which to install                         | P1       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakePlayer is a pw-play that logs the first bytes of what it plays and
// takes pause seconds.
func fakePlayer(t *testing.T, pause string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "played")
	script := "#!/bin/sh\ntail -c 2 \"$1\" >> " + log + "\nsleep " + pause + "\n"
	if err := os.WriteFile(filepath.Join(bin, "pw-play"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return log
}

func waitIdle(t *testing.T, player *Player) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for player.Playing() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPlayer(t *testing.T) {
	t.Run("SP-02 recordings play one after another, each once, through the player found", func(t *testing.T) {
		log := fakePlayer(t, "0.1")
		var player Player

		for _, pcm := range []string{"a1", "b2", "c3"} {
			if err := player.Play(WAV([]byte(pcm), 8000)); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(50 * time.Millisecond)
		waitIdle(t, &player)

		if played, _ := os.ReadFile(log); string(played) != "a1b2c3" {
			t.Errorf("played %q", played)
		}
	})

	t.Run("SP-03 Stop ends the one playing and drops the ones waiting", func(t *testing.T) {
		log := fakePlayer(t, "5")
		var player Player
		player.Play(WAV([]byte("a1"), 8000))
		player.Play(WAV([]byte("b2"), 8000))
		time.Sleep(100 * time.Millisecond)

		started := time.Now()
		player.Stop()
		waitIdle(t, &player)

		if played, _ := os.ReadFile(log); string(played) != "a1" || time.Since(started) > 2*time.Second {
			t.Errorf("played %q, stopped in %v", played, time.Since(started))
		}
	})

	t.Run("SP-04 without a player, Play says which to install", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		var player Player

		err := player.Play(WAV(nil, 8000))

		if err == nil || !strings.Contains(err.Error(), "pw-play") {
			t.Errorf("err = %v", err)
		}
	})
}
