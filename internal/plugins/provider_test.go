package plugins

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | PP-01 | Happy    | mo.provider declares a provider of a type, openai unless said, marked as the plugin's | P0 |
// | PP-02 | Happy    | Its client puts in each request the headers its function gives and the fields params give, the request's model known | P0 |
// | PP-03 | Happy    | Static params merge into the body; a request without a body is left as it is | P1     |
// | PP-04 | Edge     | A provider whose name is taken is left out with a warning; one of the settings has no client | P1 |
// | PP-05 | Error    | An unknown type, an openai provider without base_url, or a name twice fails the load | P1 |
// | PP-06 | Error    | A header or params function that fails fails the request with its error    | P1       |

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
)

// echoServer answers each request with what it got: its Authorization
// header and its body.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Write([]byte(r.Header.Get("Authorization") + "|" + string(body)))
	}))
	t.Cleanup(web.Close)
	return web
}

func post(t *testing.T, client *http.Client, url, body string) string {
	t.Helper()
	response, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	got, _ := io.ReadAll(response.Body)
	return string(got)
}

func TestPluginProviders(t *testing.T) {
	t.Run("PP-01 mo.provider declares a provider of a type, openai unless said, marked as the plugin's", func(t *testing.T) {
		set := loadSource(t, map[string]string{"gw": `
mo.provider{ name = "gateway", base_url = "https://gw.example/v1", model = "big", headers = { ["X-Team"] = "core" } }
mo.provider{ name = "claude", type = "anthropic", api_key = "k" }`}).set

		providers := set.Providers(nil)

		gateway, claude := providers["gateway"], providers["claude"]
		if gateway.Protocol != "openai" || gateway.BaseURL != "https://gw.example/v1" || gateway.Model != "big" || gateway.Headers["X-Team"] != "core" || gateway.Plugin != "gw" {
			t.Errorf("gateway = %+v", gateway)
		}
		if claude.Protocol != "anthropic" || claude.APIKey != "k" {
			t.Errorf("claude = %+v", claude)
		}
	})

	t.Run("PP-02 its client puts in each request the headers its function gives and the fields params give, the request's model known", func(t *testing.T) {
		web := echoServer(t)
		set := loadWithSettings(t, map[string]string{"gw": `
mo.setting{ name = "token" }
mo.provider{ name = "gateway", base_url = "` + web.URL + `",
  headers = function(request) return { Authorization = "Bearer " .. mo.settings.get("token") } end,
  params = function(request) return { user = "jg", metadata = { model = request.model, method = request.method } } end }`},
			map[string]map[string]any{"gw": {"token": "t1"}})
		providers := set.Providers(nil)

		got := post(t, set.ProviderClient("gateway", providers["gateway"]), web.URL+"/chat", `{"model":"big","stream":true}`)

		token, body, _ := strings.Cut(got, "|")
		var fields map[string]any
		json.Unmarshal([]byte(body), &fields)
		metadata, _ := fields["metadata"].(map[string]any)
		if token != "Bearer t1" || fields["user"] != "jg" || fields["stream"] != true || metadata["model"] != "big" || metadata["method"] != "POST" {
			t.Errorf("got %s", got)
		}
	})

	t.Run("PP-03 static params merge into the body; a request without a body is left as it is", func(t *testing.T) {
		web := echoServer(t)
		set := loadSource(t, map[string]string{"gw": `mo.provider{ name = "gateway", base_url = "` + web.URL + `", params = { temperature = 0.2, stream = false } }`}).set
		client := set.ProviderClient("gateway", set.Providers(nil)["gateway"])

		got := post(t, client, web.URL, `{"model":"m","stream":true}`)
		response, err := client.Get(web.URL + "/models")

		if !strings.Contains(got, `"temperature":0.2`) || !strings.Contains(got, `"stream":false`) {
			t.Errorf("got %s", got)
		}
		if err != nil || response.StatusCode != http.StatusOK {
			t.Errorf("get: %v", err)
		}
	})

	t.Run("PP-04 a provider whose name is taken is left out with a warning; one of the settings has no client", func(t *testing.T) {
		set := loadSource(t, map[string]string{"gw": `mo.provider{ name = "ollama", base_url = "https://x", headers = function() return {} end }`}).set

		providers := set.Providers([]string{"ollama"})

		if len(providers) != 0 || len(set.Status()[0].Warnings) != 1 {
			t.Errorf("providers = %v warnings = %v", providers, set.Status()[0].Warnings)
		}
		if set.ProviderClient("ollama", config.ProviderConfig{BaseURL: "https://x"}) != nil {
			t.Error("a provider of the settings got a plugin's client")
		}
	})

	t.Run("PP-05 an unknown type, an openai provider without base_url, or a name twice fails the load", func(t *testing.T) {
		set := loadSource(t, map[string]string{
			"kind":  `mo.provider{ name = "x", type = "bard", base_url = "https://x" }`,
			"url":   `mo.provider{ name = "x" }`,
			"twice": `mo.provider{ name = "x", base_url = "https://x" } mo.provider{ name = "x", base_url = "https://y" }`,
		}).set

		for _, status := range set.Status() {
			if status.Err == nil {
				t.Errorf("plugin %s loaded", status.Name)
			}
		}
	})

	t.Run("PP-06 a header or params function that fails fails the request with its error", func(t *testing.T) {
		web := echoServer(t)
		set := loadSource(t, map[string]string{"gw": `mo.provider{ name = "gateway", base_url = "` + web.URL + `", params = function() error("quota left unknown", 0) end }`}).set
		client := set.ProviderClient("gateway", set.Providers(nil)["gateway"])

		_, err := client.Post(web.URL, "application/json", strings.NewReader(`{"model":"m"}`))

		if err == nil || !strings.Contains(err.Error(), "params of gateway") || !strings.Contains(err.Error(), "quota left unknown") {
			t.Errorf("err = %v", err)
		}
	})
}
