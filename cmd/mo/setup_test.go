package main

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | ST-01 | Happy    | A provider that answers starts without the setup screen            | P0       |
// | ST-02 | Happy    | A provider that does not answer opens setup; a working pick is saved | P0     |
// | ST-03 | Happy    | Quitting the setup ends mo and saves nothing                       | P1       |
// | ST-04 | Happy    | Going on anyway keeps the built session and saves nothing          | P1       |
// | ST-05 | Error    | A pick that does not work comes back with its own error            | P1       |
// | ST-06 | Happy    | With nothing configured or found, setup opens with no way to go on | P1       |
// | ST-07 | Edge     | A server without a model list still counts as answering            | P1       |
// | ST-08 | Error    | A setting that is not about the provider fails as before, no setup | P1       |
// | ST-09 | Edge     | A built pick that does not answer is what going on keeps and saves | P1       |
// | ST-10 | Happy    | A changed MCP server connects on reload, in use only once activated | P0      |
// | ST-11 | Edge     | A hub that arrives after the run closed its hubs is closed too     | P1       |

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/plugins"
	"github.com/jg/mo/internal/tui"
)

func modelServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

func deadURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	return server.URL + "/v1"
}

// isolated points every config, data and login path into temporary folders
// and writes the user file.
func isolated(t *testing.T, userFile string) string {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "CODEX_HOME"} {
		t.Setenv(name, filepath.Join(home, strings.ToLower(name)))
	}
	if userFile != "" {
		path := config.UserFile()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(userFile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return t.TempDir()
}

func testExtensions(t *testing.T, workDir string) extensions {
	t.Helper()
	hub := mcphub.Connect(context.Background(), nil, "")
	loaded := plugins.Load(t.TempDir(), workDir, t.TempDir())
	t.Cleanup(loaded.Close)
	hubs := &openHubs{current: hub}
	t.Cleanup(hubs.Close)
	return extensions{mcp: hub, plugins: loaded, hubs: hubs}
}

func start(t *testing.T, workDir string, ask askProvider) (tui.Session, error) {
	t.Helper()
	cfg, err := config.Load(workDir)
	if err != nil {
		t.Fatal(err)
	}
	return startSession(context.Background(), workDir, cfg, sessionChoice{}, testExtensions(t, workDir), ask)
}

func userFileText(t *testing.T) string {
	t.Helper()
	data, _ := os.ReadFile(config.UserFile())
	return string(data)
}

func providerFile(url string) string {
	return "provider = \"lab\"\n[providers.lab]\ntype = \"openai\"\nbase_url = \"" + url + "\"\nmodel = \"m1\"\n"
}

func TestStartSession(t *testing.T) {
	t.Run("ST-01 a provider that answers starts without the setup screen", func(t *testing.T) {
		workDir := isolated(t, providerFile(modelServer(t)))
		asked := false

		session, err := start(t, workDir, func(context.Context, tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			asked = true
			return config.Config{}, tui.SetupQuit, nil
		})

		if err != nil || asked || session.Provider != "lab" {
			t.Errorf("err = %v asked = %v provider = %q", err, asked, session.Provider)
		}
	})

	t.Run("ST-02 a provider that does not answer opens setup; a working pick is saved", func(t *testing.T) {
		workDir := isolated(t, providerFile(deadURL(t)))
		working := modelServer(t)
		var problem tui.ProviderProblem

		session, err := start(t, workDir, func(_ context.Context, shown tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			problem = shown
			layer := shown.Layer
			layer.Providers = map[string]config.ProviderConfig{"lab": {Protocol: "openai", BaseURL: working, Model: "m1"}}
			return layer, tui.SetupUse, nil
		})

		if err != nil || session.Provider != "lab" || !problem.CanContinue || !strings.Contains(problem.Err.Error(), "does not answer") {
			t.Fatalf("err = %v provider = %q problem = %+v", err, session.Provider, problem)
		}
		if !strings.Contains(userFileText(t), working) {
			t.Errorf("file = %q", userFileText(t))
		}
	})

	t.Run("ST-03 quitting the setup ends mo and saves nothing", func(t *testing.T) {
		before := providerFile(deadURL(t))
		workDir := isolated(t, before)

		_, err := start(t, workDir, func(context.Context, tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			return config.Config{}, tui.SetupQuit, nil
		})

		if !errors.Is(err, errNoProvider) || userFileText(t) != before {
			t.Errorf("err = %v file = %q", err, userFileText(t))
		}
	})

	t.Run("ST-04 going on anyway keeps the built session and saves nothing", func(t *testing.T) {
		before := providerFile(deadURL(t))
		workDir := isolated(t, before)

		session, err := start(t, workDir, func(context.Context, tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			return config.Config{}, tui.SetupContinue, nil
		})

		if err != nil || session.Provider != "lab" || userFileText(t) != before {
			t.Errorf("err = %v provider = %q file = %q", err, session.Provider, userFileText(t))
		}
	})

	t.Run("ST-05 a pick that does not work comes back with its own error", func(t *testing.T) {
		workDir := isolated(t, providerFile(deadURL(t)))
		var errs []string

		_, _ = start(t, workDir, func(_ context.Context, shown tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			errs = append(errs, shown.Err.Error())
			if len(errs) == 2 {
				return config.Config{}, tui.SetupQuit, nil
			}
			layer := shown.Layer
			layer.Provider = "nowhere"
			return layer, tui.SetupUse, nil
		})

		if len(errs) != 2 || !strings.Contains(errs[1], "nowhere") {
			t.Errorf("errors = %q", errs)
		}
	})

	t.Run("ST-06 with nothing configured or found, setup opens with no way to go on", func(t *testing.T) {
		workDir := isolated(t, "[providers.llama-server]\nbase_url = \""+deadURL(t)+"\"\n[providers.ollama]\nbase_url = \""+deadURL(t)+"\"\n[providers.lmstudio]\nbase_url = \""+deadURL(t)+"\"\n")
		var problem tui.ProviderProblem

		_, err := start(t, workDir, func(_ context.Context, shown tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			problem = shown
			return config.Config{}, tui.SetupQuit, nil
		})

		if !errors.Is(err, errNoProvider) || problem.CanContinue || !strings.Contains(problem.Err.Error(), "no local server found") {
			t.Errorf("err = %v problem = %+v", err, problem)
		}
	})
}

func TestStartSessionEdges(t *testing.T) {
	never := func(context.Context, tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
		return config.Config{}, tui.SetupQuit, nil
	}

	t.Run("ST-07 a server without a model list still counts as answering", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)
		workDir := isolated(t, providerFile(server.URL+"/v1"))

		session, err := start(t, workDir, never)

		if err != nil || session.Provider != "lab" {
			t.Errorf("err = %v provider = %q", err, session.Provider)
		}
	})

	t.Run("ST-08 a setting that is not about the provider fails as before, no setup", func(t *testing.T) {
		workDir := isolated(t, providerFile(modelServer(t))+"[web]\nsearch = \"searxng\"\n")
		asked := false

		_, err := start(t, workDir, func(context.Context, tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			asked = true
			return config.Config{}, tui.SetupQuit, nil
		})

		if err == nil || asked || !strings.Contains(err.Error(), "searxng") {
			t.Errorf("err = %v asked = %v", err, asked)
		}
	})

	t.Run("ST-09 a built pick that does not answer is what going on keeps and saves", func(t *testing.T) {
		workDir := isolated(t, "")
		dead := deadURL(t)
		rounds := 0

		session, err := start(t, workDir, func(_ context.Context, shown tui.ProviderProblem) (config.Config, tui.SetupOutcome, error) {
			rounds++
			if rounds == 2 {
				return shown.Layer, tui.SetupContinue, nil
			}
			layer := shown.Layer
			layer.Provider = "down"
			layer.Providers = map[string]config.ProviderConfig{"down": {Protocol: "openai", BaseURL: dead, Model: "m1"}}
			return layer, tui.SetupUse, nil
		})

		if err != nil || session.Provider != "down" || !strings.Contains(userFileText(t), dead) {
			t.Errorf("err = %v provider = %q file = %q", err, session.Provider, userFileText(t))
		}
	})
}

func TestMCPReload(t *testing.T) {
	t.Run("ST-10 a changed MCP server connects on reload, in use only once activated", func(t *testing.T) {
		workDir := isolated(t, providerFile(modelServer(t)))
		cfg, _ := config.Load(workDir)
		loaded := testExtensions(t, workDir)
		session, err := buildSession(context.Background(), workDir, cfg, sessionChoice{}, loaded)
		if err != nil {
			t.Fatal(err)
		}
		layer, _ := config.ReadUserLayer()
		layer.MCP = map[string]config.MCPServer{"broken": {Command: "/nonexistent/server"}}

		fresh, err := session.Reload(layer)
		before := loaded.hubs.current
		fresh.Activate()

		if err != nil || len(fresh.MCP) != 1 || fresh.MCP[0].Name != "broken" || before != loaded.mcp || loaded.hubs.current == loaded.mcp {
			t.Errorf("err = %v status = %+v", err, fresh.MCP)
		}
	})

	t.Run("ST-11 a hub that arrives after the run closed its hubs is closed too", func(t *testing.T) {
		first := mcphub.Connect(context.Background(), nil, "")
		hubs := &openHubs{current: first}
		hubs.Close()

		hubs.replace(mcphub.Connect(context.Background(), nil, ""))

		if hubs.current != first || !hubs.closed {
			t.Errorf("current replaced after close")
		}
	})
}
