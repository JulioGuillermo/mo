package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	errorBodyLimit    = 4096
	networkRetryDelay = 500 * time.Millisecond
)

// HTTPError is a provider's answer with a failing HTTP status: its
// headers and body kept, to tell a limit from other errors.
type HTTPError struct {
	Code   int
	Status string
	Header http.Header
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: %s", e.Status, e.Body)
}

// postJSON sends payload and returns the response when its status is a
// success. A request that fails at the network level is sent once more.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := send(ctx, client, url, headers, body)
	if err != nil && ctx.Err() == nil {
		select {
		case <-time.After(networkRetryDelay):
			resp, err = send(ctx, client, url, headers, body)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return nil, &HTTPError{Code: resp.StatusCode, Status: resp.Status, Header: resp.Header, Body: strings.TrimSpace(string(detail))}
	}
	return resp, nil
}

func send(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	markRetryable(req)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return client.Do(req)
}

// markRetryable lets the transport resend the request when the server has
// closed the reused keep-alive connection, which llama-server does after a
// stream. The empty Idempotency-Key header is not sent on the wire.
func markRetryable(req *http.Request) {
	req.Header["Idempotency-Key"] = nil
}

func clientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}
