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
	// Token, when set, gives the bearer at each request instead of APIKey:
	// a sign-in's token, renewed when it expires.
	Token func(ctx context.Context) (string, error)
	// Headers are sent with every request.
	Headers map[string]string
	// PromptProgress asks for how far the server is in reading the prompt.
	// Only for llama-server: other servers may refuse the field.
	PromptProgress bool
	// refused are the models the server took no effort for.
	refused refusedEfforts
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	Reasoning  string           `json:"reasoning_content,omitempty"`
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
	// ExtraContent is Gemini's: the thought signature of a call, which it
	// wants back with the call in the next requests.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
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
	Model     string          `json:"model,omitempty"`
	Messages  []openAIMessage `json:"messages"`
	Tools     []openAITool    `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	// ReasoningEffort is OpenAI's name for the effort, also taken by many
	// compatible servers; others ignore it.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ReturnProgress is llama-server's: stream prompt_progress chunks
	// while the prompt is read.
	ReturnProgress bool `json:"return_progress,omitempty"`
	// ChatTemplateKwargs reach llama-server's chat template: the effort
	// gpt-oss takes there, or enable_thinking off for none.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	Stream             bool           `json:"stream"`
	StreamOptions      struct {
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
	PromptProgress *struct {
		Total     int `json:"total"`
		Cache     int `json:"cache"`
		Processed int `json:"processed"`
	} `json:"prompt_progress"`
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
	headers, err := p.headers(ctx)
	if err != nil {
		return Response{}, err
	}
	resp, err := postJSON(ctx, clientOrDefault(p.Client), p.url("/chat/completions"), headers, p.buildRequest(req))
	if err != nil && req.Effort != "" && refusesEffort(err) {
		// The server takes no effort for this model: once more without it,
		// and none sent from now on.
		p.refused.refuse(req.Model)
		resp, err = postJSON(ctx, clientOrDefault(p.Client), p.url("/chat/completions"), headers, p.buildRequest(req))
	}
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	var text, reasoning strings.Builder
	var think thinkSplitter
	emit := func(answer, thought string) {
		if thought != "" {
			reasoning.WriteString(thought)
			onEvent(Event{Kind: EventReasoning, Text: thought})
		}
		if answer != "" {
			text.WriteString(answer)
			onEvent(Event{Kind: EventText, Text: answer})
		}
	}
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
		if progress := chunk.PromptProgress; progress != nil {
			onEvent(Event{Kind: EventPromptProgress, Progress: PromptProgress{Done: progress.Processed, Total: progress.Total, Cached: progress.Cache}})
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
			emit("", delta.ReasoningContent)
			emit("", delta.Reasoning)
			if delta.Content != "" {
				emit(think.feed(delta.Content))
			}
			calls = mergeToolCallDeltas(calls, delta.ToolCalls, func(index int, name, args string) {
				onEvent(Event{Kind: EventToolCall, Call: index, Name: name, Text: args})
			})
			if choice.FinishReason == "length" {
				cutOff = true
			}
		}
		return nil
	})
	if err != nil {
		return Response{}, err
	}

	emit(think.flush())
	message := Message{Role: RoleAssistant, Text: text.String(), Reasoning: reasoning.String()}
	if cutOff && len(calls) == 0 {
		message.Text += cutOffTextNote
		onEvent(Event{Kind: EventText, Text: cutOffTextNote})
	}
	for i, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		toolCall := newToolCall(id, call.Function.Name, call.Function.Arguments)
		toolCall.Extra = call.ExtraContent
		message.ToolCalls = append(message.ToolCalls, toolCall)
	}
	return Response{Message: message, Usage: clock.fill(usage)}, nil
}

// mergeToolCallDeltas assembles tool calls that arrive in pieces: the first
// piece for an index carries id and name, later ones append to the arguments.
// A piece without index continues the last call unless it starts a new one.
func mergeToolCallDeltas(calls []*openAIToolCall, deltas []openAIToolCall, onDelta func(index int, name, args string)) []*openAIToolCall {
	for _, delta := range deltas {
		index := deltaIndex(delta, len(calls))
		onDelta(index, delta.Function.Name, delta.Function.Arguments)
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
		if len(delta.ExtraContent) > 0 {
			call.ExtraContent = delta.ExtraContent
		}
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
	effort := req.Effort
	if p.refused.refused(req.Model) {
		effort = ""
	}
	out := openAIRequest{Model: req.Model, MaxTokens: req.MaxTokens, ReasoningEffort: effort, ReturnProgress: p.PromptProgress, Stream: true}
	if p.PromptProgress && effort != "" {
		if effort == "none" {
			out.ChatTemplateKwargs, out.ReasoningEffort = map[string]any{"enable_thinking": false}, ""
		} else {
			out.ChatTemplateKwargs = map[string]any{"reasoning_effort": effort}
		}
	}
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
	out := openAIMessage{Role: string(message.Role), Content: openAIContent(message), ToolCallID: message.ToolCallID, Reasoning: message.Reasoning}
	for _, call := range message.ToolCalls {
		var wire openAIToolCall
		wire.ID = call.ID
		wire.Type = "function"
		wire.Function.Name = call.Name
		wire.Function.Arguments = string(call.Args)
		wire.ExtraContent = call.Extra
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

// StatusError is a server that answered listing its models with an error.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return "listing models: " + e.Status
}

func (p *OpenAI) headers(ctx context.Context) (map[string]string, error) {
	headers := map[string]string{}
	for name, value := range p.Headers {
		headers[name] = value
	}
	key := p.APIKey
	if p.Token != nil {
		token, err := p.Token(ctx)
		if err != nil {
			return nil, err
		}
		key = token
	}
	if key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	return headers, nil
}

func (p *OpenAI) url(path string) string {
	return strings.TrimRight(p.BaseURL, "/") + path
}
