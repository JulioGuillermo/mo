package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Anthropic talks the Messages API and authenticates with an API key only.
type Anthropic struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
	// Token, when set, gives a bearer at each request instead of APIKey.
	Token func(ctx context.Context) (string, error)
	// Subscription sends the requests as Claude Code does with a Claude
	// account's sign-in: its beta header and the line its system prompt
	// must start with.
	Subscription bool
}

const (
	claudeCodeBeta   = "oauth-2025-04-20"
	claudeCodeSystem = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeAgent  = "claude-cli/2.1.299 (external, cli)"
)

const anthropicDefaultMaxTokens = 8192

type anthropicBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      string          `json:"content,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	Source       *imageSource    `json:"source,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []anthropicBlock   `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream"`
	// OutputConfig carries the effort, for the models that take it.
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type anthropicEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage anthropicUsage `json:"usage"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *Anthropic) Stream(ctx context.Context, req Request, onEvent func(Event)) (Response, error) {
	clock := newSpeedClock()
	headers, err := p.headers(ctx)
	if err != nil {
		return Response{}, err
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
	body := buildAnthropicRequest(req)
	if p.Subscription {
		body.System = append([]anthropicBlock{{Type: "text", Text: claudeCodeSystem}}, body.System...)
	}
	resp, err := postJSON(ctx, clientOrDefault(p.Client), url, headers, body)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	stream := newAnthropicStream(onEvent)
	err = readSSE(resp.Body, func(data []byte) error {
		var event anthropicEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("bad stream event: %w", err)
		}
		if event.Type == "content_block_delta" {
			clock.piece()
		}
		return stream.handle(event)
	})
	if err != nil {
		return Response{}, err
	}
	return Response{Message: stream.message(), Usage: clock.fill(stream.usage)}, nil
}

// buildAnthropicRequest also marks the system prompt, the tool list and the
// newest message as prompt cache breakpoints.
func buildAnthropicRequest(req Request) anthropicRequest {
	ephemeral := &cacheControl{Type: "ephemeral"}
	out := anthropicRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: true}
	if out.MaxTokens == 0 {
		out.MaxTokens = anthropicDefaultMaxTokens
	}
	if req.Effort != "" {
		out.OutputConfig = &anthropicOutputConfig{Effort: req.Effort}
	}
	if req.System != "" {
		out.System = []anthropicBlock{{Type: "text", Text: req.System, CacheControl: ephemeral}}
	}
	for _, spec := range req.Tools {
		out.Tools = append(out.Tools, anthropicTool{Name: spec.Name, Description: spec.Description, InputSchema: spec.Schema})
	}
	if len(out.Tools) > 0 {
		out.Tools[len(out.Tools)-1].CacheControl = ephemeral
	}
	out.Messages = toAnthropicMessages(req.Messages)
	if len(out.Messages) > 0 {
		lastBlocks := out.Messages[len(out.Messages)-1].Content
		lastBlocks[len(lastBlocks)-1].CacheControl = ephemeral
	}
	return out
}

// toAnthropicMessages folds consecutive tool results into one user message.
func toAnthropicMessages(messages []Message) []anthropicMessage {
	var out []anthropicMessage
	for _, message := range messages {
		switch message.Role {
		case RoleTool:
			block := anthropicBlock{
				Type:      "tool_result",
				ToolUseID: message.ToolCallID,
				Content:   message.Text,
				IsError:   message.IsError,
			}
			previous := len(out) - 1
			if previous >= 0 && out[previous].Role == "user" && out[previous].Content[0].Type == "tool_result" {
				out[previous].Content = append(out[previous].Content, block)
			} else {
				out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{block}})
			}
		case RoleAssistant:
			var blocks []anthropicBlock
			if strings.TrimSpace(message.Text) != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: message.Text})
			}
			for _, call := range message.ToolCalls {
				blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Args})
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: "(empty)"})
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: blocks})
		default:
			blocks := []anthropicBlock{{Type: "text", Text: message.Text}}
			for _, image := range message.Images {
				source := &imageSource{Type: "base64", MediaType: image.MIME, Data: base64.StdEncoding.EncodeToString(image.Data)}
				blocks = append(blocks, anthropicBlock{Type: "image", Source: source})
			}
			out = append(out, anthropicMessage{Role: "user", Content: blocks})
		}
	}
	return out
}

// ListModels returns the ids of the models the key can use, newest first,
// from the Models API.
func (p *Anthropic) ListModels(ctx context.Context) ([]string, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/models?limit=1000"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	headers, err := p.headers(ctx)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := clientOrDefault(p.Client).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	var ids []string
	for _, model := range list.Data {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

// headers authenticate a request: the key, or a sign-in's token as the
// bearer.
func (p *Anthropic) headers(ctx context.Context) (map[string]string, error) {
	headers := map[string]string{"anthropic-version": "2023-06-01"}
	if p.Token == nil {
		headers["x-api-key"] = p.APIKey
		return headers, nil
	}
	token, err := p.Token(ctx)
	if err != nil {
		return nil, err
	}
	headers["Authorization"] = "Bearer " + token
	if p.Subscription {
		headers["anthropic-beta"] = claudeCodeBeta
		headers["x-app"] = "cli"
		headers["User-Agent"] = claudeCodeAgent
	}
	return headers, nil
}

// ModelWindow is the model's context, from the Models API, or 0 when it
// does not say.
func (p *Anthropic) ModelWindow(ctx context.Context, model string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.BaseURL, "/")+"/v1/models/"+model, nil)
	if err != nil {
		return 0
	}
	headers, err := p.headers(ctx)
	if err != nil {
		return 0
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := clientOrDefault(p.Client).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var info struct {
		MaxInputTokens int `json:"max_input_tokens"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&info) != nil {
		return 0
	}
	return info.MaxInputTokens
}
