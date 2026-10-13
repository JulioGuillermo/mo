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
	size, _ := p.Props(ctx)
	return size
}

// Props reads llama-server's /props: its context size, and whether it is
// llama-server at all, which no other server answers there.
func (p *OpenAI) Props(ctx context.Context) (int, bool) {
	url := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1") + "/props"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	for name, value := range p.headers() {
		req.Header.Set(name, value)
	}
	resp, err := clientOrDefault(p.Client).Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var props struct {
		DefaultGenerationSettings struct {
			ContextSize int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if json.NewDecoder(resp.Body).Decode(&props) != nil {
		return 0, false
	}
	return props.DefaultGenerationSettings.ContextSize, props.DefaultGenerationSettings.ContextSize > 0
}
