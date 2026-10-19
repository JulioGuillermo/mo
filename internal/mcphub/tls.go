package mcphub

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jg/mo/internal/config"
)

// httpTransport is how mo reaches a server over HTTP: the system's
// certificate authorities, with the server's own ca added, or any
// certificate when it is insecure.
func httpTransport(settings config.MCPServer) (http.RoundTripper, error) {
	if settings.CA == "" && !settings.Insecure {
		return http.DefaultTransport, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if settings.Insecure {
		transport.TLSClientConfig.InsecureSkipVerify = true
		return transport, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	path := homePath(settings.CA)
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ca: %w", err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ca: no PEM certificate in %s", path)
	}
	transport.TLSClientConfig.RootCAs = pool
	return transport, nil
}

func homePath(path string) string {
	rest, found := strings.CutPrefix(path, "~/")
	if !found {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}
