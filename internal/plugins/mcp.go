package plugins

import (
	"context"
	"fmt"
	"maps"
	"slices"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/config"
)

// mcpServer is an MCP server a plugin declares: its settings, and the
// function that gives the headers of each request, when it has one.
type mcpServer struct {
	name     string
	settings config.MCPServer
	headers  *lua.LFunction
}

// declareMCP is mo.mcp{ name, url, headers, ca, insecure, trust } or
// mo.mcp{ name, command, args, env, trust }: a server mo connects to with
// those of the settings. headers is a table, or a function giving the
// headers of each request.
func (p *Plugin) declareMCP(state *lua.LState) int {
	p.mustBeLoading(state, "mo.mcp")
	spec := state.CheckTable(1)
	name := luaString(spec.RawGetString("name"))
	if !validName(name) {
		state.RaiseError("mo.mcp needs a name (a letter or _, then letters, digits, _ or -)")
	}
	if slices.ContainsFunc(p.servers, func(server mcpServer) bool { return server.name == name }) {
		state.RaiseError("mo.mcp: %s is declared twice", name)
	}
	settings := config.MCPServer{
		URL:      luaString(spec.RawGetString("url")),
		Command:  luaString(spec.RawGetString("command")),
		CA:       luaString(spec.RawGetString("ca")),
		Insecure: lua.LVAsBool(spec.RawGetString("insecure")),
		Trust:    lua.LVAsBool(spec.RawGetString("trust")),
		Plugin:   p.name,
	}
	if (settings.URL == "") == (settings.Command == "") {
		state.RaiseError("mo.mcp %s: give a url or a command", name)
	}
	var err error
	if settings.Args, err = stringList(spec.RawGetString("args")); err != nil {
		state.RaiseError("mo.mcp %s: args: %v", name, err)
	}
	if settings.Env, err = stringMap(spec.RawGetString("env")); err != nil {
		state.RaiseError("mo.mcp %s: env: %v", name, err)
	}
	server := mcpServer{name: name}
	switch headers := spec.RawGetString("headers").(type) {
	case *lua.LFunction:
		if settings.URL == "" {
			state.RaiseError("mo.mcp %s: headers are for a url", name)
		}
		server.headers = headers
	case *lua.LNilType:
	default:
		if settings.Headers, err = stringMap(headers); err != nil {
			state.RaiseError("mo.mcp %s: headers: %v", name, err)
		}
	}
	server.settings = settings
	p.servers = append(p.servers, server)
	return 0
}

// MCPServers are the servers the plugins declare, by name. One whose name
// is in taken, or another plugin already has, is left out with a warning.
func (s *Set) MCPServers(taken []string) map[string]config.MCPServer {
	servers := map[string]config.MCPServer{}
	for _, plugin := range s.loaded() {
		for _, server := range plugin.servers {
			if _, twice := servers[server.name]; twice || slices.Contains(taken, server.name) {
				plugin.warn("MCP server " + server.name + " left out: the name is taken")
				continue
			}
			servers[server.name] = server.settings
		}
	}
	return servers
}

// MCPHeaders are the headers a plugin's function gives for one request to
// its server; none when the server's headers are a table, or it is gone.
func (s *Set) MCPHeaders(ctx context.Context, plugin, server, method, url string) (map[string]string, error) {
	for _, each := range s.loaded() {
		if each.name != plugin {
			continue
		}
		for _, declared := range each.servers {
			if declared.name != server || declared.headers == nil {
				continue
			}
			result, err := each.callFunction(ctx, hookTimeout, declared.headers, headerValues, func(state *lua.LState) lua.LValue {
				request := state.NewTable()
				request.RawSetString("method", lua.LString(method))
				request.RawSetString("url", lua.LString(url))
				return request
			})
			if err != nil {
				return nil, fmt.Errorf("headers of %s: %w", server, err)
			}
			return result.(map[string]string), nil
		}
	}
	return nil, nil
}

func headerValues(result lua.LValue) (any, error) {
	if result == lua.LNil {
		return map[string]string{}, nil
	}
	return stringMap(result)
}

// stringList reads a Lua list of strings; nil is none.
func stringList(value lua.LValue) ([]string, error) {
	if value == lua.LNil {
		return nil, nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("a list of strings, not a %s", value.Type())
	}
	var list []string
	for i := 1; i <= table.Len(); i++ {
		list = append(list, lua.LVAsString(table.RawGetInt(i)))
	}
	return list, nil
}

// stringMap reads a Lua table of names to strings; nil is none.
func stringMap(value lua.LValue) (map[string]string, error) {
	if value == lua.LNil {
		return nil, nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("a table of names to strings, not a %s", value.Type())
	}
	values := map[string]string{}
	var bad error
	table.ForEach(func(key, each lua.LValue) {
		if _, isText := key.(lua.LString); !isText {
			bad = fmt.Errorf("its names are strings")
			return
		}
		values[string(key.(lua.LString))] = lua.LVAsString(each)
	})
	if bad != nil {
		return nil, bad
	}
	return maps.Clone(values), nil
}
