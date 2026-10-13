package config

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | PJ-01 | Happy    | .mo/settings.toml is read, and wins over the old .mo.toml                  | P0       |
// | PJ-02 | Security | A file saved from mo picks a paid provider; once edited it no longer can   | P0       |
// | PJ-03 | Security | A file that came with the repository is not trusted, wherever it is        | P0       |
// | PJ-04 | Happy    | Overrides write the keys set, clear the rest and keep what else the file has | P0     |
// | PJ-05 | Happy    | Saving a project layer that sets nothing removes the file and .mo/         | P1       |
// | PJ-06 | Happy    | LoadLayers applies the project layer given as trusted                      | P1       |
// | PJ-07 | Happy    | ProjectSets counts a provider's model as the model                         | P2       |

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProjectFile(t *testing.T) {
	t.Run("PJ-01 .mo/settings.toml is read, and wins over the old .mo.toml", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		writeConfig(t, LegacyProjectFile(workDir), "model = \"old\"\n")
		before, _ := Load(workDir)
		writeConfig(t, ProjectFile(workDir), "model = \"new\"\n")

		after, err := Load(workDir)

		if err != nil || before.Model != "old" || after.Model != "new" {
			t.Errorf("before %q after %q err %v", before.Model, after.Model, err)
		}
	})

	t.Run("PJ-02 a file saved from mo picks a paid provider; once edited it no longer can", func(t *testing.T) {
		configHome := isolatedHome(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test")
		writeConfig(t, filepath.Join(configHome, "mo", "config.toml"), "provider = \"ollama\"\n")
		workDir := t.TempDir()
		if err := SaveProject(workDir, Config{Provider: "openrouter", Tools: Tools{Confirm: []string{}}}); err != nil {
			t.Fatal(err)
		}

		saved, err := Load(workDir)
		if err != nil || saved.Provider != "openrouter" || len(saved.Tools.Confirm) != 0 {
			t.Fatalf("saved: provider %q confirm %v err %v", saved.Provider, saved.Tools.Confirm, err)
		}
		project, _ := ReadProject(workDir)
		if !project.Trusted {
			t.Error("the saved file is not trusted")
		}
		data, _ := os.ReadFile(ProjectFile(workDir))
		writeConfig(t, ProjectFile(workDir), string(data)+"# edited\n")
		edited, _ := Load(workDir)

		if edited.Provider != "ollama" || !slices.Contains(edited.Tools.Confirm, "bash") {
			t.Errorf("edited: provider %q confirm %v", edited.Provider, edited.Tools.Confirm)
		}
	})

	t.Run("PJ-03 a file that came with the repository is not trusted, wherever it is", func(t *testing.T) {
		isolatedHome(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test")
		mine := t.TempDir()
		cloned := t.TempDir()
		if err := SaveProject(mine, Config{Provider: "openrouter"}); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(ProjectFile(mine))
		writeConfig(t, ProjectFile(cloned), string(data))

		project, _ := ReadProject(cloned)
		cfg, _ := Load(cloned)

		if project.Trusted || cfg.Provider == "openrouter" {
			t.Errorf("trusted %v provider %q", project.Trusted, cfg.Provider)
		}
	})

	t.Run("PJ-04 overrides write the keys set, clear the rest and keep what else the file has", func(t *testing.T) {
		into := Config{Model: "stale", Render: Render{Sidebar: "off"}, Tools: Tools{Mode: "search"},
			Providers: map[string]ProviderConfig{"ollama": {Model: "qwen"}}}
		target := Defaults()
		target.Provider = "ollama"
		target.Providers["ollama"] = ProviderConfig{Model: "gemma"}
		target.Tools.Disabled = nil
		set := map[string]bool{"provider": true, "model": true, "tools.disabled": true, "loop.warnings": true}

		layer := ProjectOverrides(target, set, into)

		switch {
		case layer.Provider != "ollama" || layer.Model != "gemma" || layer.Providers != nil:
			t.Errorf("provider %q model %q providers %v", layer.Provider, layer.Model, layer.Providers)
		case layer.Tools.Disabled == nil || len(layer.Tools.Disabled) != 0:
			t.Errorf("disabled = %#v, want an empty list", layer.Tools.Disabled)
		case layer.Tools.Mode != "" || layer.Render.Sidebar != "off":
			t.Errorf("mode %q sidebar %q", layer.Tools.Mode, layer.Render.Sidebar)
		case layer.Loop.Warnings == nil || *layer.Loop.Warnings != 2 || layer.Loop.Warnings == target.Loop.Warnings:
			t.Errorf("warnings = %v, want a copy of 2", layer.Loop.Warnings)
		}
	})

	t.Run("PJ-05 saving a project layer that sets nothing removes the file and .mo/", func(t *testing.T) {
		isolatedHome(t)
		workDir := t.TempDir()
		if err := SaveProject(workDir, Config{Model: "gemma"}); err != nil {
			t.Fatal(err)
		}

		err := SaveProject(workDir, Config{})

		if _, statErr := os.Stat(ProjectDir(workDir)); err != nil || !os.IsNotExist(statErr) {
			t.Errorf("err %v, .mo still there: %v", err, statErr)
		}
	})

	t.Run("PJ-06 LoadLayers applies the project layer given as trusted", func(t *testing.T) {
		isolatedHome(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test")

		cfg, err := LoadLayers(t.TempDir(), Config{Provider: "ollama"}, Config{Provider: "openrouter", Model: "m"})

		if err != nil || cfg.Provider != "openrouter" || cfg.Model != "m" {
			t.Errorf("provider %q model %q err %v", cfg.Provider, cfg.Model, err)
		}
	})

	t.Run("PJ-07 ProjectSets counts a provider's model as the model", func(t *testing.T) {
		set := ProjectSets(Config{Providers: map[string]ProviderConfig{"ollama": {Model: "qwen"}}, Tools: Tools{Confirm: []string{}}})

		if !set["model"] || !set["tools.confirm"] || set["provider"] || len(set) != 2 {
			t.Errorf("set = %v", set)
		}
	})
}
