package mcphub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

const (
	maxToolNameLength = 64
	nameHashLength    = 6
	callTimeout       = 5 * time.Minute
)

var unsafeNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// nameTools gives every tool of every connected server a name providers
// accept, mcp__<server>__<tool>, unique across all servers. A name that had
// to be cut, or that another tool already took, ends in a hash of the
// original server and tool.
func (h *Hub) nameTools() {
	taken := map[string]bool{}
	for _, entry := range h.servers {
		for i := range entry.tools {
			original := entry.name + "/" + entry.tools[i].tool.Name
			name := "mcp__" + safe(entry.name) + "__" + safe(entry.tools[i].tool.Name)
			for attempt := 0; len(name) > maxToolNameLength || taken[name]; attempt++ {
				name = hashedName(name, fmt.Sprintf("%s#%d", original, attempt))
			}
			taken[name] = true
			entry.tools[i].name = name
		}
	}
}

func hashedName(name, seed string) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(seed)))[:nameHashLength]
	return name[:min(len(name), maxToolNameLength-nameHashLength-1)] + "_" + hash
}

func safe(name string) string {
	return unsafeNameChars.ReplaceAllString(name, "_")
}

// ToolServers maps the name of each tool to the server it comes from.
func (h *Hub) ToolServers() map[string]string {
	servers := map[string]string{}
	for _, entry := range h.servers {
		for _, named := range entry.tools {
			servers[named.name] = entry.name
		}
	}
	return servers
}

// Descriptions are what the servers that connected say they are for, by
// server, each cut as its settings say; a server without one, or whose
// settings turn it off, is left out.
func (h *Hub) Descriptions() map[string]string {
	descriptions := map[string]string{}
	for _, entry := range h.servers {
		limit := entry.settings.DescriptionLimit()
		if entry.description == "" || limit == 0 {
			continue
		}
		descriptions[entry.name] = cut(entry.description, limit)
	}
	return descriptions
}

// cut keeps the first limit characters of text, with … when it drops some.
func cut(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// Tools adapts the tools of the servers that connected.
func (h *Hub) Tools() []tools.Tool {
	var adapted []tools.Tool
	for _, entry := range h.servers {
		if entry.session == nil {
			continue
		}
		for _, named := range entry.tools {
			adapted = append(adapted, entry.adapt(named))
		}
	}
	return adapted
}

// NeedConfirmation names the tools of servers not marked trust.
func (h *Hub) NeedConfirmation() []string {
	var names []string
	for _, entry := range h.servers {
		if entry.session == nil || entry.trust {
			continue
		}
		for _, named := range entry.tools {
			names = append(names, named.name)
		}
	}
	return names
}

func (s *server) adapt(named namedTool) tools.Tool {
	schema, err := json.Marshal(named.tool.InputSchema)
	if err != nil || string(schema) == "null" {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	return tools.Tool{
		Name:        named.name,
		Description: named.tool.Description,
		Schema:      schema,
		LooseArgs:   true,
		Run: func(ctx context.Context, _ tools.Env, raw json.RawMessage) (string, error) {
			return s.call(ctx, named.tool.Name, raw)
		},
		Summary: func(raw json.RawMessage) string { return string(raw) },
	}
}

// call runs a tool on the server with a time limit. A broken connection is
// remembered, so Status shows the server as failed.
func (s *server) call(ctx context.Context, tool string, raw json.RawMessage) (string, error) {
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	result, err := s.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if errors.Is(err, mcp.ErrConnectionClosed) || errors.Is(err, io.EOF) {
		s.fail(err)
	}
	if err != nil {
		return "", err
	}
	text := contentText(result.Content)
	if result.IsError {
		return text, errors.New("the tool reported an error")
	}
	return text, nil
}

func savedImage(item *mcp.ImageContent) string {
	marker, err := render.SaveImage(item.Data, item.MIMEType)
	if err != nil {
		return fmt.Sprintf("[image %s, %d bytes, not saved: %v]", item.MIMEType, len(item.Data), err)
	}
	return marker
}

// contentText renders a result as text. Images are saved to a file and
// marked, so the UI draws them; audio is only named.
func contentText(content []mcp.Content) string {
	var parts []string
	for _, item := range content {
		switch item := item.(type) {
		case *mcp.TextContent:
			parts = append(parts, item.Text)
		case *mcp.ImageContent:
			parts = append(parts, savedImage(item))
		case *mcp.AudioContent:
			parts = append(parts, fmt.Sprintf("[audio %s, %d bytes]", item.MIMEType, len(item.Data)))
		case *mcp.ResourceLink:
			parts = append(parts, fmt.Sprintf("[%s](%s)", item.Name, item.URI))
		case *mcp.EmbeddedResource:
			if item.Resource != nil {
				parts = append(parts, item.Resource.Text)
			}
		}
	}
	if len(parts) == 0 {
		return "(no output)"
	}
	return strings.Join(parts, "\n")
}
