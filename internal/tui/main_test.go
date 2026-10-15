package tui

import (
	"os"
	"testing"
)

// TestMain keeps every test of the package away from the user's own
// folders: a test that saves a conversation or settings writes to
// temporary ones.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "mo-tui-test")
	if err != nil {
		panic(err)
	}
	for name, dir := range map[string]string{"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
		os.Setenv(name, home+"/"+dir)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
