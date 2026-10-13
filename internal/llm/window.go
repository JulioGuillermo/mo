package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// ContextWindow asks the server for its context size. Only llama-server
// reports it, through /props; for any other server it returns 0.
func (p *OpenAI) ContextWindow(ctx context.Context) int {
	url := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1") + "/props"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	for name, value := range p.headers() {
		req.Header.Set(name, value)
	}
	resp, err := clientOrDefault(p.Client).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var props struct {
		DefaultGenerationSettings struct {
			ContextSize int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if json.NewDecoder(resp.Body).Decode(&props) != nil {
		return 0
	}
	return props.DefaultGenerationSettings.ContextSize
}
