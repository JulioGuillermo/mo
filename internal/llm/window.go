package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	headers, err := p.headers(ctx)
	if err != nil {
		return 0, false
	}
	for name, value := range headers {
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

// ModelWindow asks the server how much context the model has, in the ways
// servers that are not llama-server say it: the model's entry in /models,
// Ollama's loaded or set context, LM Studio's, or Gemini's own model API.
// It returns 0 when none says, and where the size came from.
func (p *OpenAI) ModelWindow(ctx context.Context, model string) (int, string) {
	if model == "" {
		return 0, ""
	}
	if strings.Contains(p.BaseURL, "generativelanguage.googleapis.com") {
		return p.geminiWindow(ctx, model), "Gemini's model API"
	}
	for _, probe := range []struct {
		from string
		size func(context.Context, string) int
	}{
		{"the server's model list", p.listedWindow},
		{"Ollama", p.ollamaWindow},
		{"LM Studio", p.lmStudioWindow},
	} {
		if size := probe.size(ctx, model); size > 0 {
			return size, probe.from
		}
	}
	return 0, ""
}

// root is the server's address without the /v1 the OpenAI routes are under.
func (p *OpenAI) root() string {
	return strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1")
}

// getJSON reads a JSON answer of the server into into, with its key.
func (p *OpenAI) getJSON(ctx context.Context, method, url string, body any, into any) error {
	headers, err := p.headers(ctx)
	if err != nil {
		return err
	}
	var resp *http.Response
	if method == http.MethodPost {
		resp, err = postJSON(ctx, clientOrDefault(p.Client), url, headers, body)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, method, url, nil)
		if reqErr != nil {
			return reqErr
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		resp, err = clientOrDefault(p.Client).Do(req)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// windowFields are the names servers give a model's context in /models:
// OpenRouter and Together, Groq, vLLM, Mistral.
var windowFields = []string{"context_length", "context_window", "max_model_len", "max_context_length"}

// listedWindow reads the model's context from its entry in /models.
func (p *OpenAI) listedWindow(ctx context.Context, model string) int {
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if p.getJSON(ctx, http.MethodGet, p.url("/models"), nil, &list) != nil {
		return 0
	}
	for _, entry := range list.Data {
		if id, _ := entry["id"].(string); id != model {
			continue
		}
		for _, field := range windowFields {
			if size, ok := entry[field].(float64); ok && size > 0 {
				return int(size)
			}
		}
		if top, ok := entry["top_provider"].(map[string]any); ok {
			if size, ok := top["context_length"].(float64); ok && size > 0 {
				return int(size)
			}
		}
	}
	return 0
}

// ollamaWindow is the context Ollama runs the model with: the one it is
// loaded with, else its num_ctx, else the most the model takes.
func (p *OpenAI) ollamaWindow(ctx context.Context, model string) int {
	var running struct {
		Models []struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if p.getJSON(ctx, http.MethodGet, p.root()+"/api/ps", nil, &running) == nil {
		for _, loaded := range running.Models {
			if (loaded.Name == model || loaded.Model == model) && loaded.ContextLength > 0 {
				return loaded.ContextLength
			}
		}
	}
	var shown struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if p.getJSON(ctx, http.MethodPost, p.root()+"/api/show", map[string]string{"model": model}, &shown) != nil {
		return 0
	}
	for _, line := range strings.Split(shown.Parameters, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "num_ctx" {
			var size int
			if _, err := fmt.Sscan(fields[1], &size); err == nil && size > 0 {
				return size
			}
		}
	}
	for key, value := range shown.ModelInfo {
		if size, ok := value.(float64); ok && strings.HasSuffix(key, ".context_length") && size > 0 {
			return int(size)
		}
	}
	return 0
}

// lmStudioWindow is the context LM Studio loaded the model with, else the
// most it takes.
func (p *OpenAI) lmStudioWindow(ctx context.Context, model string) int {
	var info struct {
		Loaded int `json:"loaded_context_length"`
		Max    int `json:"max_context_length"`
	}
	if p.getJSON(ctx, http.MethodGet, p.root()+"/api/v0/models/"+url.PathEscape(model), nil, &info) != nil {
		return 0
	}
	if info.Loaded > 0 {
		return info.Loaded
	}
	return info.Max
}

// geminiWindow reads the model's input limit from Gemini's own API, next
// to the OpenAI-compatible one.
func (p *OpenAI) geminiWindow(ctx context.Context, model string) int {
	base := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/openai")
	var info struct {
		InputTokenLimit int `json:"inputTokenLimit"`
	}
	address := base + "/models/" + url.PathEscape(strings.TrimPrefix(model, "models/"))
	if p.APIKey == "" {
		if p.getJSON(ctx, http.MethodGet, address, nil, &info) != nil {
			return 0
		}
		return info.InputTokenLimit
	}
	// The native API takes the key as key=, not as a bearer.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"?key="+url.QueryEscape(p.APIKey), nil)
	if err != nil {
		return 0
	}
	resp, err := clientOrDefault(p.Client).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&info) != nil {
		return 0
	}
	return info.InputTokenLimit
}
