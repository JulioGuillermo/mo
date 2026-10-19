package config

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | RF-01 | Happy    | ReadFiles makes the settings of both files                               | P0       |
// | RF-02 | Error    | A user file that does not parse is a BrokenFile naming it                | P0       |
// | RF-03 | Error    | A wrong value is a BrokenFile naming the file that holds it              | P0       |
// | RF-04 | Happy    | Restore puts back the file as read and keeps the broken one aside        | P0       |
// | RF-05 | Happy    | ResetFile moves the file aside, so the defaults apply                    | P1       |
// | RF-06 | Security | HeldBackKeys names what an untrusted project sets that does not apply    | P0       |
// | RF-07 | Security | Trust makes the file as read apply whole                                 | P0       |
// | RF-08 | Happy    | The watcher reports a change to the user file, and not the same content  | P0       |
// | RF-09 | Happy    | The watcher follows .mo/ once it is created                              | P1       |
// | RF-10 | Happy    | Check reports a change the system did not                                | P1       |

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestReadFiles(t *testing.T) {
	t.Run("RF-01 ReadFiles makes the settings of both files", func(t *testing.T) {
		configHome := isolatedHome(t)
		workDir := t.TempDir()
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "effort = \"high\"\n")
		writeConfig(t, ProjectFile(workDir), "model = \"small\"\n")

		files, err := ReadFiles(workDir)

		if err != nil || files.Settings.Effort != "high" || files.Settings.Model != "small" {
			t.Errorf("effort %q model %q err %v", files.Settings.Effort, files.Settings.Model, err)
		}
	})

	t.Run("RF-02 a user file that does not parse is a BrokenFile naming it", func(t *testing.T) {
		configHome := isolatedHome(t)
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "effort = \n")

		_, err := ReadFiles(t.TempDir())

		var broken *BrokenFile
		if !errors.As(err, &broken) || broken.Path != UserFile() {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("RF-03 a wrong value is a BrokenFile naming the file that holds it", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		writeConfig(t, ProjectFile(workDir), "effort = \"enormous\"\n")

		_, err := ReadFiles(workDir)

		var broken *BrokenFile
		if !errors.As(err, &broken) || broken.Path != ProjectFile(workDir) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("RF-04 Restore puts back the file as read and keeps the broken one aside", func(t *testing.T) {
		configHome := isolatedHome(t)
		path := filepath.Join(configHome, "mo", "config.toml")
		writeConfig(t, path, "# mine\neffort = \"low\"\n")
		files, _ := ReadFiles(t.TempDir())
		writeConfig(t, path, "effort = \n")

		err := files.Restore(path)

		restored, _ := os.ReadFile(path)
		kept, _ := os.ReadFile(path + ".broken")
		if err != nil || string(restored) != "# mine\neffort = \"low\"\n" || string(kept) != "effort = \n" {
			t.Errorf("restored %q kept %q err %v", restored, kept, err)
		}
	})

	t.Run("RF-05 ResetFile moves the file aside, so the defaults apply", func(t *testing.T) {
		configHome := isolatedHome(t)
		path := filepath.Join(configHome, "mo", "config.toml")
		writeConfig(t, path, "effort = \n")

		err := ResetFile(path)

		files, readErr := ReadFiles(t.TempDir())
		if err != nil || readErr != nil || files.Settings.Effort != Defaults().Effort {
			t.Errorf("err %v read %v effort %q", err, readErr, files.Settings.Effort)
		}
	})
}

func TestProjectTrust(t *testing.T) {
	t.Run("RF-06 HeldBackKeys names what an untrusted project sets that does not apply", func(t *testing.T) {
		isolatedHome(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test")
		workDir := t.TempDir()
		writeConfig(t, ProjectFile(workDir), "model = \"small\"\nprovider = \"openrouter\"\n")
		project, _ := ReadProject(workDir)

		held := project.HeldBackKeys(workDir, Config{Provider: "ollama"})

		if !slices.Equal(held, []string{"provider"}) {
			t.Errorf("held = %v", held)
		}
	})

	t.Run("RF-07 Trust makes the file as read apply whole", func(t *testing.T) {
		isolatedHome(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test")
		workDir := t.TempDir()
		writeConfig(t, ProjectFile(workDir), "provider = \"openrouter\"\n")
		project, _ := ReadProject(workDir)

		err := project.Trust()

		files, _ := ReadFiles(workDir)
		if err != nil || !files.Project.Trusted || files.Settings.Provider != "openrouter" {
			t.Errorf("trusted %v provider %q err %v", files.Project.Trusted, files.Settings.Provider, err)
		}
	})
}

func TestWatcher(t *testing.T) {
	t.Run("RF-08 the watcher reports a change to the user file, and not the same content", func(t *testing.T) {
		configHome := isolatedHome(t)
		path := filepath.Join(configHome, "mo", "config.toml")
		writeConfig(t, path, "effort = \"low\"\n")
		watcher, err := Watch(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer watcher.Close()

		writeConfig(t, path, "effort = \"low\"\n")
		if reported(watcher) {
			t.Error("reported the same content")
		}
		writeConfig(t, path, "effort = \"high\"\n")
		if !reported(watcher) {
			t.Error("missed the change")
		}
	})

	t.Run("RF-09 the watcher follows .mo/ once it is created", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		watcher, err := Watch(workDir)
		if err != nil {
			t.Fatal(err)
		}
		defer watcher.Close()

		os.MkdirAll(ProjectDir(workDir), 0o755)
		time.Sleep(50 * time.Millisecond)
		writeConfig(t, ProjectFile(workDir), "model = \"small\"\n")

		if !reported(watcher) {
			t.Error("missed the new project file")
		}
	})

	t.Run("RF-10 Check reports a change the system did not", func(t *testing.T) {
		configHome := isolatedHome(t)
		path := filepath.Join(configHome, "mo", "config.toml")
		watcher := &Watcher{files: []string{path}, changed: make(chan struct{}, 1)}
		watcher.stamp = watcher.read()
		writeConfig(t, path, "effort = \"high\"\n")

		watcher.Check()

		if !reported(watcher) {
			t.Error("Check missed the change")
		}
	})
}

func reported(watcher *Watcher) bool {
	select {
	case <-watcher.Changes():
		return true
	case <-time.After(watchSettle + 300*time.Millisecond):
		return false
	}
}
