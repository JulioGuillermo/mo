package llm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
)

const (
	// CloudCodeProject is the project Antigravity uses when the account has
	// none of its own.
	cloudCodeFallbackProject = "rising-fact-p41fc"
	cloudCodeVersion         = "1.18.3"
	// cloudCodePreamble is what Antigravity's own requests start their
	// system instruction with.
	cloudCodePreamble = "You are Antigravity, a powerful agentic AI coding assistant designed by the Google DeepMind team working on Advanced Agentic Coding."
	// cloudCodeBridge follows the preamble, so the model does not take
	// the two openings for two identities.
	cloudCodeBridge = "Here you run inside mo, a coding agent in the user's terminal: the instructions that follow are mo's and take the place of Antigravity's."
	// skipSignature is what Gemini accepts in place of the thought signature
	// of a call it did not make.
	skipSignature = "skip_thought_signature_validator"
)

// CloudCode talks to Google's Cloud Code Assist API, which Antigravity and
// the Gemini CLI use: Gemini's own format inside an envelope that names the
// project, sent with the bearer of a Google sign-in.
type CloudCode struct {
	BaseURL string
	Client  *http.Client
	// Token gives the bearer at each request.
	Token func(ctx context.Context) (string, error)

	mu      sync.Mutex
	project string
	session string
}

type ccPart struct {
	Text             string  `json:"text,omitempty"`
	Thought          bool    `json:"thought,omitempty"`
	ThoughtSignature string  `json:"thoughtSignature,omitempty"`
	InlineData       *ccBlob `json:"inlineData,omitempty"`
	FunctionCall     *ccCall `json:"functionCall,omitempty"`
	FunctionResponse *struct {
		ID       string         `json:"id,omitempty"`
		Name     string         `json:"name"`
		Response map[string]any `json:"response"`
	} `json:"functionResponse,omitempty"`
}

type ccBlob struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type ccCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type ccContent struct {
	Role  string   `json:"role,omitempty"`
	Parts []ccPart `json:"parts"`
}

type ccRequest struct {
	Contents          []ccContent `json:"contents"`
	SystemInstruction *ccContent  `json:"systemInstruction,omitempty"`
	Tools             []struct {
		FunctionDeclarations []ccDeclaration `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
	GenerationConfig struct {
		MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
		ThinkingConfig  *struct {
			IncludeThoughts bool   `json:"includeThoughts"`
			ThinkingLevel   string `json:"thinkingLevel,omitempty"`
		} `json:"thinkingConfig,omitempty"`
	} `json:"generationConfig"`
	SessionID string `json:"sessionId"`
}

type ccDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type ccEnvelope struct {
	Project     string    `json:"project"`
	Model       string    `json:"model"`
	Request     ccRequest `json:"request"`
	RequestType string    `json:"requestType"`
	UserAgent   string    `json:"userAgent"`
	RequestID   string    `json:"requestId"`
}

type ccChunk struct {
	Response struct {
		Candidates []struct {
			Content      ccContent `json:"content"`
			FinishReason string    `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	} `json:"response"`
}

func (p *CloudCode) Stream(ctx context.Context, req Request, onEvent func(Event)) (Response, error) {
	clock := newSpeedClock()
	headers, project, err := p.prepare(ctx)
	if err != nil {
		return Response{}, err
	}
	body := ccEnvelope{
		Project: project, Model: req.Model, Request: p.buildRequest(req),
		RequestType: "agent", UserAgent: "antigravity", RequestID: "agent-" + newUUID(),
	}
	resp, err := postJSON(ctx, clientOrDefault(p.Client), p.url("/v1internal:streamGenerateContent?alt=sse"), headers, body)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	var text, reasoning strings.Builder
	var calls []ToolCall
	var usage Usage
	cutOff := false
	err = readSSE(resp.Body, func(data []byte) error {
		var chunk ccChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("bad stream chunk: %w", err)
		}
		if meta := chunk.Response.UsageMetadata; meta != nil {
			usage.InputTokens, usage.OutputTokens = meta.PromptTokenCount, meta.CandidatesTokenCount+meta.ThoughtsTokenCount
		}
		for _, candidate := range chunk.Response.Candidates {
			for _, part := range candidate.Content.Parts {
				clock.piece()
				switch {
				case part.FunctionCall != nil:
					call := newToolCall(part.FunctionCall.ID, part.FunctionCall.Name, string(part.FunctionCall.Args))
					if call.ID == "" {
						call.ID = fmt.Sprintf("call_%d", len(calls))
					}
					if part.ThoughtSignature != "" {
						call.Extra, _ = json.Marshal(map[string]string{"thoughtSignature": part.ThoughtSignature})
					}
					onEvent(Event{Kind: EventToolCall, Call: len(calls), Name: call.Name, Text: string(call.Args)})
					calls = append(calls, call)
				case part.Thought:
					reasoning.WriteString(part.Text)
					onEvent(Event{Kind: EventReasoning, Text: part.Text})
				case part.Text != "":
					text.WriteString(part.Text)
					onEvent(Event{Kind: EventText, Text: part.Text})
				}
			}
			if candidate.FinishReason == "MAX_TOKENS" {
				cutOff = true
			}
		}
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	message := Message{Role: RoleAssistant, Text: text.String(), Reasoning: reasoning.String(), ToolCalls: calls}
	if cutOff && len(calls) == 0 {
		message.Text += cutOffTextNote
		onEvent(Event{Kind: EventText, Text: cutOffTextNote})
	}
	return Response{Message: message, Usage: clock.fill(usage)}, nil
}

func (p *CloudCode) buildRequest(req Request) ccRequest {
	p.mu.Lock()
	if p.session == "" {
		p.session = "-" + newUUID()
	}
	session := p.session
	p.mu.Unlock()

	out := ccRequest{SessionID: session}
	out.SystemInstruction = &ccContent{Role: "user", Parts: []ccPart{{Text: cloudCodePreamble}, {Text: cloudCodeBridge}}}
	if req.System != "" {
		out.SystemInstruction.Parts = append(out.SystemInstruction.Parts, ccPart{Text: req.System})
	}
	out.GenerationConfig.MaxOutputTokens = req.MaxTokens
	if strings.Contains(req.Model, "gemini-3") {
		out.GenerationConfig.ThinkingConfig = &struct {
			IncludeThoughts bool   `json:"includeThoughts"`
			ThinkingLevel   string `json:"thinkingLevel,omitempty"`
		}{IncludeThoughts: true, ThinkingLevel: cloudCodeLevel(req.Effort)}
	}
	names := map[string]string{}
	for _, message := range req.Messages {
		for _, call := range message.ToolCalls {
			names[call.ID] = call.Name
		}
		out.Contents = appendContent(out.Contents, toCloudCodeContent(message, names))
	}
	if len(req.Tools) > 0 {
		var declarations []ccDeclaration
		for _, spec := range req.Tools {
			declarations = append(declarations, ccDeclaration{Name: spec.Name, Description: spec.Description, Parameters: geminiSchema(spec.Schema)})
		}
		out.Tools = append(out.Tools, struct {
			FunctionDeclarations []ccDeclaration `json:"functionDeclarations"`
		}{declarations})
	}
	return out
}

// cloudCodeLevel is Gemini's thinking level for an effort; empty leaves the
// model's default.
func cloudCodeLevel(effort string) string {
	switch effort {
	case "none":
		return "minimal"
	case "minimal", "low", "medium", "high":
		return effort
	case "xhigh", "max":
		return "high"
	}
	return ""
}

// appendContent joins a message to the one before when they have the same
// role, as the results of several calls go together in one turn.
func appendContent(contents []ccContent, next ccContent) []ccContent {
	if last := len(contents) - 1; last >= 0 && contents[last].Role == next.Role {
		contents[last].Parts = append(contents[last].Parts, next.Parts...)
		return contents
	}
	return append(contents, next)
}

func toCloudCodeContent(message Message, names map[string]string) ccContent {
	switch message.Role {
	case RoleAssistant:
		content := ccContent{Role: "model"}
		if message.Text != "" {
			content.Parts = append(content.Parts, ccPart{Text: message.Text})
		}
		for _, call := range message.ToolCalls {
			signature := skipSignature
			var extra struct {
				ThoughtSignature string `json:"thoughtSignature"`
			}
			if json.Unmarshal(call.Extra, &extra) == nil && extra.ThoughtSignature != "" {
				signature = extra.ThoughtSignature
			}
			content.Parts = append(content.Parts, ccPart{FunctionCall: &ccCall{ID: call.ID, Name: call.Name, Args: call.Args}, ThoughtSignature: signature})
		}
		if len(content.Parts) == 0 {
			content.Parts = []ccPart{{Text: " "}}
		}
		return content
	case RoleTool:
		key := "output"
		if message.IsError {
			key = "error"
		}
		part := ccPart{FunctionResponse: &struct {
			ID       string         `json:"id,omitempty"`
			Name     string         `json:"name"`
			Response map[string]any `json:"response"`
		}{ID: message.ToolCallID, Name: names[message.ToolCallID], Response: map[string]any{key: message.Text}}}
		return ccContent{Role: "user", Parts: []ccPart{part}}
	}
	content := ccContent{Role: "user", Parts: []ccPart{{Text: message.Text}}}
	for _, image := range message.Images {
		content.Parts = append(content.Parts, ccPart{InlineData: &ccBlob{MIMEType: image.MIME, Data: base64.StdEncoding.EncodeToString(image.Data)}})
	}
	return content
}

// schemaKeys are the words of a JSON Schema that Gemini's function
// declarations take; it refuses any other.
var schemaKeys = []string{"type", "format", "description", "nullable", "enum", "items", "properties", "required", "anyOf", "minItems", "maxItems", "minimum", "maximum"}

// geminiSchema turns a JSON Schema into the subset Gemini takes: only the
// words it knows, and one type where the schema allows several.
func geminiSchema(raw json.RawMessage) any {
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return nil
	}
	return cleanSchema(schema)
}

func cleanSchema(schema map[string]any) map[string]any {
	clean := map[string]any{}
	for _, key := range schemaKeys {
		if value, ok := schema[key]; ok {
			clean[key] = value
		}
	}
	if types, ok := clean["type"].([]any); ok {
		clean["type"] = "string"
		for _, each := range types {
			if name, _ := each.(string); name != "null" {
				clean["type"] = name
				break
			}
		}
	}
	if properties, ok := clean["properties"].(map[string]any); ok {
		cleaned := map[string]any{}
		for name, each := range properties {
			sub, _ := each.(map[string]any)
			cleaned[name] = cleanSchema(sub)
		}
		clean["properties"] = cleaned
	}
	if items, ok := clean["items"].(map[string]any); ok {
		clean["items"] = cleanSchema(items)
	}
	if options, ok := clean["anyOf"].([]any); ok {
		var cleaned []any
		for _, each := range options {
			sub, _ := each.(map[string]any)
			cleaned = append(cleaned, cleanSchema(sub))
		}
		clean["anyOf"] = cleaned
	}
	return clean
}

// prepare gives the headers and the project of a request.
func (p *CloudCode) prepare(ctx context.Context) (map[string]string, string, error) {
	token, err := p.Token(ctx)
	if err != nil {
		return nil, "", err
	}
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"User-Agent":    fmt.Sprintf("antigravity/%s %s/%s", cloudCodeVersion, runtime.GOOS, runtime.GOARCH),
	}
	project, err := p.projectFor(ctx, headers)
	return headers, project, err
}

// projectFor is the account's own project, asked once; the one Antigravity
// falls back on when it has none.
func (p *CloudCode) projectFor(ctx context.Context, headers map[string]string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.project != "" {
		return p.project, nil
	}
	loadHeaders := map[string]string{"Authorization": headers["Authorization"], "User-Agent": "google-api-nodejs-client/9.15.1"}
	body := map[string]any{"metadata": map[string]string{"ideType": "ANTIGRAVITY", "platform": "PLATFORM_UNSPECIFIED", "pluginType": "GEMINI"}}
	p.project = cloudCodeFallbackProject
	resp, err := postJSON(ctx, clientOrDefault(p.Client), p.url("/v1internal:loadCodeAssist"), loadHeaders, body)
	if err != nil {
		return p.project, nil
	}
	defer resp.Body.Close()
	var loaded struct {
		Project json.RawMessage `json:"cloudaicompanionProject"`
	}
	if json.NewDecoder(resp.Body).Decode(&loaded) != nil {
		return p.project, nil
	}
	var id string
	var object struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(loaded.Project, &id) == nil && id != "" {
		p.project = id
	} else if json.Unmarshal(loaded.Project, &object) == nil && object.ID != "" {
		p.project = object.ID
	}
	return p.project, nil
}

// ListModels returns the models the account can use.
func (p *CloudCode) ListModels(ctx context.Context) ([]string, error) {
	headers, project, err := p.prepare(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := postJSON(ctx, clientOrDefault(p.Client), p.url("/v1internal:fetchAvailableModels"), headers, map[string]string{"project": project})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	var ids []string
	for id := range list.Models {
		if strings.HasPrefix(id, "gemini") || strings.HasPrefix(id, "claude") || strings.HasPrefix(id, "gpt") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

func (p *CloudCode) url(path string) string {
	return strings.TrimRight(p.BaseURL, "/") + path
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
