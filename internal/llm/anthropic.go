package llm

import (
	"context"
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
}

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
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
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
	headers := map[string]string{
		"x-api-key":         p.APIKey,
		"anthropic-version": "2023-06-01",
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
	resp, err := postJSON(ctx, clientOrDefault(p.Client), url, headers, buildAnthropicRequest(req))
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
		return stream.handle(event)
	})
	if err != nil {
		return Response{}, err
	}
	return Response{Message: stream.message(), Usage: stream.usage}, nil
}

// buildAnthropicRequest also marks the system prompt, the tool list and the
// newest message as prompt cache breakpoints.
func buildAnthropicRequest(req Request) anthropicRequest {
	ephemeral := &cacheControl{Type: "ephemeral"}
	out := anthropicRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: true}
	if out.MaxTokens == 0 {
		out.MaxTokens = anthropicDefaultMaxTokens
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
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{{Type: "text", Text: message.Text}}})
		}
	}
	return out
}
