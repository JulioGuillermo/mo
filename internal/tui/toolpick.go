package tui

import (
	"slices"
	"strings"

	"github.com/jg/mo/internal/tools"
)

// toolPick is a list of tools ticked, where an MCP server is one row: all
// its tools at once as mcp__server__*, or some of them by name.
type toolPick struct {
	own     []string
	servers []toolServer
	chosen  *[]string
}

// toolServer is an MCP server and the tools it gave, none when it is not
// connected.
type toolServer struct {
	name  string
	tools []string
}

func newToolPick(names, servers []string, chosen *[]string) *toolPick {
	pick := &toolPick{chosen: chosen}
	for _, server := range servers {
		pick.servers = append(pick.servers, toolServer{name: server})
	}
	for _, name := range names {
		at := slices.IndexFunc(pick.servers, func(server toolServer) bool { return strings.HasPrefix(name, mcpToolPrefix(server.name)) })
		if at < 0 {
			pick.own = append(pick.own, name)
			continue
		}
		pick.servers[at].tools = append(pick.servers[at].tools, name)
	}
	return pick
}

func (p *toolPick) server(name string) toolServer {
	at := slices.IndexFunc(p.servers, func(server toolServer) bool { return server.name == name })
	return p.servers[at]
}

func (p *toolPick) ticked(name string) bool {
	return tools.Matches(*p.chosen, name)
}

func (p *toolPick) toggle(name string) {
	if slices.Contains(*p.chosen, name) {
		*p.chosen = slices.DeleteFunc(slices.Clone(*p.chosen), func(each string) bool { return each == name })
		return
	}
	*p.chosen = append(slices.Clone(*p.chosen), name)
}

func (p *toolPick) wholeServer(server toolServer) bool {
	return slices.Contains(*p.chosen, mcpToolPrefix(server.name)+"*")
}

func (p *toolPick) tickedOf(server toolServer) []string {
	var ticked []string
	for _, name := range server.tools {
		if p.ticked(name) {
			ticked = append(ticked, name)
		}
	}
	return ticked
}

// allOf tells whether all of a server is ticked: by its pattern, or each
// of its tools.
func (p *toolPick) allOf(server toolServer) bool {
	return p.wholeServer(server) || (len(server.tools) > 0 && len(p.tickedOf(server)) == len(server.tools))
}

// toggleServer ticks all of a server, or none when it was all.
func (p *toolPick) toggleServer(server toolServer) {
	if p.allOf(server) {
		p.setServer(server, nil)
		return
	}
	p.tickServer(server)
}

// tickServer ticks a server by its pattern, which takes the tools it adds
// later too.
func (p *toolPick) tickServer(server toolServer) {
	p.setServer(server, nil)
	*p.chosen = append(*p.chosen, mcpToolPrefix(server.name)+"*")
}

// setServer ticks these tools of a server and no others; all of them
// become its pattern.
func (p *toolPick) setServer(server toolServer, ticked []string) {
	prefix := mcpToolPrefix(server.name)
	*p.chosen = slices.DeleteFunc(slices.Clone(*p.chosen), func(each string) bool { return strings.HasPrefix(each, prefix) })
	if len(ticked) > 0 && len(ticked) == len(server.tools) {
		*p.chosen = append(*p.chosen, prefix+"*")
		return
	}
	*p.chosen = append(*p.chosen, ticked...)
}
