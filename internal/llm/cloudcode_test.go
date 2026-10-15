package llm

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | CC-01 | Happy    | Wraps the request with project and model, and streams text/thoughts   | P0       |
// | CC-02 | Happy    | A function call keeps its thought signature and goes back with it     | P0       |
// | CC-03 | Edge     | Results of several calls go in one user turn, named by their call     | P1       |
// | CC-04 | Edge     | Tool schemas keep only the words Gemini takes                         | P0       |
// | CC-05 | Happy    | ListModels returns the text models sorted, without the others         | P2       |

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cloudCodeServer(t *testing.T, chunks []string, inspect func(body []byte)) *CloudCode {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(r.URL.Path, "loadCodeAssist"):
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"my-project"}`))
		case strings.Contains(r.URL.Path, "fetchAvailableModels"):
			_, _ = w.Write([]byte(`{"models":{"gemini-b":{},"chat_23310":{},"claude-a":{}}}`))
		default:
			if inspect != nil {
				inspect(body)
			}
			for _, chunk := range chunks {
				_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			}
		}
	}))
	t.Cleanup(server.Close)
	return &CloudCode{BaseURL: server.URL, Token: func(context.Context) (string, error) { return "tok", nil }}
}

func TestCloudCodeStream(t *testing.T) {
	var sent ccEnvelope
	provider := cloudCodeServer(t, []string{
		`{"response":{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]}}]}}`,
		`{"response":{"candidates":[{"content":{"parts":[{"text":"pong"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":3}}}`,
	}, func(body []byte) { _ = json.Unmarshal(body, &sent) })
	var events []Event
	resp, err := provider.Stream(context.Background(), Request{Model: "gemini-3-flash", System: "be brief", Messages: []Message{{Role: RoleUser, Text: "ping"}}}, func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Text != "pong" || resp.Message.Reasoning != "thinking" || resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("response = %+v", resp)
	}
	if sent.Project != "my-project" || sent.Model != "gemini-3-flash" || sent.RequestType != "agent" || len(events) != 2 {
		t.Fatalf("sent %+v, events %v", sent, events)
	}
	system := sent.Request.SystemInstruction
	if system.Role != "user" || len(system.Parts) != 3 || system.Parts[0].Text != cloudCodePreamble || system.Parts[1].Text != cloudCodeBridge || system.Parts[2].Text != "be brief" {
		t.Fatalf("system = %+v", system)
	}
}

func TestCloudCodeToolCalls(t *testing.T) {
	var sent ccEnvelope
	provider := cloudCodeServer(t, []string{
		`{"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read","args":{"path":"a"}},"thoughtSignature":"sig"}]}}]}}`,
	}, func(body []byte) { _ = json.Unmarshal(body, &sent) })
	resp, err := provider.Stream(context.Background(), Request{Model: "gemini-3-flash", Messages: []Message{{Role: RoleUser, Text: "go"}}}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.Message.ToolCalls
	if len(calls) != 1 || calls[0].Name != "read" || calls[0].ID != "call_0" || !strings.Contains(string(calls[0].Extra), "sig") {
		t.Fatalf("calls = %+v", calls)
	}
	history := []Message{
		{Role: RoleUser, Text: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{calls[0], {ID: "call_1", Name: "ls", Args: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "call_0", Text: "one"},
		{Role: RoleTool, ToolCallID: "call_1", Text: "two", IsError: true},
	}
	if _, err := provider.Stream(context.Background(), Request{Model: "gemini-3-flash", Messages: history}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	contents := sent.Request.Contents
	if len(contents) != 3 || contents[1].Role != "model" || contents[2].Role != "user" || len(contents[2].Parts) != 2 {
		t.Fatalf("contents = %+v", contents)
	}
	if contents[1].Parts[0].ThoughtSignature != "sig" || contents[1].Parts[1].ThoughtSignature != skipSignature {
		t.Fatalf("signatures = %q, %q", contents[1].Parts[0].ThoughtSignature, contents[1].Parts[1].ThoughtSignature)
	}
	first, second := contents[2].Parts[0].FunctionResponse, contents[2].Parts[1].FunctionResponse
	if first.Name != "read" || first.Response["output"] != "one" || second.Name != "ls" || second.Response["error"] != "two" {
		t.Fatalf("responses = %+v %+v", first, second)
	}
}

func TestGeminiSchema(t *testing.T) {
	raw := json.RawMessage(`{"$schema":"x","type":"object","additionalProperties":false,"properties":{"opts":{"type":["object","null"],"propertyNames":{"type":"string"},"properties":{"default":{"type":"string","default":"a"}}},"list":{"type":"array","items":{"type":"string","examples":["a"]}}},"required":["opts"]}`)
	got, _ := json.Marshal(geminiSchema(raw))
	want := `{"properties":{"list":{"items":{"type":"string"},"type":"array"},"opts":{"properties":{"default":{"type":"string"}},"type":"object"}},"required":["opts"],"type":"object"}`
	if string(got) != want {
		t.Fatalf("schema = %s", got)
	}
}

func TestCloudCodeListModels(t *testing.T) {
	provider := cloudCodeServer(t, nil, nil)
	models, err := provider.ListModels(context.Background())
	if err != nil || strings.Join(models, ",") != "claude-a,gemini-b" {
		t.Fatalf("models = %v, %v", models, err)
	}
}

func TestCloudCodeWithoutPreamble(t *testing.T) {
	t.Run("without the preamble the system prompt is mo's alone, and none at all without one", func(t *testing.T) {
		provider := &CloudCode{NoPreamble: true}

		alone := provider.buildRequest(Request{Model: "gemini-3-flash", System: "be brief"})
		empty := provider.buildRequest(Request{Model: "gemini-3-flash"})

		if alone.SystemInstruction == nil || len(alone.SystemInstruction.Parts) != 1 || alone.SystemInstruction.Parts[0].Text != "be brief" {
			t.Errorf("alone = %+v", alone.SystemInstruction)
		}
		if empty.SystemInstruction != nil {
			t.Errorf("empty = %+v", empty.SystemInstruction)
		}
	})
}
