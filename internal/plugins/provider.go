package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/config"
)

var providerTypes = []string{"openai", "anthropic", "claude-code", "responses", "cloudcode"}

// providerDeclared is a provider a plugin declares: its settings, and the
// functions giving the headers and the extra fields of each request.
type providerDeclared struct {
	name     string
	settings config.ProviderConfig
	headers  *lua.LFunction
	params   lua.LValue
}

// declareProvider is mo.provider{ name, type, base_url, model, api_key,
// headers, params }: a provider mo can talk to, as one of the settings.
// headers is a table or a function giving those of each request; params
// are fields put in the body of each request, a table or a function.
func (p *Plugin) declareProvider(state *lua.LState) int {
	p.mustBeLoading(state, "mo.provider")
	spec := state.CheckTable(1)
	name := luaString(spec.RawGetString("name"))
	if !validName(name) {
		state.RaiseError("mo.provider needs a name (a letter or _, then letters, digits, _ or -)")
	}
	if slices.ContainsFunc(p.providers, func(each providerDeclared) bool { return each.name == name }) {
		state.RaiseError("mo.provider: %s is declared twice", name)
	}
	kind := luaString(spec.RawGetString("type"))
	if kind == "" {
		kind = "openai"
	}
	if !slices.Contains(providerTypes, kind) {
		state.RaiseError("mo.provider %s: unknown type %q (use %s)", name, kind, strings.Join(providerTypes, ", "))
	}
	settings := config.ProviderConfig{
		Protocol: kind,
		BaseURL:  luaString(spec.RawGetString("base_url")),
		Model:    luaString(spec.RawGetString("model")),
		APIKey:   luaString(spec.RawGetString("api_key")),
		Plugin:   p.name,
	}
	if settings.BaseURL == "" && kind == "openai" {
		state.RaiseError("mo.provider %s: an openai provider needs its base_url", name)
	}
	declared := providerDeclared{name: name}
	switch headers := spec.RawGetString("headers").(type) {
	case *lua.LFunction:
		declared.headers = headers
	case *lua.LNilType:
	default:
		var err error
		if settings.Headers, err = stringMap(headers); err != nil {
			state.RaiseError("mo.provider %s: headers: %v", name, err)
		}
	}
	switch params := spec.RawGetString("params"); params.(type) {
	case *lua.LFunction, *lua.LTable:
		declared.params = params
	case *lua.LNilType:
	default:
		state.RaiseError("mo.provider %s: params are a table or a function", name)
	}
	declared.settings = settings
	p.providers = append(p.providers, declared)
	return 0
}

// Providers are the providers the plugins declare, by name. One whose
// name is in taken, or another plugin already has, is left out with a
// warning.
func (s *Set) Providers(taken []string) map[string]config.ProviderConfig {
	providers := map[string]config.ProviderConfig{}
	for _, plugin := range s.loaded() {
		for _, declared := range plugin.providers {
			if _, twice := providers[declared.name]; twice || slices.Contains(taken, declared.name) {
				plugin.warn("provider " + declared.name + " left out: the name is taken")
				continue
			}
			providers[declared.name] = declared.settings
		}
	}
	return providers
}

// ProviderClient is the HTTP client of a provider a plugin declared: it puts
// in each request the headers and the fields the plugin's functions give.
// nil for a provider of the settings, or one whose plugin is gone.
func (s *Set) ProviderClient(name string, settings config.ProviderConfig) *http.Client {
	if settings.Plugin == "" {
		return nil
	}
	for _, plugin := range s.loaded() {
		if plugin.name != settings.Plugin {
			continue
		}
		for _, declared := range plugin.providers {
			if declared.name == name && (declared.headers != nil || declared.params != nil) {
				return &http.Client{Transport: &pluginTransport{plugin: plugin, declared: declared, base: http.DefaultTransport}}
			}
		}
	}
	return nil
}

// pluginTransport sets on each request to a plugin's provider what its
// functions give: headers, then fields merged into a JSON body.
type pluginTransport struct {
	plugin   *Plugin
	declared providerDeclared
	base     http.RoundTripper
}

func (t *pluginTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	body, err := readBody(req)
	if err != nil {
		return nil, err
	}
	request := map[string]string{"method": req.Method, "url": req.URL.String()}
	var fields map[string]any
	if body != nil && json.Unmarshal(body, &fields) == nil {
		request["model"], _ = fields["model"].(string)
	}
	if t.declared.headers != nil {
		headers, err := t.callTable(req.Context(), t.declared.headers, request)
		if err != nil {
			return nil, fmt.Errorf("headers of %s: %w", t.declared.name, err)
		}
		for name, value := range headers {
			req.Header.Set(name, fmt.Sprint(value))
		}
	}
	if t.declared.params != nil && fields != nil {
		params, err := t.params(req.Context(), request)
		if err != nil {
			return nil, fmt.Errorf("params of %s: %w", t.declared.name, err)
		}
		for name, value := range params {
			fields[name] = value
		}
		if body, err = json.Marshal(fields); err != nil {
			return nil, err
		}
	}
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	return t.base.RoundTrip(req)
}

// params are the fields to put in a request's body: the plugin's table, or
// what its function gives for the request.
func (t *pluginTransport) params(ctx context.Context, request map[string]string) (map[string]any, error) {
	if function, ok := t.declared.params.(*lua.LFunction); ok {
		return t.callTable(ctx, function, request)
	}
	result, err := t.plugin.call(ctx, hookTimeout, func(*lua.LState) (any, error) {
		return toGo(t.declared.params)
	})
	if err != nil {
		return nil, err
	}
	return asFields(result)
}

// callTable calls a plugin's function with the request and reads the table
// it gives; nil is none.
func (t *pluginTransport) callTable(ctx context.Context, function *lua.LFunction, request map[string]string) (map[string]any, error) {
	result, err := t.plugin.callFunction(ctx, hookTimeout, function, func(value lua.LValue) (any, error) {
		if value == lua.LNil {
			return map[string]any{}, nil
		}
		return toGo(value)
	}, func(state *lua.LState) lua.LValue {
		table := state.NewTable()
		for name, value := range request {
			table.RawSetString(name, lua.LString(value))
		}
		return table
	})
	if err != nil {
		return nil, err
	}
	return asFields(result)
}

func asFields(value any) (map[string]any, error) {
	switch value := value.(type) {
	case map[string]any:
		return value, nil
	case []any:
		if len(value) == 0 {
			return map[string]any{}, nil
		}
	}
	return nil, fmt.Errorf("give a table of names to values")
}

func readBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	defer req.Body.Close()
	return io.ReadAll(req.Body)
}
