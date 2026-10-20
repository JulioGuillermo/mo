package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | MP-01 | Happy    | /model lists the provider's models, the one in use marked; the pick is saved | P0       |
// | MP-02 | Happy    | When the project sets the model, the pick goes to the project's file         | P0       |
// | MP-03 | Error    | A provider that cannot list its models asks for the name instead             | P1       |
// | MP-04 | Happy    | The Model setting picks from the list too, for the provider the form has     | P1       |
// | MP-05 | Happy    | Subagents' provider lists the providers set up; another one goes on to its models | P0  |
// | MP-06 | Happy    | Subagents' model alone lists the main agent's provider's models and takes it too | P1   |
// | MP-07 | Happy    | A type's Provider and Model are picked from lists the same way                | P0       |
// | MP-08 | Error    | A provider left without a model is not saved, and says to pick one            | P1       |

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
)

func modelServer(t *testing.T, models ...string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		var data []string
		for _, model := range models {
			data = append(data, fmt.Sprintf(`{"id":%q}`, model))
		}
		fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(data, ","))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

// modelModel is mo on a local provider whose server lists models.
func modelModel(t *testing.T, url, project string) (model, *[]*config.Config) {
	t.Helper()
	m, _, reloaded := projectMenuModel(t, fmt.Sprintf("provider = \"local\"\n[providers.local]\ntype = \"openai\"\nbase_url = %q\nmodel = \"qwen3-8b\"\n", url), project)
	m.settings = nil
	m.session.Provider, m.session.Model = "local", "qwen3-8b"
	m.session.Config.Providers = map[string]config.ProviderConfig{"local": {Protocol: "openai", BaseURL: url}}
	return m, reloaded
}

func TestModelPicker(t *testing.T) {
	t.Run("MP-01 /model lists the provider's models, the one in use marked; the pick is saved", func(t *testing.T) {
		m, _ := modelModel(t, modelServer(t, "qwen3-8b", "gemma-3-12b", "qwen3-coder-30b"), "")

		updated, cmd := m.runCommand("/model")
		m = deliver(updated.(model), cmd, 3)
		list := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "qwen3-coder-30b")

		layer, _ := config.ReadUserLayer()
		if !strings.Contains(list, "qwen3-8b  ● in use") || !strings.Contains(list, "gemma-3-12b") || !strings.Contains(list, "type another name") {
			t.Errorf("list:\n%s", list)
		}
		if layer.Providers["local"].Model != "qwen3-coder-30b" {
			t.Errorf("saved %+v", layer.Providers["local"])
		}
	})

	t.Run("MP-02 when the project sets the model, the pick goes to the project's file", func(t *testing.T) {
		m, reloaded := modelModel(t, modelServer(t, "qwen3-8b", "gemma-3-12b"), "model = \"qwen3-8b\"\n")

		updated, cmd := m.runCommand("/model")
		m = deliver(updated.(model), cmd, 3)
		m = pick(t, m, "gemma-3-12b")

		project, _ := config.ReadProject(m.session.WorkDir)
		user, _ := config.ReadUserLayer()
		if project.Layer.Model != "gemma-3-12b" || user.Providers["local"].Model != "qwen3-8b" || len(*reloaded) == 0 || (*reloaded)[0] == nil {
			t.Errorf("project %q user %q", project.Layer.Model, user.Providers["local"].Model)
		}
	})

	t.Run("MP-03 a provider that cannot list its models asks for the name instead", func(t *testing.T) {
		m, _ := modelModel(t, "http://127.0.0.1:1/v1", "")

		updated, cmd := m.runCommand("/model")
		m = deliver(updated.(model), cmd, 3)

		if view := ansi.Strip(m.settings.form.View()); !strings.Contains(view, "could not list the models") || !strings.Contains(view, "the model's id") {
			t.Errorf("view:\n%s", view)
		}
	})

	t.Run("MP-04 the Model setting picks from the list too, for the provider the form has", func(t *testing.T) {
		m, _ := modelModel(t, modelServer(t, "qwen3-8b", "gemma-3-12b"), "")
		m.session.Config = config.UserSettings(config.Config{Provider: "local", Providers: m.session.Config.Providers})
		layer, _ := config.ReadUserLayer()
		menu := m.newSettingsMenu(settingsLoadedMsg{layer: layer})
		section := settingsSections[0]

		updated, cmd := m.editItem(menu, section, section.items(m, menu)[1])
		m = deliver(updated.(model), cmd, 3)
		m = pick(t, m, "gemma-3-12b")

		if menu.draft.model != "gemma-3-12b" || !strings.Contains(ansi.Strip(m.settings.form.View()), "Settings › Model") {
			t.Errorf("draft model %q view:\n%s", menu.draft.model, m.settings.form.View())
		}
	})

	agentsMenu := func(t *testing.T) (model, *settingsMenu, settingsSection) {
		t.Helper()
		m, _ := modelModel(t, modelServer(t, "qwen3-8b", "gemma-3-12b"), "")
		m.session.Config = config.UserSettings(config.Config{Provider: "local", Providers: m.session.Config.Providers})
		layer, _ := config.ReadUserLayer()
		menu := m.newSettingsMenu(settingsLoadedMsg{layer: layer})
		section, _ := sectionByKey("agents")
		return m, menu, section
	}
	itemNamed := func(m model, menu *settingsMenu, section settingsSection, title string) settingItem {
		for _, item := range section.items(m, menu) {
			if item.title == title {
				return item
			}
		}
		return settingItem{}
	}

	t.Run("MP-05 Subagents' provider lists the providers set up; another one goes on to its models", func(t *testing.T) {
		m, menu, section := agentsMenu(t)

		updated, cmd := m.editItem(menu, section, itemNamed(m, menu, section, subagentsProvider))
		m = deliver(updated.(model), cmd, 3)
		providers := ansi.Strip(m.settings.form.View())
		m = pick(t, m, "local")
		m = pick(t, m, "gemma-3-12b")
		layer, err := menu.apply(menu.layer)

		if !strings.Contains(providers, "the main agent's") || !strings.Contains(providers, "local") {
			t.Errorf("providers:\n%s", providers)
		}
		if err != nil || layer.Agents.Model != "local/gemma-3-12b" || !strings.Contains(ansi.Strip(m.settings.form.View()), "Subagents' provider") {
			t.Errorf("err %v agents.model %q view:\n%s", err, layer.Agents.Model, m.settings.form.View())
		}
	})

	t.Run("MP-06 Subagents' model alone lists the main agent's provider's models and takes it too", func(t *testing.T) {
		m, menu, section := agentsMenu(t)

		updated, cmd := m.editItem(menu, section, itemNamed(m, menu, section, subagentsModel))
		m = deliver(updated.(model), cmd, 3)
		m = pick(t, m, "qwen3-8b")

		if menu.draft.agentsModel != (providerModel{"local", "qwen3-8b"}) {
			t.Errorf("agents model %+v", menu.draft.agentsModel)
		}
	})

	t.Run("MP-07 a type's Provider and Model are picked from lists the same way", func(t *testing.T) {
		m, menu, _ := agentsMenu(t)
		updated, _ := m.editAgentType(menu, newTypeChoice)
		e := updated.(model).settings.editor
		var providerEntry entry
		for _, each := range e.entries() {
			if each.title == typeProviderEntry {
				providerEntry = each
			}
		}

		updated, cmd := updated.(model).editEntry(e, providerEntry)
		m = deliver(updated.(model), cmd, 3)
		m = pick(t, m, "local")
		m = pick(t, m, "gemma-3-12b")

		if view := ansi.Strip(m.settings.form.View()); !strings.Contains(view, "local") || !strings.Contains(view, "gemma-3-12b") || !strings.Contains(view, "New type of subagent") {
			t.Errorf("view:\n%s", view)
		}
	})

	t.Run("MP-08 a provider left without a model is not saved, and says to pick one", func(t *testing.T) {
		_, menu, _ := agentsMenu(t)
		menu.draft.agentsModel = providerModel{provider: "local"}

		_, err := menu.apply(menu.layer)

		if err == nil || !strings.Contains(err.Error(), "pick a model of local") {
			t.Errorf("err = %v", err)
		}
	})
}
