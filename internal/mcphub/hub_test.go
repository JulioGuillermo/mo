package mcphub

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | MH-01 | Happy    | A server's tools become mcp__server__tool with their schema         | P0       |
// | MH-02 | Happy    | Calling an adapted tool returns the server's text                  | P0       |
// | MH-03 | Error    | A tool result flagged as error comes back as an error              | P1       |
// | MH-04 | Security | Tools of untrusted servers need confirmation, trusted ones do not  | P0       |
// | MH-05 | Error    | A server that fails to start is reported and adds no tools         | P1       |
// | MH-06 | Edge     | Unsafe characters and long names are made acceptable to providers  | P2       |
// | MH-07 | Happy    | Images are saved and marked, resource links named, in the result   | P2       |
// | MH-08 | Security | Tools whose names collide after cleaning each keep their own name  | P0       |
// | MH-09 | Security | Close also stops the processes a stdio server started              | P0       |
// | MH-10 | Edge     | ${NAME} reads the environment and any other $ stays as written     | P2       |
// | MH-11 | Happy    | Test lists a URL server's tools                                    | P1       |
// | MH-12 | Happy    | Test starts a command server and stops it after                    | P1       |
// | MH-13 | Error    | Test tells why a server does not answer                            | P1       |
// | MH-14 | Happy    | Reconnect keeps unchanged servers' connections and connects the new | P0       |
// | MH-15 | Security | CloseExcept leaves what the other hub shares and stops the rest    | P0       |

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

type greetArgs struct {
	Name string `json:"name"`
}

func demoServer(t *testing.T) *mcp.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "demo", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "Greet someone."}, func(_ context.Context, _ *mcp.CallToolRequest, args greetArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hello " + args.Name}}}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fail", Description: "Always fails."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "boom"}}}, nil, nil
	})
	return server
}

func hubWith(t *testing.T, name string, trust bool) *Hub {
	t.Helper()
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := demoServer(t).Connect(ctx, serverSide, nil); err != nil {
		t.Fatal(err)
	}
	session, found, err := connectWith(ctx, clientSide)
	if err != nil {
		t.Fatal(err)
	}
	entry := &server{name: name, trust: trust, session: session}
	for _, tool := range found {
		entry.tools = append(entry.tools, namedTool{tool: tool})
	}
	hub := &Hub{servers: []*server{entry}}
	hub.nameTools()
	t.Cleanup(hub.Close)
	return hub
}

func TestHubTools(t *testing.T) {
	ctx := context.Background()

	t.Run("MH-01 a server's tools become mcp__server__tool with their schema", func(t *testing.T) {
		adapted := hubWith(t, "demo", false).Tools()

		names := map[string]string{}
		for _, tool := range adapted {
			names[tool.Name] = string(tool.Schema)
		}
		if !strings.Contains(names["mcp__demo__greet"], `"name"`) || names["mcp__demo__fail"] == "" {
			t.Errorf("tools = %v", names)
		}
	})

	t.Run("MH-02 calling an adapted tool returns the server's text", func(t *testing.T) {
		greet := hubWith(t, "demo", false).Tools()[1]

		output, err := greet.Run(ctx, tools.Env{}, json.RawMessage(`{"name":"Julio"}`))

		if err != nil || output != "hello Julio" {
			t.Errorf("greet = %s output = %q err = %v", greet.Name, output, err)
		}
	})

	t.Run("MH-03 a tool result flagged as error comes back as an error", func(t *testing.T) {
		fail := hubWith(t, "demo", false).Tools()[0]

		output, err := fail.Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if err == nil || output != "boom" {
			t.Errorf("tool = %s output = %q err = %v", fail.Name, output, err)
		}
	})

	t.Run("MH-04 tools of untrusted servers need confirmation, trusted ones do not", func(t *testing.T) {
		untrusted := hubWith(t, "demo", false).NeedConfirmation()
		trusted := hubWith(t, "demo", true).NeedConfirmation()

		if len(untrusted) != 2 || len(trusted) != 0 {
			t.Errorf("untrusted = %v trusted = %v", untrusted, trusted)
		}
	})

	t.Run("MH-05 a server that fails to start is reported and adds no tools", func(t *testing.T) {
		hub := Connect(ctx, map[string]config.MCPServer{"broken": {Command: "/nonexistent/server"}}, t.TempDir(), nil)
		defer hub.Close()

		status := hub.Status()
		if len(status) != 1 || status[0].Err == nil || len(hub.Tools()) != 0 {
			t.Errorf("status = %+v tools = %d", status, len(hub.Tools()))
		}
	})
}

func TestNamesAndContent(t *testing.T) {
	t.Run("MH-06 unsafe characters and long names are made acceptable to providers", func(t *testing.T) {
		hub := &Hub{servers: []*server{{name: "my server", tools: []namedTool{{tool: &mcp.Tool{Name: "do.it/now" + strings.Repeat("x", 80)}}}}}}
		hub.nameTools()
		name := hub.servers[0].tools[0].name

		if strings.ContainsAny(name, " ./") || len(name) > maxToolNameLength || !strings.HasPrefix(name, "mcp__my_server__do_it_now") {
			t.Errorf("name = %q", name)
		}
	})

	t.Run("MH-08 tools whose names collide after cleaning each keep their own name", func(t *testing.T) {
		long := strings.Repeat("x", 70)
		hub := &Hub{servers: []*server{
			{name: "my.srv", tools: []namedTool{{tool: &mcp.Tool{Name: "a.b"}}, {tool: &mcp.Tool{Name: "a_b"}}, {tool: &mcp.Tool{Name: long + "1"}}, {tool: &mcp.Tool{Name: long + "2"}}}},
			{name: "my_srv", tools: []namedTool{{tool: &mcp.Tool{Name: "a_b"}}}},
			{name: "rev", tools: []namedTool{{tool: &mcp.Tool{Name: "a.b"}}, {tool: &mcp.Tool{Name: "a_b"}}, {tool: &mcp.Tool{Name: "a~b"}}}},
		}}
		taken := hashedName("mcp__rev__a_b", "rev/a~b#0")
		hub.servers[2].tools = append(hub.servers[2].tools, namedTool{tool: &mcp.Tool{Name: strings.TrimPrefix(taken, "mcp__rev__")}})

		hub.nameTools()

		seen := map[string]bool{}
		for _, entry := range hub.servers {
			for _, named := range entry.tools {
				if seen[named.name] || len(named.name) > maxToolNameLength {
					t.Errorf("name %q repeated or too long", named.name)
				}
				seen[named.name] = true
			}
		}
	})

	t.Run("MH-07 images are saved and marked, resource links named, in the text result", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		text := contentText([]mcp.Content{
			&mcp.TextContent{Text: "see"},
			&mcp.ImageContent{MIMEType: "image/png", Data: []byte("abc")},
			&mcp.ResourceLink{Name: "doc", URI: "file:///doc"},
		})

		paths := render.MarkedImages(text)
		if !strings.HasPrefix(text, "see\n[image image/png") || !strings.HasSuffix(text, "\n[doc](file:///doc)") || len(paths) != 1 {
			t.Fatalf("text = %q", text)
		}
		if saved, err := os.ReadFile(paths[0]); err != nil || string(saved) != "abc" {
			t.Errorf("saved = %q err = %v", saved, err)
		}
	})
}

func TestMain(m *testing.M) {
	if os.Getenv("MO_TEST_MCP_SERVER") == "1" {
		server := mcp.NewServer(&mcp.Implementation{Name: "helper", Version: "1"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: "ping"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
		})
		server.Run(context.Background(), &mcp.StdioTransport{})
		return
	}
	os.Exit(m.Run())
}

func TestProcesses(t *testing.T) {
	t.Run("MH-09 Close also stops the processes a stdio server started", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "child.pid")
		script := fmt.Sprintf("sleep 300 & echo $! > %s; exec %s", marker, os.Args[0])
		settings := config.MCPServer{Command: "sh", Args: []string{"-c", script}, Env: map[string]string{"MO_TEST_MCP_SERVER": "1"}}
		hub := Connect(context.Background(), map[string]config.MCPServer{"helper": settings}, t.TempDir(), nil)
		if status := hub.Status(); status[0].Err != nil {
			t.Fatal(status[0].Err)
		}
		data, _ := os.ReadFile(marker)
		child, _ := strconv.Atoi(strings.TrimSpace(string(data)))

		hub.Close()

		deadline := time.Now().Add(3 * time.Second)
		for processRunning(child) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if child == 0 || processRunning(child) {
			t.Errorf("child %d still running", child)
		}
	})

	t.Run("MH-10 ${NAME} reads the environment and any other $ stays as written", func(t *testing.T) {
		t.Setenv("MO_TOKEN", "secret")

		values := expanded(map[string]string{"auth": "Bearer ${MO_TOKEN}", "raw": "pa$$word $HOME"})

		if values["auth"] != "Bearer secret" || values["raw"] != "pa$$word $HOME" {
			t.Errorf("values = %v", values)
		}
	})
}

func TestTest(t *testing.T) {
	t.Run("MH-11 Test lists a URL server's tools", func(t *testing.T) {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return demoServer(t) }, nil)
		web := httptest.NewServer(handler)
		defer web.Close()

		names, err := Test(context.Background(), "demo", config.MCPServer{URL: web.URL}, t.TempDir())

		if err != nil || !slices.Equal(names, []string{"fail", "greet"}) {
			t.Errorf("names = %v, err = %v", names, err)
		}
	})

	t.Run("MH-12 Test starts a command server and stops it after", func(t *testing.T) {
		settings := config.MCPServer{Command: os.Args[0], Env: map[string]string{"MO_TEST_MCP_SERVER": "1"}}

		names, err := Test(context.Background(), "helper", settings, t.TempDir())

		if err != nil || !slices.Equal(names, []string{"ping"}) {
			t.Errorf("names = %v, err = %v", names, err)
		}
	})

	t.Run("MH-13 Test tells why a server does not answer", func(t *testing.T) {
		_, err := Test(context.Background(), "gone", config.MCPServer{Command: "false"}, t.TempDir())

		if err == nil {
			t.Error("no error from a server that exits")
		}
	})
}

func TestReconnect(t *testing.T) {
	web := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return demoServer(t) }, nil))
	defer web.Close()
	helper := config.MCPServer{Command: os.Args[0], Env: map[string]string{"MO_TEST_MCP_SERVER": "1"}}
	demo := config.MCPServer{URL: web.URL}

	t.Run("MH-14 Reconnect keeps unchanged servers' connections and connects the new", func(t *testing.T) {
		first := Connect(context.Background(), map[string]config.MCPServer{"demo": demo}, t.TempDir(), nil)
		defer first.Close()

		second := Reconnect(context.Background(), first, map[string]config.MCPServer{"demo": demo, "helper": helper}, t.TempDir(), nil)
		defer second.CloseExcept(first)

		if second.servers[0].session != first.servers[0].session {
			t.Error("demo connected again")
		}
		if status := second.Status(); len(status) != 2 || status[1].Err != nil || status[1].Tools != 1 {
			t.Errorf("status = %+v", status)
		}
	})

	t.Run("MH-15 CloseExcept leaves what the other hub shares and stops the rest", func(t *testing.T) {
		first := Connect(context.Background(), map[string]config.MCPServer{"demo": demo, "helper": helper}, t.TempDir(), nil)
		second := Reconnect(context.Background(), first, map[string]config.MCPServer{"demo": demo}, t.TempDir(), nil)
		defer second.Close()
		process := first.servers[1].command.Process.Pid

		first.CloseExcept(second)

		if _, err := second.servers[0].session.ListTools(context.Background(), nil); err != nil {
			t.Errorf("shared session closed: %v", err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for processRunning(process) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if processRunning(process) {
			t.Error("helper still running")
		}
	})
}
