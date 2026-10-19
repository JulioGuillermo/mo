package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	codexOriginator = "codex_cli_rs"
	// codexClientVersion is what the model list asks for; the server only
	// uses it to leave out the models that client is too old for.
	codexClientVersion = "0.152.0"
	// responsesFallbackInstructions stands in for a system prompt, which the
	// ChatGPT backend refuses to be without.
	responsesFallbackInstructions = "You are a helpful coding assistant."
)

// Responses talks the Responses API as Codex does with a ChatGPT sign-in:
// the backend behind chatgpt.com/backend-api/codex, which takes the
// instructions apart from the conversation and keeps no state.
type Responses struct {
	BaseURL string
	Client  *http.Client
	// Token gives the bearer at each request: the ChatGPT sign-in's access
	// token, which also says which account it is.
	Token func(ctx context.Context) (string, error)
}

type respContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type respItem struct {
	Type      string        `json:"type"`
	Role      string        `json:"role,omitempty"`
	Content   []respContent `json:"content,omitempty"`
	CallID    string        `json:"call_id,omitempty"`
	Name      string        `json:"name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Output    *string       `json:"output,omitempty"`
}

type respTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type respRequest struct {
	Model             string     `json:"model"`
	Instructions      string     `json:"instructions"`
	Input             []respItem `json:"input"`
	Tools             []respTool `json:"tools"`
	ToolChoice        string     `json:"tool_choice"`
	ParallelToolCalls bool       `json:"parallel_tool_calls"`
	Reasoning         struct {
		Effort  string `json:"effort,omitempty"`
		Summary string `json:"summary"`
	} `json:"reasoning"`
	Store  bool `json:"store"`
	Stream bool `json:"stream"`
}

type respEvent struct {
	Type        string `json:"type"`
	Delta       string `json:"delta"`
	OutputIndex int    `json:"output_index"`
	Item        struct {
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Response struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
	Message string `json:"message"`
}

func (p *Responses) Stream(ctx context.Context, req Request, onEvent func(Event)) (Response, error) {
	clock := newSpeedClock()
	headers, err := p.headers(ctx)
	if err != nil {
		return Response{}, err
	}
	headers["Accept"] = "text/event-stream"
	resp, err := postJSON(ctx, clientOrDefault(p.Client), p.url("/responses"), headers, buildResponsesRequest(req))
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	var text, reasoning strings.Builder
	var calls []*ToolCall
	slots := map[int]int{}
	var usage Usage
	cutOff, refused := false, false
	err = readSSE(resp.Body, func(data []byte) error {
		var event respEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("bad stream event: %w", err)
		}
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta":
			clock.piece()
			text.WriteString(event.Delta)
			onEvent(Event{Kind: EventText, Text: event.Delta})
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			clock.piece()
			reasoning.WriteString(event.Delta)
			onEvent(Event{Kind: EventReasoning, Text: event.Delta})
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				slots[event.OutputIndex] = len(calls)
				calls = append(calls, &ToolCall{ID: event.Item.CallID, Name: event.Item.Name})
				onEvent(Event{Kind: EventToolCall, Call: len(calls) - 1, Name: event.Item.Name, ID: event.Item.CallID})
			}
		case "response.function_call_arguments.delta":
			if slot, ok := slots[event.OutputIndex]; ok {
				clock.piece()
				calls[slot].Args = append(calls[slot].Args, event.Delta...)
				onEvent(Event{Kind: EventToolCall, Call: slot, Text: event.Delta})
			}
		case "response.output_item.done":
			if slot, ok := slots[event.OutputIndex]; ok && event.Item.Type == "function_call" {
				calls[slot].Args = json.RawMessage(event.Item.Arguments)
			}
		case "response.completed", "response.incomplete":
			if event.Response.Usage != nil {
				usage.InputTokens, usage.OutputTokens = event.Response.Usage.InputTokens, event.Response.Usage.OutputTokens
			}
			if details := event.Response.IncompleteDetails; details != nil {
				cutOff = details.Reason == "max_output_tokens"
				refused = details.Reason == "content_filter"
			}
			return errStopSSE
		case "response.failed":
			message := "the response failed"
			if event.Response.Error != nil {
				message = event.Response.Error.Message
			}
			return fmt.Errorf("provider error: %s", message)
		case "error":
			return fmt.Errorf("provider error: %s", event.Message)
		}
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	message := Message{Role: RoleAssistant, Text: text.String(), Reasoning: reasoning.String()}
	for i, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		message.ToolCalls = append(message.ToolCalls, newToolCall(id, call.Name, string(call.Args)))
	}
	if cutOff && len(calls) == 0 {
		message.Text += cutOffTextNote
		onEvent(Event{Kind: EventText, Text: cutOffTextNote})
	}
	if refused {
		noteRefusal(&message, onEvent)
	}
	return Response{Message: message, Usage: clock.fill(usage)}, nil
}

func buildResponsesRequest(req Request) respRequest {
	out := respRequest{Model: req.Model, Instructions: req.System, ToolChoice: "auto", Store: false, Stream: true, Input: []respItem{}, Tools: []respTool{}}
	if strings.TrimSpace(out.Instructions) == "" {
		out.Instructions = responsesFallbackInstructions
	}
	out.Reasoning.Effort, out.Reasoning.Summary = req.Effort, "auto"
	for _, spec := range req.Tools {
		out.Tools = append(out.Tools, respTool{Type: "function", Name: spec.Name, Description: spec.Description, Parameters: spec.Schema})
	}
	for _, message := range req.Messages {
		out.Input = append(out.Input, toResponsesItems(message)...)
	}
	return out
}

func toResponsesItems(message Message) []respItem {
	switch message.Role {
	case RoleAssistant:
		var items []respItem
		if strings.TrimSpace(message.Text) != "" {
			items = append(items, respItem{Type: "message", Role: "assistant", Content: []respContent{{Type: "output_text", Text: message.Text}}})
		}
		for _, call := range message.ToolCalls {
			items = append(items, respItem{Type: "function_call", CallID: call.ID, Name: call.Name, Arguments: string(call.Args)})
		}
		return items
	case RoleTool:
		output := message.Text
		return []respItem{{Type: "function_call_output", CallID: message.ToolCallID, Output: &output}}
	}
	content := []respContent{{Type: "input_text", Text: message.Text}}
	for _, image := range message.Images {
		content = append(content, respContent{Type: "input_image", ImageURL: "data:" + image.MIME + ";base64," + base64.StdEncoding.EncodeToString(image.Data)})
	}
	return []respItem{{Type: "message", Role: "user", Content: content}}
}

// ListModels returns the models the account can use, from the backend's own
// list.
func (p *Responses) ListModels(ctx context.Context) ([]string, error) {
	headers, err := p.headers(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url("/models?client_version="+codexClientVersion), nil)
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
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	var ids []string
	for _, model := range list.Models {
		if model.Slug != "" && model.Visibility != "hide" {
			ids = append(ids, model.Slug)
		}
	}
	return ids, nil
}

// headers are the ones Codex sends: the bearer, and the ChatGPT account the
// token belongs to.
func (p *Responses) headers(ctx context.Context) (map[string]string, error) {
	token, err := p.Token(ctx)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"originator":    codexOriginator,
		"User-Agent":    codexOriginator + "/" + codexClientVersion,
		"OpenAI-Beta":   "responses=experimental",
	}
	if account := chatGPTAccount(token); account != "" {
		headers["chatgpt-account-id"] = account
	}
	return headers, nil
}

// chatGPTAccount is the account a ChatGPT access token is for: a claim of
// the token, which is a JWT. Empty when it does not say.
func chatGPTAccount(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Auth.AccountID
}

func (p *Responses) url(path string) string {
	return strings.TrimRight(p.BaseURL, "/") + path
}
