// Package mcphub connects to the MCP servers in the config and offers their
// tools to the agent as ordinary tools.
package mcphub

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jg/mo/internal/config"
)

const (
	connectTimeout   = 20 * time.Second
	stopGrace        = 2 * time.Second
	stopPollInterval = 50 * time.Millisecond
)

var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type Hub struct {
	servers []*server
}

type server struct {
	name    string
	trust   bool
	session *mcp.ClientSession
	command *exec.Cmd
	tools   []namedTool

	mu  sync.Mutex
	err error
}

type namedTool struct {
	name string
	tool *mcp.Tool
}

type Status struct {
	Name  string
	Tools int
	Err   error
}

// Connect starts or reaches every server at once. A server that fails is
// kept with its error, so Status can report it; its tools are left out.
// logDir receives what stdio servers write to stderr.
func Connect(ctx context.Context, servers map[string]config.MCPServer, logDir string) *Hub {
	hub := &Hub{}
	var wg sync.WaitGroup
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		entry := &server{name: name, trust: servers[name].Trust}
		hub.servers = append(hub.servers, entry)
		wg.Go(func() {
			entry.connect(ctx, servers[name], logDir)
		})
	}
	wg.Wait()
	hub.nameTools()
	return hub
}

func (s *server) connect(ctx context.Context, settings config.MCPServer, logDir string) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	transport, command, err := transportFor(s.name, settings, logDir)
	if err != nil {
		s.err = err
		return
	}
	s.command = command
	session, tools, err := connectWith(ctx, transport)
	if err != nil {
		s.err = err
		s.stopProcesses()
		return
	}
	s.session = session
	for _, tool := range tools {
		s.tools = append(s.tools, namedTool{tool: tool})
	}
}

func connectWith(ctx context.Context, transport mcp.Transport) (*mcp.ClientSession, []*mcp.Tool, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "mo", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, err
	}
	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			session.Close()
			return nil, nil, err
		}
		tools = append(tools, tool)
	}
	return session, tools, nil
}

func transportFor(name string, settings config.MCPServer, logDir string) (mcp.Transport, *exec.Cmd, error) {
	switch {
	case settings.URL != "":
		client := &http.Client{Transport: headerTransport{headers: expanded(settings.Headers)}}
		return &mcp.StreamableClientTransport{Endpoint: settings.URL, HTTPClient: client}, nil, nil
	case settings.Command != "":
		cmd := exec.Command(settings.Command, settings.Args...)
		cmd.Env = os.Environ()
		for key, value := range expanded(settings.Env) {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		logFile, err := openLog(logDir, name)
		if err != nil {
			return nil, nil, err
		}
		cmd.Stderr = logFile
		return &mcp.CommandTransport{Command: cmd}, cmd, nil
	}
	return nil, nil, fmt.Errorf("set command or url")
}

// openLog keeps a stdio server's stderr out of the terminal.
func openLog(logDir, name string) (*os.File, error) {
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, err
	}
	fileName := "mcp-" + unsafeNameChars.ReplaceAllString(name, "_") + ".log"
	return os.OpenFile(filepath.Join(logDir, fileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// expanded replaces ${NAME} with the environment variable; any other $ is
// kept as it is.
func expanded(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		out[key] = envReference.ReplaceAllStringFunc(value, func(reference string) string {
			return os.Getenv(envReference.FindStringSubmatch(reference)[1])
		})
	}
	return out
}

type headerTransport struct {
	headers map[string]string
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for name, value := range t.headers {
		req.Header.Set(name, value)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (h *Hub) Status() []Status {
	var statuses []Status
	for _, entry := range h.servers {
		statuses = append(statuses, Status{Name: entry.name, Tools: len(entry.tools), Err: entry.failure()})
	}
	return statuses
}

func (s *server) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *server) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Close ends every session at once, then stops whatever their servers
// started.
func (h *Hub) Close() {
	var wg sync.WaitGroup
	for _, entry := range h.servers {
		wg.Go(func() {
			if entry.session != nil {
				entry.session.Close()
			}
			entry.stopProcesses()
		})
	}
	wg.Wait()
}

// stopProcesses signals the server's whole process group: it runs in its own
// session, so its children would outlive mo otherwise. What ignores SIGTERM
// for stopGrace gets SIGKILL.
func (s *server) stopProcesses() {
	if s.command == nil || s.command.Process == nil {
		return
	}
	group := -s.command.Process.Pid
	if syscall.Kill(group, syscall.SIGTERM) != nil {
		return
	}
	deadline := time.Now().Add(stopGrace)
	for time.Now().Before(deadline) {
		if syscall.Kill(group, 0) != nil {
			return
		}
		time.Sleep(stopPollInterval)
	}
	syscall.Kill(group, syscall.SIGKILL)
}
