package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// EffortLister is a provider that says which efforts a model takes: the
// server's own word where it gives one, else what is known of the model's
// family; known is false when nothing is, and every effort is offered.
// An empty list means the model takes none.
type EffortLister interface {
	Efforts(ctx context.Context, model string) (efforts []string, known bool)
}

// AllEfforts are every effort mo sends, for a model nothing is known of.
var AllEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// refusedEfforts remembers the models whose server refused an effort, so
// they are offered none and sent none.
type refusedEfforts struct {
	mu     sync.Mutex
	models map[string]bool
}

func (r *refusedEfforts) refuse(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.models == nil {
		r.models = map[string]bool{}
	}
	r.models[model] = true
}

func (r *refusedEfforts) refused(model string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.models[model]
}

// refusesEffort tells an error that says the request's effort is not
// taken, as servers word it.
func refusesEffort(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if !strings.Contains(text, "400") && !strings.Contains(text, "422") && !strings.Contains(text, "invalid") {
		return false
	}
	return strings.Contains(text, "reasoning_effort") || strings.Contains(text, "reasoning effort") ||
		strings.Contains(text, "effort") || strings.Contains(text, "thinking")
}

// Efforts are what an OpenAI-compatible server's model takes: for
// llama-server, as its chat template says; else by the model's family.
func (p *OpenAI) Efforts(ctx context.Context, model string) ([]string, bool) {
	if p.refused.refused(model) {
		return nil, true
	}
	if p.PromptProgress {
		template := p.ChatTemplate(ctx)
		switch {
		case strings.Contains(template, "reasoning_effort"):
			return []string{"low", "medium", "high"}, true
		case strings.Contains(template, "enable_thinking"):
			return []string{"none"}, true
		case template != "":
			return nil, true
		}
	}
	name := strings.ToLower(model)
	switch {
	case strings.Contains(name, "gpt-oss"):
		return []string{"low", "medium", "high"}, true
	case strings.HasPrefix(name, "gpt-5"), strings.Contains(name, "/gpt-5"):
		return []string{"minimal", "low", "medium", "high"}, true
	case strings.HasPrefix(name, "o1"), strings.HasPrefix(name, "o3"), strings.HasPrefix(name, "o4"):
		return []string{"low", "medium", "high"}, true
	}
	return AllEfforts, false
}

// ChatTemplate is llama-server's chat template for the model it serves,
// from /props; empty from any other server.
func (p *OpenAI) ChatTemplate(ctx context.Context) string {
	url := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1") + "/props"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	headers, err := p.headers(ctx)
	if err != nil {
		return ""
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := clientOrDefault(p.Client).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var props struct {
		ChatTemplate string `json:"chat_template"`
	}
	if json.NewDecoder(resp.Body).Decode(&props) != nil {
		return ""
	}
	return props.ChatTemplate
}

// Efforts are the levels an Anthropic model takes, as Claude Code knows
// them; a model it does not know is offered every level.
func (p *Anthropic) Efforts(_ context.Context, model string) ([]string, bool) {
	if p.refused.refused(model) {
		return nil, true
	}
	if known, ok := lookupAnthropicModel(model); ok {
		return known.levels, true
	}
	return anthropicAllLevels, false
}

// Efforts are the levels the Responses API takes for Codex's models.
func (p *Responses) Efforts(context.Context, string) ([]string, bool) {
	return []string{"minimal", "low", "medium", "high"}, true
}

// Efforts are Gemini's thinking levels, none being its minimal one.
func (p *CloudCode) Efforts(context.Context, string) ([]string, bool) {
	return []string{"none", "low", "medium", "high"}, true
}
