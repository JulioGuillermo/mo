package mcphub

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | MT-01 | Security | A server signed by a private authority is refused by default              | P0       |
// | MT-02 | Happy    | With ca set to that authority's PEM, mo lists its tools                   | P0       |
// | MT-03 | Happy    | With insecure, a self-signed server is accepted                           | P0       |
// | MT-04 | Error    | A ca file that is missing or holds no certificate says so                 | P1       |

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jg/mo/internal/config"
)

// privateServer is an MCP server over TLS whose certificate the system
// does not trust, and the PEM file of the authority that signed it.
func privateServer(t *testing.T) (url, caFile string) {
	t.Helper()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return demoServer(t) }, nil)
	web := httptest.NewTLSServer(handler)
	t.Cleanup(web.Close)
	caFile = filepath.Join(t.TempDir(), "ca.crt")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: web.Certificate().Raw})
	if err := os.WriteFile(caFile, block, 0o600); err != nil {
		t.Fatal(err)
	}
	return web.URL, caFile
}

func TestPrivateCertificates(t *testing.T) {
	t.Run("MT-01 a server signed by a private authority is refused by default", func(t *testing.T) {
		url, _ := privateServer(t)

		_, err := Test(context.Background(), "private", config.MCPServer{URL: url}, t.TempDir())

		if err == nil || !strings.Contains(err.Error(), "certificate") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("MT-02 with ca set to that authority's PEM, mo lists its tools", func(t *testing.T) {
		url, caFile := privateServer(t)

		names, err := Test(context.Background(), "private", config.MCPServer{URL: url, CA: caFile}, t.TempDir())

		if err != nil || !slices.Equal(names, []string{"fail", "greet"}) {
			t.Errorf("names = %v err = %v", names, err)
		}
	})

	t.Run("MT-03 with insecure, a self-signed server is accepted", func(t *testing.T) {
		url, _ := privateServer(t)

		names, err := Test(context.Background(), "private", config.MCPServer{URL: url, Insecure: true}, t.TempDir())

		if err != nil || !slices.Equal(names, []string{"fail", "greet"}) {
			t.Errorf("names = %v err = %v", names, err)
		}
	})

	t.Run("MT-04 a ca file that is missing or holds no certificate says so", func(t *testing.T) {
		url, _ := privateServer(t)
		empty := filepath.Join(t.TempDir(), "empty.pem")
		os.WriteFile(empty, []byte("nothing here\n"), 0o600)

		_, missing := Test(context.Background(), "private", config.MCPServer{URL: url, CA: filepath.Join(t.TempDir(), "none.pem")}, t.TempDir())
		_, noPEM := Test(context.Background(), "private", config.MCPServer{URL: url, CA: empty}, t.TempDir())

		if missing == nil || !strings.Contains(missing.Error(), "ca:") || noPEM == nil || !strings.Contains(noPEM.Error(), "no PEM certificate") {
			t.Errorf("missing = %v noPEM = %v", missing, noPEM)
		}
	})
}
