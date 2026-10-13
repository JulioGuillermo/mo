package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OpenAI talks the chat completions protocol, which llama-server, Ollama,
// LM Studio, vLLM, OpenRouter and OpenAI itself all serve.
type OpenAI struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type openAIRequest struct {
	Model         string          `json:"model,omitempty"`
	Messages      []openAIMessage `json:"messages"`
	Tools         []openAITool    `json:"tools,omitempty"`
	MaxTokens     int             `json:"max_tokens,omitempty"`
	Stream        bool            `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content          string           `json:"content"`
			ReasoningContent string           `json:"reasoning_content"`
			Reasoning        string           `json:"reasoning"`
			ToolCalls        []openAIToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Timings *struct {
		PromptPerSecond    float64 `json:"prompt_per_second"`
		PredictedPerSecond float64 `json:"predicted_per_second"`
	} `json:"timings"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAI) Stream(ctx context.Context, req Request, onEvent func(Event)) (Response, error) {
	clock := newSpeedClock()
	resp, err := postJSON(ctx, clientOrDefault(p.Client), p.url("/chat/completions"), p.headers(), p.buildRequest(req))
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	var text strings.Builder
	var calls []*openAIToolCall
	var usage Usage
	cutOff := false
	err = readSSE(resp.Body, func(data []byte) error {
		if string(data) == "[DONE]" {
			return errStopSSE
		}
		var chunk openAIChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("bad stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("provider error: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			usage.InputTokens, usage.OutputTokens = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
		}
		if chunk.Timings != nil {
			usage.PromptRate, usage.GenerationRate = chunk.Timings.PromptPerSecond, chunk.Timings.PredictedPerSecond
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if delta.Content != "" || delta.ReasoningContent != "" || delta.Reasoning != "" || len(delta.ToolCalls) > 0 {
				clock.piece()
			}
			if delta.Content != "" {
				text.WriteString(delta.Content)
				onEvent(Event{Kind: EventText, Text: delta.Content})
			}
			if delta.ReasoningContent != "" {
				onEvent(Event{Kind: EventReasoning, Text: delta.ReasoningContent})
			}
			if delta.Reasoning != "" {
				onEvent(Event{Kind: EventReasoning, Text: delta.Reasoning})
			}
			calls = mergeToolCallDeltas(calls, delta.ToolCalls)
			if choice.FinishReason == "length" {
				cutOff = true
			}
		}
		return nil
	})
	if err != nil {
		return Response{}, err
	}

	message := Message{Role: RoleAssistant, Text: text.String()}
	if cutOff && len(calls) == 0 {
		message.Text += cutOffTextNote
		onEvent(Event{Kind: EventText, Text: cutOffTextNote})
	}
	for i, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		message.ToolCalls = append(message.ToolCalls, newToolCall(id, call.Function.Name, call.Function.Arguments))
	}
	return Response{Message: message, Usage: clock.fill(usage)}, nil
}

// mergeToolCallDeltas assembles tool calls that arrive in pieces: the first
// piece for an index carries id and name, later ones append to the arguments.
// A piece without index continues the last call unless it starts a new one.
func mergeToolCallDeltas(calls []*openAIToolCall, deltas []openAIToolCall) []*openAIToolCall {
	for _, delta := range deltas {
		index := deltaIndex(delta, len(calls))
		for len(calls) <= index {
			calls = append(calls, &openAIToolCall{})
		}
		call := calls[index]
		if delta.ID != "" {
			call.ID = delta.ID
		}
		if delta.Function.Name != "" {
			call.Function.Name = delta.Function.Name
		}
		call.Function.Arguments += delta.Function.Arguments
	}
	return calls
}

func deltaIndex(delta openAIToolCall, callCount int) int {
	if delta.Index != nil && *delta.Index >= 0 {
		return *delta.Index
	}
	startsNewCall := delta.ID != "" || delta.Function.Name != ""
	if startsNewCall || callCount == 0 {
		return callCount
	}
	return callCount - 1
}

func (p *OpenAI) buildRequest(req Request) openAIRequest {
	out := openAIRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: true}
	out.StreamOptions.IncludeUsage = true
	if req.System != "" {
		out.Messages = append(out.Messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, message := range req.Messages {
		out.Messages = append(out.Messages, toOpenAIMessage(message))
	}
	for _, spec := range req.Tools {
		var tool openAITool
		tool.Type = "function"
		tool.Function.Name = spec.Name
		tool.Function.Description = spec.Description
		tool.Function.Parameters = spec.Schema
		out.Tools = append(out.Tools, tool)
	}
	return out
}

type openAIPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// openAIContent is the text alone, or the text and the images as parts.
func openAIContent(message Message) any {
	if len(message.Images) == 0 {
		return message.Text
	}
	parts := []openAIPart{{Type: "text", Text: message.Text}}
	for _, image := range message.Images {
		part := openAIPart{Type: "image_url", ImageURL: &struct {
			URL string `json:"url"`
		}{URL: "data:" + image.MIME + ";base64," + base64.StdEncoding.EncodeToString(image.Data)}}
		parts = append(parts, part)
	}
	return parts
}

func toOpenAIMessage(message Message) openAIMessage {
	out := openAIMessage{Role: string(message.Role), Content: openAIContent(message), ToolCallID: message.ToolCallID}
	for _, call := range message.ToolCalls {
		var wire openAIToolCall
		wire.ID = call.ID
		wire.Type = "function"
		wire.Function.Name = call.Name
		wire.Function.Arguments = string(call.Args)
		out.ToolCalls = append(out.ToolCalls, wire)
	}
	return out
}

// ListModels returns the ids the server offers, in the order it lists them.
func (p *OpenAI) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url("/models"), nil)
	if err != nil {
		return nil, err
	}
	for name, value := range p.headers() {
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

// StatusError is a server that answered listing its models with an error.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return "listing models: " + e.Status
}

func (p *OpenAI) headers() map[string]string {
	if p.APIKey == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + p.APIKey}
}

func (p *OpenAI) url(path string) string {
	return strings.TrimRight(p.BaseURL, "/") + path
}
