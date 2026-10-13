package llm

// Test Traceability Table
// | ID    | Category | Description                                                       | Priority |
// | LA-01 | Happy    | Streams text, assembles a tool_use and counts cached input tokens | P0       |
// | LA-02 | Error    | An error event ends the stream with its message                   | P1       |
// | LA-03 | Happy    | Folds consecutive tool results into one user message              | P0       |
// | LA-04 | Happy    | Puts cache breakpoints on system, last tool and last message      | P1       |
// | LA-05 | Edge     | An empty assistant message still carries a content block          | P2       |
// | LA-06 | Happy    | Sends the API key and version headers                             | P0       |
// | LA-07 | Error    | A tool_use cut at max_tokens keeps the history serializable       | P0       |
// | LA-08 | Edge     | Whitespace-only assistant text is not sent back                   | P1       |

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicStream(t *testing.T) {
	t.Run("LA-01 streams text, assembles a tool_use and counts cached input tokens", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me look"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"read"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}`,
			`{"type":"message_delta","usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`,
		}, nil)
		provider := &Anthropic{BaseURL: server.URL, APIKey: "k"}

		var texts []string
		response, err := provider.Stream(context.Background(), Request{Model: "m"}, func(event Event) {
			texts = append(texts, event.Text)
		})

		if err != nil {
			t.Fatal(err)
		}
		if response.Message.Text != "Let me look" || len(texts) != 1 {
			t.Errorf("text = %q, deltas = %v", response.Message.Text, texts)
		}
		calls := response.Message.ToolCalls
		if len(calls) != 1 || calls[0].ID != "t1" || calls[0].Name != "read" || string(calls[0].Args) != `{"path":"go.mod"}` {
			t.Errorf("calls = %+v", calls)
		}
		if usage := response.Usage; usage.InputTokens != 125 || usage.OutputTokens != 9 {
			t.Errorf("usage = %+v", response.Usage)
		}
	})

	t.Run("LA-02 an error event ends the stream with its message", func(t *testing.T) {
		server := sseServer(t, []string{`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`}, nil)
		provider := &Anthropic{BaseURL: server.URL, APIKey: "k"}

		_, err := provider.Stream(context.Background(), Request{Model: "m"}, func(Event) {})

		if err == nil || !strings.Contains(err.Error(), "Overloaded") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("LA-06 sends the API key and version headers", func(t *testing.T) {
		var key, version string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, version = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
			w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
		}))
		defer server.Close()

		_, err := (&Anthropic{BaseURL: server.URL, APIKey: "secret"}).Stream(context.Background(), Request{Model: "m"}, func(Event) {})

		if err != nil || key != "secret" || version == "" {
			t.Errorf("key = %q version = %q err = %v", key, version, err)
		}
	})
}

func TestBuildAnthropicRequest(t *testing.T) {
	request := Request{
		Model:  "m",
		System: "be brief",
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Text: "checking", ToolCalls: []ToolCall{
				{ID: "a", Name: "ls", Args: json.RawMessage(`{}`)},
				{ID: "b", Name: "ls", Args: json.RawMessage(`{}`)},
			}},
			{Role: RoleTool, ToolCallID: "a", Text: "x"},
			{Role: RoleTool, ToolCallID: "b", Text: "y", IsError: true},
		},
		Tools: []ToolSpec{
			{Name: "ls", Schema: json.RawMessage(`{"type":"object"}`)},
			{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	built := buildAnthropicRequest(request)

	t.Run("LA-03 folds consecutive tool results into one user message", func(t *testing.T) {
		if len(built.Messages) != 3 {
			t.Fatalf("messages = %+v", built.Messages)
		}
		results := built.Messages[2]
		if results.Role != "user" || len(results.Content) != 2 {
			t.Fatalf("results = %+v", results)
		}
		if results.Content[0].ToolUseID != "a" || results.Content[1].ToolUseID != "b" || !results.Content[1].IsError {
			t.Errorf("results = %+v", results.Content)
		}
		if built.MaxTokens != anthropicDefaultMaxTokens {
			t.Errorf("max tokens = %d", built.MaxTokens)
		}
	})

	t.Run("LA-04 puts cache breakpoints on system, last tool and last message", func(t *testing.T) {
		if built.System[0].CacheControl == nil {
			t.Error("system has no cache breakpoint")
		}
		if built.Tools[0].CacheControl != nil || built.Tools[1].CacheControl == nil {
			t.Error("only the last tool should carry the breakpoint")
		}
		last := built.Messages[2].Content
		if last[1].CacheControl == nil || last[0].CacheControl != nil {
			t.Error("only the last block of the last message should carry the breakpoint")
		}
	})

	t.Run("LA-05 an empty assistant message still carries a content block", func(t *testing.T) {
		messages := toAnthropicMessages([]Message{{Role: RoleUser, Text: "hi"}, {Role: RoleAssistant}})
		if len(messages[1].Content) != 1 {
			t.Errorf("content = %+v", messages[1].Content)
		}
	})
}

func TestAnthropicEdges(t *testing.T) {
	t.Run("LA-07 a tool_use cut at max_tokens keeps the history serializable", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"write"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a\",\"content\":\"ab"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":8192}}`,
			`{"type":"message_stop"}`,
		}, nil)

		response, err := (&Anthropic{BaseURL: server.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "m"}, func(Event) {})

		if err != nil {
			t.Fatal(err)
		}
		call := response.Message.ToolCalls[0]
		if call.ArgsError == "" {
			t.Errorf("call = %+v", call)
		}
		history := []Message{{Role: RoleUser, Text: "write"}, response.Message, {Role: RoleTool, ToolCallID: "t1", Text: call.ArgsError, IsError: true}}
		if _, err := json.Marshal(buildAnthropicRequest(Request{Model: "m", Messages: history})); err != nil {
			t.Errorf("history no longer serializes: %v", err)
		}
	})

	t.Run("LA-08 whitespace-only assistant text is not sent back", func(t *testing.T) {
		messages := toAnthropicMessages([]Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Text: "\n\n", ToolCalls: []ToolCall{{ID: "a", Name: "ls", Args: json.RawMessage(`{}`)}}},
		})

		if blocks := messages[1].Content; len(blocks) != 1 || blocks[0].Type != "tool_use" {
			t.Errorf("blocks = %+v", blocks)
		}
	})
}
