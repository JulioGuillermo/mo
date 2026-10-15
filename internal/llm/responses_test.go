package llm

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | RS-01 | Happy    | Streams text and reasoning summaries, and reports usage                  | P0       |
// | RS-02 | Happy    | A function call comes with its arguments, and its result goes back       | P0       |
// | RS-03 | Edge     | Instructions are never empty and the request keeps no state              | P1       |
// | RS-04 | Happy    | The bearer, the originator and the account of the token are sent         | P0       |
// | RS-05 | Error    | A failed response is an error with its message                           | P1       |
// | RS-06 | Happy    | ListModels returns the visible models                                    | P2       |
// | RS-07 | Happy    | Claude Code's requests carry its beta header and system line first       | P0       |

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recorded struct {
	header http.Header
	path   string
	body   []byte
}

func responsesServer(t *testing.T, events []string, got *recorded) *Responses {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header, got.path = r.Header, r.URL.Path
		got.body, _ = io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-a","visibility":"list"},{"slug":"gpt-b","visibility":"hide"}]}`))
			return
		}
		for _, event := range events {
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
		}
	}))
	t.Cleanup(server.Close)
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`))
	return &Responses{BaseURL: server.URL, Token: func(context.Context) (string, error) { return "h." + claims + ".s", nil }}
}

func TestResponsesStream(t *testing.T) {
	var got recorded
	provider := responsesServer(t, []string{
		`{"type":"response.reasoning_summary_text.delta","delta":"hmm"}`,
		`{"type":"response.output_text.delta","delta":"po"}`,
		`{"type":"response.output_text.delta","delta":"ng"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":2}}}`,
	}, &got)
	resp, err := provider.Stream(context.Background(), Request{Model: "gpt-5.6", Messages: []Message{{Role: RoleUser, Text: "ping"}}}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Text != "pong" || resp.Message.Reasoning != "hmm" || resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 2 {
		t.Fatalf("response %+v", resp)
	}
	var sent map[string]any
	_ = json.Unmarshal(got.body, &sent)
	if got.path != "/responses" || sent["instructions"] != responsesFallbackInstructions || sent["store"] != false || sent["stream"] != true {
		t.Fatalf("path %s sent %v", got.path, sent)
	}
	if got.header.Get("Authorization") == "" || got.header.Get("chatgpt-account-id") != "acct-1" || got.header.Get("originator") != codexOriginator {
		t.Fatalf("headers %v", got.header)
	}
}

func TestResponsesToolCalls(t *testing.T) {
	var got recorded
	provider := responsesServer(t, []string{
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"c1","name":"read"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\""}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":":\"a\"}"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"c1","name":"read","arguments":"{\"path\":\"a\"}"}}`,
		`{"type":"response.completed","response":{}}`,
	}, &got)
	var pieces []Event
	resp, err := provider.Stream(context.Background(), Request{Model: "m", System: "sys", Messages: []Message{{Role: RoleUser, Text: "go"}}}, func(e Event) { pieces = append(pieces, e) })
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Name != "read" || string(calls[0].Args) != `{"path":"a"}` || len(pieces) != 3 {
		t.Fatalf("calls %+v pieces %v", calls, pieces)
	}
	history := []Message{
		{Role: RoleUser, Text: "go", Images: []Image{{MIME: "image/png", Data: []byte("x")}}},
		{Role: RoleAssistant, Text: "ok", ToolCalls: calls},
		{Role: RoleTool, ToolCallID: "c1", Text: "contents"},
	}
	if _, err := provider.Stream(context.Background(), Request{Model: "m", System: "sys", Messages: history, Tools: []ToolSpec{{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}}}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Instructions string     `json:"instructions"`
		Input        []respItem `json:"input"`
		Tools        []respTool `json:"tools"`
	}
	_ = json.Unmarshal(got.body, &sent)
	types := []string{}
	for _, item := range sent.Input {
		types = append(types, item.Type)
	}
	if sent.Instructions != "sys" || strings.Join(types, ",") != "message,message,function_call,function_call_output" || len(sent.Input[0].Content) != 2 || sent.Input[0].Content[1].Type != "input_image" {
		t.Fatalf("input %v", types)
	}
	if sent.Input[3].CallID != "c1" || *sent.Input[3].Output != "contents" || len(sent.Tools) != 1 || sent.Tools[0].Type != "function" {
		t.Fatalf("sent %+v", sent)
	}
}

func TestResponsesFailure(t *testing.T) {
	var got recorded
	provider := responsesServer(t, []string{`{"type":"response.failed","response":{"error":{"message":"no quota"}}}`}, &got)
	_, err := provider.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "x"}}}, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "no quota") {
		t.Fatalf("err %v", err)
	}
}

func TestResponsesListModels(t *testing.T) {
	var got recorded
	models, err := responsesServer(t, nil, &got).ListModels(context.Background())
	if err != nil || strings.Join(models, ",") != "gpt-a" || !strings.Contains(got.path, "/models") {
		t.Fatalf("models %v err %v", models, err)
	}
}

func TestAnthropicSubscription(t *testing.T) {
	var got recorded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header = r.Header
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	provider := &Anthropic{BaseURL: server.URL, Subscription: true, Token: func(context.Context) (string, error) { return "tok", nil }}
	_, _ = provider.Stream(context.Background(), Request{Model: "m", System: "mine", Messages: []Message{{Role: RoleUser, Text: "x"}}}, func(Event) {})
	var sent struct {
		System []anthropicBlock `json:"system"`
	}
	_ = json.Unmarshal(got.body, &sent)
	if got.header.Get("Authorization") != "Bearer tok" || got.header.Get("anthropic-beta") != claudeCodeBeta || got.header.Get("x-api-key") != "" {
		t.Fatalf("headers %v", got.header)
	}
	if len(sent.System) != 3 || sent.System[0].Text != claudeCodeSystem || sent.System[1].Text != claudeCodeBridge || sent.System[2].Text != "mine" {
		t.Fatalf("system %+v", sent.System)
	}

	provider.NoPreamble = true
	_, _ = provider.Stream(context.Background(), Request{Model: "m", System: "mine", Messages: []Message{{Role: RoleUser, Text: "x"}}}, func(Event) {})
	sent.System = nil
	_ = json.Unmarshal(got.body, &sent)
	if len(sent.System) != 1 || sent.System[0].Text != "mine" {
		t.Fatalf("without the preamble, system %+v", sent.System)
	}
}
