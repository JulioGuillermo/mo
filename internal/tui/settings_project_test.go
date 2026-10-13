package tui

// Test Traceability Table
// | ID    | Category | Description                                                                   | Priority |
// | SP-01 | Happy    | Tab switches between Global and This project, which shows only what a project sets | P0  |
// | SP-02 | Happy    | A setting changed in the project is marked and saved alone to .mo/settings.toml  | P0       |
// | SP-03 | Happy    | What the project does not set follows the global value, also as it is edited     | P0       |
// | SP-04 | Happy    | "Use the global value" drops the key and keeps the rest of the file              | P0       |
// | SP-05 | Security | Saving global settings leaves a project file not opened untouched and untrusted  | P0       |
// | SP-06 | Security | mo warns at start when a project file it does not trust is held back             | P1       |
// | SP-07 | Happy    | The project's permissions are kept in the user file for this folder              | P1       |
// | SP-08 | Happy    | /settings project opens the project screen                                       | P2       |
// | SP-09 | Happy    | The project turns an MCP server off for itself, saved as tools.mcp_disabled      | P0       |
// | SP-10 | Happy    | The Skills section lists what was found, and says how to add skills when none    | P1       |

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
)

// projectMenuModel opens the settings of a project whose file holds
// project, with the user file holding user, in an isolated home.
func projectMenuModel(t *testing.T, user, project string) (model, *settingsMenu, *[]*config.Config) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	workDir := t.TempDir()
	if user != "" {
		writeFile(t, config.UserFile(), user)
	}
	if project != "" {
		writeFile(t, config.ProjectFile(workDir), project)
	}
	var reloaded []*config.Config
	session := Session{Agent: &agent.Agent{}, Provider: "llama-server", Config: config.Defaults(), WorkDir: workDir,
		Reload: func(_ config.Config, project *config.Config) (Session, error) {
			reloaded = append(reloaded, project)
			return Session{Agent: &agent.Agent{}, Provider: "llama-server", Config: config.Defaults(), WorkDir: workDir}, nil
		}}
	m := model{session: session, printer: newPrinter(), input: newInput(), width: 100, height: 40}
	layer, err := config.ReadUserLayer()
	if err != nil {
		t.Fatal(err)
	}
	file, err := config.ReadProject(workDir)
	menu := m.newSettingsMenu(settingsLoadedMsg{layer: layer, project: file, projectErr: err})
	updated, _ := m.showMenu(menu)
	return updated.(model), menu, &reloaded
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// save picks save and runs what it starts.
func save(t *testing.T, m model, menu *settingsMenu) settingsSavedMsg {
	t.Helper()
	_, cmd := m.openMenuChoice(menu, menuSave)
	if cmd == nil {
		t.Fatal("save started nothing")
	}
	saved, ok := cmd().(settingsSavedMsg)
	if !ok || saved.err != nil {
		t.Fatalf("saved = %#v", saved)
	}
	return saved
}

func TestProjectSettings(t *testing.T) {
	t.Run("SP-01 tab switches between Global and This project, which shows only what a project sets", func(t *testing.T) {
		m, _, _ := projectMenuModel(t, "", "")
		global := m.settings.form.View()

		switched, _ := m.Update(keyTab)
		project := switched.(model).settings.form.View()
		back, _ := switched.(model).Update(keyTab)

		if !strings.Contains(global, "Web search") || !strings.Contains(global, "This project ›") {
			t.Errorf("global:\n%s", global)
		}
		if strings.Contains(project, "Web search") || strings.Contains(project, "MCP servers") || !strings.Contains(project, "Loop guard") || !strings.Contains(project, ".mo/settings.toml") {
			t.Errorf("project:\n%s", project)
		}
		if !strings.Contains(back.(model).settings.form.View(), "Web search") {
			t.Error("tab did not come back to Global")
		}
	})

	t.Run("SP-02 a setting changed in the project is marked and saved alone to .mo/settings.toml", func(t *testing.T) {
		m, menu, reloaded := projectMenuModel(t, "[render]\nsidebar = \"off\"\n", "")
		switched, _ := m.switchScope(menu)
		section := projectSections()[3]
		items := overridable(section.items(switched.(model), menu))
		opened, _ := switched.(model).editItem(menu, section, items[1])
		menu.project.draft.loopRepeats = "7"
		listed, _ := opened.(model).settings.next(opened.(model))
		userBefore, _ := os.ReadFile(config.UserFile())

		saved := save(t, listed.(model), menu)

		data, _ := os.ReadFile(config.ProjectFile(m.session.WorkDir))
		userAfter, _ := os.ReadFile(config.UserFile())
		if !strings.Contains(listed.(model).settings.form.View(), "● this project") {
			t.Errorf("not marked:\n%s", listed.(model).settings.form.View())
		}
		if strings.Join(strings.Fields(string(data)), " ") != "[loop] repeats = 7" {
			t.Errorf("project file:\n%s", data)
		}
		if string(userBefore) != string(userAfter) || len(saved.saved) != 1 || len(*reloaded) != 1 || (*reloaded)[0] == nil {
			t.Errorf("user file changed or not reloaded with the project: %v", saved.saved)
		}
		if project, _ := config.ReadProject(m.session.WorkDir); !project.Trusted {
			t.Error("the saved file is not trusted")
		}
	})

	t.Run("SP-03 what the project does not set follows the global value, also as it is edited", func(t *testing.T) {
		m, menu, _ := projectMenuModel(t, "", "[loop]\nreplies = 4\n")
		menu.draft.loopRepeats = "5"
		menu.draft.loopReplies = "9"
		switched, _ := m.switchScope(menu)
		listed, _ := switched.(model).openMenuChoice(menu, "loop")
		view := listed.(model).settings.form.View()

		layer, err := menu.projectLayer()

		if err != nil || layer.Loop.Replies != 4 || layer.Loop.Repeats != 0 {
			t.Errorf("layer loop = %+v err %v", layer.Loop, err)
		}
		if !strings.Contains(view, "5") || !strings.Contains(view, "· global") || !strings.Contains(view, "4") {
			t.Errorf("section:\n%s", view)
		}
	})

	t.Run("SP-04 use the global value drops the key and keeps the rest of the file", func(t *testing.T) {
		m, menu, _ := projectMenuModel(t, "", "model = \"gemma\"\n[render]\nsidebar = \"off\"\n[loop]\nreplies = 4\n")
		switched, _ := m.switchScope(menu)
		section := projectSections()[3]
		items := overridable(section.items(switched.(model), menu))
		asked, _ := switched.(model).chooseOverride(menu, section, items[2])
		if !strings.Contains(asked.(model).settings.form.View(), "Use the global value") {
			t.Fatalf("asked:\n%s", asked.(model).settings.form.View())
		}
		down, _ := asked.(model).Update(keyDown)
		picked, cmd := down.(model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		listed := deliver(picked.(model), cmd, 5)
		if menu.project.set["loop.replies"] || !strings.Contains(listed.settings.form.View(), "This project › Loop guard") {
			t.Fatalf("still set, or not back in the section:\n%s", listed.settings.form.View())
		}

		save(t, listed, menu)

		data, _ := os.ReadFile(config.ProjectFile(m.session.WorkDir))
		if strings.Contains(string(data), "replies") || !strings.Contains(string(data), "sidebar = \"off\"") || !strings.Contains(string(data), "gemma") {
			t.Errorf("project file:\n%s", data)
		}
	})

	t.Run("SP-05 saving global settings leaves a project file not opened untouched and untrusted", func(t *testing.T) {
		m, menu, reloaded := projectMenuModel(t, "", "provider = \"openrouter\"\n")
		menu.draft.loopRepeats = "6"

		save(t, m, menu)

		data, _ := os.ReadFile(config.ProjectFile(m.session.WorkDir))
		project, _ := config.ReadProject(m.session.WorkDir)
		if string(data) != "provider = \"openrouter\"\n" || project.Trusted || (*reloaded)[0] != nil {
			t.Errorf("file %q trusted %v", data, project.Trusted)
		}
	})

	t.Run("SP-06 mo warns at start when a project file it does not trust is held back", func(t *testing.T) {
		m, _, _ := projectMenuModel(t, "provider = \"ollama\"\n", "provider = \"openrouter\"\n")
		t.Setenv("OPENROUTER_API_KEY", "sk-test")
		user, _ := config.ReadUserLayer()
		harmless := t.TempDir()
		writeFile(t, config.ProjectFile(harmless), "[loop]\nreplies = 4\n")

		if projectNotice(m.session.WorkDir, user) == "" || projectNotice(harmless, user) != "" {
			t.Error("the notice is missing for a held-back file or shown for a harmless one")
		}
	})

	t.Run("SP-07 the project's permissions are kept in the user file for this folder", func(t *testing.T) {
		m, menu, _ := projectMenuModel(t, "", "")
		switched, _ := m.switchScope(menu)
		listed, _ := switched.(model).openMenuChoice(menu, projectPermissions)
		menu.draft.projectAllow = "make *"

		save(t, listed.(model), menu)

		layer, _ := config.ReadUserLayer()
		if rules := layer.Permissions.Projects[config.ProjectKey(m.session.WorkDir)]; len(rules.Allow) != 1 || rules.Allow[0] != "make *" {
			t.Errorf("rules = %+v", layer.Permissions.Projects)
		}
		if _, err := os.Stat(config.ProjectFile(m.session.WorkDir)); !os.IsNotExist(err) {
			t.Error("the rules went to the project file")
		}
	})

	t.Run("SP-08 /settings project opens the project screen", func(t *testing.T) {
		m, _, _ := projectMenuModel(t, "", "")
		m.settings = nil
		m.openSection = menuProject
		layer, _ := config.ReadUserLayer()

		opened, _ := m.openSettingsMenu(settingsLoadedMsg{layer: layer})

		if !strings.Contains(opened.(model).settings.form.View(), ".mo/settings.toml · what is set here wins") {
			t.Errorf("view:\n%s", opened.(model).settings.form.View())
		}
	})

	t.Run("SP-09 the project turns an MCP server off for itself, saved as tools.mcp_disabled", func(t *testing.T) {
		m, menu, _ := projectMenuModel(t, "[mcp.docs]\nurl = \"https://example.com/mcp\"\n", "")
		switched, _ := m.switchScope(menu)
		section := projectSections()[1]
		items := overridable(section.items(switched.(model), menu))
		index := slices.IndexFunc(items, func(item settingItem) bool { return item.title == "MCP servers off" })
		if index < 0 {
			t.Fatalf("no MCP item in %v", items)
		}
		opened, _ := switched.(model).editItem(menu, section, items[index])
		menu.project.draft.mcpDisabled = []string{"docs"}
		listed, _ := opened.(model).settings.next(opened.(model))

		save(t, listed.(model), menu)

		project, _ := config.ReadProject(m.session.WorkDir)
		user, _ := config.ReadUserLayer()
		if !slices.Equal(project.Layer.Tools.MCPDisabled, []string{"docs"}) || user.Tools.MCPDisabled != nil {
			t.Errorf("project %v user %v", project.Layer.Tools.MCPDisabled, user.Tools.MCPDisabled)
		}
	})

	t.Run("SP-10 the Skills section lists what was found, and says how to add skills when none", func(t *testing.T) {
		m, menu, _ := projectMenuModel(t, "", "")
		empty, _ := m.openMenuChoice(menu, "skills")
		m.session.Skills = []string{"pdf", "deploy"}
		found, _ := m.openMenuChoice(menu, "skills")

		if view := empty.(model).settings.form.View(); !strings.Contains(view, "No skills found") || strings.Contains(view, "Skills off") {
			t.Errorf("empty:\n%s", view)
		}
		if view := found.(model).settings.form.View(); !strings.Contains(view, "2 found: pdf, deploy") || !strings.Contains(view, "Skills off") {
			t.Errorf("found:\n%s", view)
		}
	})
}
