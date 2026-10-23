package tui

// Test Traceability Table
// | ID    | Category | Description                                                                   | Priority |
// | CS-01 | Happy    | A look changed on disk applies at once, with no new session                   | P0       |
// | CS-02 | Happy    | Tools changed on disk build a session that keeps provider, model and effort   | P0       |
// | CS-03 | Happy    | An effort changed on disk waits for /reload, and says so                      | P0       |
// | CS-04 | Happy    | /reload builds the session from the files whole                               | P0       |
// | CS-05 | Happy    | The same settings on disk, as this mo saved them, change nothing              | P0       |
// | CS-06 | Happy    | A session built while the model works waits for the turn's end                | P0       |
// | CS-07 | Error    | A broken file opens a red box and keeps the settings in force                 | P0       |
// | CS-08 | Happy    | The box closes once the file is fixed, from here or another mo                | P0       |
// | CS-09 | Happy    | Restoring puts back the last file that worked and applies it                  | P1       |
// | CS-10 | Security | An untrusted project file that asks for more opens a box before applying      | P0       |
// | CS-11 | Security | Ignoring it keeps the project file in force, also after other changes         | P0       |
// | CS-12 | Happy    | While the settings screen is open the change waits                            | P1       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
)

type syncFixture struct {
	user    string
	workDir string
	built   []config.Config
}

func newSyncModel(t *testing.T, userFile string) (model, *syncFixture) {
	t.Helper()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	fixture := &syncFixture{user: filepath.Join(configHome, "mo", "config.toml"), workDir: t.TempDir()}
	fixture.write(t, fixture.user, userFile)
	files, err := config.ReadFiles(fixture.workDir)
	if err != nil {
		t.Fatal(err)
	}
	m := conversationModel()
	m.session.WorkDir = fixture.workDir
	m.session.Config = files.Settings
	m.session.Provider, m.session.Model = files.Settings.Provider, config.EffectiveModel(files.Settings)
	m.session.Rebuild = func(cfg config.Config) (Session, error) {
		fixture.built = append(fixture.built, cfg)
		return Session{Agent: &agent.Agent{}, Config: cfg, WorkDir: fixture.workDir, Provider: cfg.Provider, Model: config.EffectiveModel(cfg)}, nil
	}
	m.config = &configSync{applied: files, answeredProject: files.Project.Digest()}
	return m, fixture
}

func (f *syncFixture) write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// follow hands the model a message and the messages its commands give
// back about settings, as the program would.
func follow(m model, msg tea.Msg) model {
	updated, cmd := m.Update(msg)
	m = updated.(model)
	for _, next := range settingsMsgs(cmd) {
		m = follow(m, next)
	}
	return m
}

func settingsMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(100 * time.Millisecond):
		return nil
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		var found []tea.Msg
		for _, each := range msg {
			found = append(found, settingsMsgs(each)...)
		}
		return found
	case configBuiltMsg, printMsg:
		return []tea.Msg{msg}
	}
	return nil
}

func TestConfigSync(t *testing.T) {
	const start = "provider = \"llama-server\"\nmodel = \"big\"\neffort = \"low\"\n"

	t.Run("CS-01 a look changed on disk applies at once, with no new session", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, start+"[render]\nkeys = \"vim\"\n")

		m = follow(m, configChangedMsg{})

		if m.session.Config.Render.Keys != "vim" || !m.vim.enabled || len(fixture.built) != 0 {
			t.Errorf("keys %q vim %v built %d", m.session.Config.Render.Keys, m.vim.enabled, len(fixture.built))
		}
	})

	t.Run("CS-02 tools changed on disk build a session that keeps provider, model and effort", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "provider = \"llama-server\"\nmodel = \"other\"\neffort = \"high\"\n[tools]\nmax_output = 1234\n")

		m = follow(m, configChangedMsg{})

		if len(fixture.built) != 1 {
			t.Fatalf("built %d", len(fixture.built))
		}
		built := fixture.built[0]
		if built.Tools.MaxOutput != 1234 || built.Model != "big" || built.Effort != "low" || m.session.Config.Tools.MaxOutput != 1234 {
			t.Errorf("max_output %d model %q effort %q", built.Tools.MaxOutput, built.Model, built.Effort)
		}
	})

	t.Run("CS-03 an effort changed on disk waits for /reload, and says so", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "provider = \"llama-server\"\nmodel = \"big\"\neffort = \"high\"\n")

		m = follow(m, configChangedMsg{})

		if len(fixture.built) != 0 || m.session.Config.Effort != "low" || !strings.Contains(shown(m), "/reload") {
			t.Errorf("built %d effort %q shown:\n%s", len(fixture.built), m.session.Config.Effort, shown(m))
		}
	})

	t.Run("CS-04 /reload builds the session from the files whole", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "provider = \"llama-server\"\nmodel = \"other\"\neffort = \"high\"\n")
		m = follow(m, configChangedMsg{})
		m.input.SetValue("/reload")

		m = follow(m, keyEnter)

		if len(fixture.built) != 1 || m.session.Config.Effort != "high" || m.session.Model != "other" {
			t.Errorf("built %d effort %q model %q", len(fixture.built), m.session.Config.Effort, m.session.Model)
		}
	})

	t.Run("CS-05 the same settings on disk, as this mo saved them, change nothing", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "# saved again\n"+start)

		m = follow(m, configChangedMsg{})

		if len(fixture.built) != 0 || strings.Contains(shown(m), "changed on disk") {
			t.Errorf("built %d shown:\n%s", len(fixture.built), shown(m))
		}
	})

	t.Run("CS-06 a session built while the model works waits for the turn's end", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		m.running = true
		fixture.write(t, fixture.user, start+"[tools]\nmax_output = 1234\n")

		m = follow(m, configChangedMsg{})

		if m.pendingSession == nil || m.session.Config.Tools.MaxOutput == 1234 {
			t.Errorf("pending %v max_output %d", m.pendingSession != nil, m.session.Config.Tools.MaxOutput)
		}
	})

	t.Run("CS-07 a broken file opens a red box and keeps the settings in force", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "effort = \n")

		m = follow(m, configChangedMsg{})

		if m.popup == nil || m.popup.border != popupError() || !strings.Contains(m.popup.title, "broken") || m.session.Config.Effort != "low" {
			t.Fatalf("popup %+v effort %q", m.popup, m.session.Config.Effort)
		}
	})

	t.Run("CS-08 the box closes once the file is fixed, from here or another mo", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "effort = \n")
		m = follow(m, configChangedMsg{})
		fixture.write(t, fixture.user, start)

		m = follow(m, configChangedMsg{})

		if m.popup != nil {
			t.Errorf("popup still open: %q", m.popup.title)
		}
	})

	t.Run("CS-09 restoring puts back the last file that worked and applies it", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, fixture.user, "effort = \n")
		m = follow(m, configChangedMsg{})

		updated, _ := m.choosePopup(1)
		m = updated.(model)

		restored, _ := os.ReadFile(fixture.user)
		if _, err := os.Stat(fixture.user + ".broken"); err != nil || string(restored) != start || m.popup != nil {
			t.Errorf("restored %q broken kept: %v popup %v", restored, err, m.popup != nil)
		}
	})

	t.Run("CS-10 an untrusted project file that asks for more opens a box before applying", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, config.ProjectFile(fixture.workDir), "provider = \"openrouter\"\n")

		m = follow(m, configChangedMsg{})

		if m.popup == nil || !strings.Contains(ansi.Strip(m.popup.description), "provider") || len(fixture.built) != 0 {
			t.Fatalf("popup %v built %d", m.popup, len(fixture.built))
		}
		updated, _ := m.choosePopup(0)
		m = updated.(model)
		if project, _ := config.ReadProject(fixture.workDir); !project.Trusted || m.popup != nil {
			t.Errorf("trusted %v popup %v", project.Trusted, m.popup != nil)
		}
	})

	t.Run("CS-11 ignoring it keeps the project file in force, also after other changes", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		fixture.write(t, config.ProjectFile(fixture.workDir), "provider = \"openrouter\"\n[tools]\nmax_output = 99\n")
		m = follow(m, configChangedMsg{})
		updated, _ := m.choosePopup(2)
		m = updated.(model)
		fixture.write(t, fixture.user, start+"[tools]\nmax_output = 1234\n")

		m = follow(m, configChangedMsg{})

		if m.popup != nil || len(fixture.built) != 1 || fixture.built[0].Tools.MaxOutput != 1234 {
			t.Errorf("popup %v built %v", m.popup != nil, fixture.built)
		}
	})

	t.Run("CS-12 while the settings screen is open the change waits", func(t *testing.T) {
		m, fixture := newSyncModel(t, start)
		m.settingsInFlight = true
		fixture.write(t, fixture.user, start+"[tools]\nmax_output = 1234\n")

		updated, cmd := m.Update(configChangedMsg{})

		if len(fixture.built) != 0 || cmd == nil || updated.(model).session.Config.Tools.MaxOutput == 1234 {
			t.Errorf("built %d retry %v", len(fixture.built), cmd != nil)
		}
	})
}
