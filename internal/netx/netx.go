// Package netx makes the HTTP transports mo's clients share.
package netx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Transport is how mo reaches a server over HTTP: the system's certificate
// authorities, with the PEM file ca added when set, or any certificate
// when insecure.
func Transport(ca string, insecure bool) (http.RoundTripper, error) {
	if ca == "" && !insecure {
		return http.DefaultTransport, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		transport.TLSClientConfig.InsecureSkipVerify = true
		return transport, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	path := HomePath(ca)
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

// HomePath turns a leading ~/ into the home folder.
func HomePath(path string) string {
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
