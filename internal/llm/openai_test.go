package llm

// Test Traceability Table
// | ID    | Category | Description                                                     | Priority |
// | LO-01 | Happy    | Streams text and reasoning deltas and reports usage             | P0       |
// | LO-02 | Happy    | Assembles a tool call split across chunks by index              | P0       |
// | LO-03 | Edge     | A tool call with no arguments becomes {}                        | P1       |
// | LO-04 | Error    | A non-2xx answer is an error carrying the body                  | P1       |
// | LO-05 | Happy    | Sends system, tool results and tools in chat completions shape  | P0       |
// | LO-06 | Happy    | ListModels returns the ids in order                             | P1       |
// | LO-07 | Edge     | Fragments without index continue the last call                 | P1       |
// | LO-08 | Error    | Cut-off arguments become {} with an ArgsError                   | P0       |
// | LO-09 | Edge     | finish_reason length on text adds a cut-off note                | P2       |
// | LO-10 | Error    | A keep-alive connection the server closed is retried            | P0       |
// | LO-11 | Happy    | ContextWindow reads n_ctx from llama-server /props              | P1       |
// | LO-12 | Edge     | ContextWindow is 0 when the server has no /props                | P1       |
// | LO-13 | Error    | A request cut by the network on a new connection is sent again  | P1       |

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sseServer(t *testing.T, chunks []string, inspect func(body []byte)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if inspect != nil {
			inspect(body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOpenAIStream(t *testing.T) {
	t.Run("LO-01 streams text and reasoning deltas and reports usage", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"reasoning_content":"hmm"}}]}`,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
			`[DONE]`,
		}, nil)
		provider := &OpenAI{BaseURL: server.URL}

		var texts, reasoning []string
		response, err := provider.Stream(context.Background(), Request{Model: "m"}, func(event Event) {
			if event.Kind == EventText {
				texts = append(texts, event.Text)
			} else {
				reasoning = append(reasoning, event.Text)
			}
		})

		if err != nil {
			t.Fatal(err)
		}
		if response.Message.Text != "Hello" || strings.Join(texts, "|") != "Hel|lo" {
			t.Errorf("text = %q, deltas = %v", response.Message.Text, texts)
		}
		if strings.Join(reasoning, "") != "hmm" {
			t.Errorf("reasoning = %v", reasoning)
		}
		if response.Usage != (Usage{InputTokens: 12, OutputTokens: 3}) {
			t.Errorf("usage = %+v", response.Usage)
		}
	})

	t.Run("LO-02 assembles a tool call split across chunks by index", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"bash","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"read","arguments":"{\"path\":\"a\"}"}}]}}]}`,
			`[DONE]`,
		}, nil)
		provider := &OpenAI{BaseURL: server.URL}

		response, err := provider.Stream(context.Background(), Request{}, func(Event) {})

		if err != nil {
			t.Fatal(err)
		}
		calls := response.Message.ToolCalls
		if len(calls) != 2 {
			t.Fatalf("calls = %+v", calls)
		}
		if calls[0].ID != "c1" || calls[0].Name != "bash" || string(calls[0].Args) != `{"command":"ls"}` {
			t.Errorf("first call = %+v (%s)", calls[0], calls[0].Args)
		}
		if calls[1].ID != "c2" || calls[1].Name != "read" {
			t.Errorf("second call = %+v", calls[1])
		}
	})

	t.Run("LO-03 a tool call with no arguments becomes {}", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"ls","arguments":""}}]}}]}`,
			`[DONE]`,
		}, nil)
		provider := &OpenAI{BaseURL: server.URL}

		response, err := provider.Stream(context.Background(), Request{}, func(Event) {})

		if err != nil {
			t.Fatal(err)
		}
		if string(response.Message.ToolCalls[0].Args) != "{}" {
			t.Errorf("args = %s", response.Message.ToolCalls[0].Args)
		}
	})

	t.Run("LO-04 a non-2xx answer is an error carrying the body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"model not loaded"}`, http.StatusBadRequest)
		}))
		defer server.Close()
		provider := &OpenAI{BaseURL: server.URL}

		_, err := provider.Stream(context.Background(), Request{}, func(Event) {})

		if err == nil || !strings.Contains(err.Error(), "model not loaded") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("LO-05 sends system, tool results and tools in chat completions shape", func(t *testing.T) {
		var sent openAIRequest
		server := sseServer(t, []string{`[DONE]`}, func(body []byte) {
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Error(err)
			}
		})
		provider := &OpenAI{BaseURL: server.URL}
		request := Request{
			Model:  "m",
			System: "be brief",
			Messages: []Message{
				{Role: RoleUser, Text: "hi"},
				{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "ls", Args: json.RawMessage(`{}`)}}},
				{Role: RoleTool, ToolCallID: "c1", Text: "a.go"},
			},
			Tools: []ToolSpec{{Name: "ls", Description: "list", Schema: json.RawMessage(`{"type":"object"}`)}},
		}

		if _, err := provider.Stream(context.Background(), request, func(Event) {}); err != nil {
			t.Fatal(err)
		}

		roles := []string{}
		for _, message := range sent.Messages {
			roles = append(roles, message.Role)
		}
		if strings.Join(roles, ",") != "system,user,assistant,tool" {
			t.Errorf("roles = %v", roles)
		}
		if sent.Messages[2].ToolCalls[0].Function.Arguments != "{}" || sent.Messages[3].ToolCallID != "c1" {
			t.Errorf("tool round trip = %+v", sent.Messages[2:])
		}
		if len(sent.Tools) != 1 || sent.Tools[0].Function.Name != "ls" || !sent.Stream {
			t.Errorf("tools = %+v stream = %v", sent.Tools, sent.Stream)
		}
	})
}

func TestOpenAIListModels(t *testing.T) {
	t.Run("LO-06 ListModels returns the ids in order", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[{"id":"qwen"},{"id":"llama"}]}`)
		}))
		defer server.Close()

		models, err := (&OpenAI{BaseURL: server.URL}).ListModels(context.Background())

		if err != nil || strings.Join(models, ",") != "qwen,llama" {
			t.Errorf("models = %v, err = %v", models, err)
		}
	})
}

func TestOpenAIStreamEdges(t *testing.T) {
	t.Run("LO-07 fragments without index continue the last call", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"tool_calls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"a\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"id":"c2","function":{"name":"ls","arguments":"{}"}}]}}]}`,
			`[DONE]`,
		}, nil)

		response, err := (&OpenAI{BaseURL: server.URL}).Stream(context.Background(), Request{}, func(Event) {})

		if err != nil {
			t.Fatal(err)
		}
		calls := response.Message.ToolCalls
		if len(calls) != 2 || string(calls[0].Args) != `{"path":"a"}` || calls[1].Name != "ls" {
			t.Errorf("calls = %+v", calls)
		}
	})

	t.Run("LO-08 cut-off arguments become {} with an ArgsError", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"write","arguments":"{\"path\":\"a\",\"content\":\"abc"}}]},"finish_reason":"length"}]}`,
			`[DONE]`,
		}, nil)

		response, err := (&OpenAI{BaseURL: server.URL}).Stream(context.Background(), Request{}, func(Event) {})

		if err != nil {
			t.Fatal(err)
		}
		call := response.Message.ToolCalls[0]
		if string(call.Args) != "{}" || call.ArgsError == "" {
			t.Errorf("call = %+v", call)
		}
		if _, err := json.Marshal(toOpenAIMessage(response.Message)); err != nil {
			t.Errorf("history no longer serializes: %v", err)
		}
	})

	t.Run("LO-09 finish_reason length on text adds a cut-off note", func(t *testing.T) {
		server := sseServer(t, []string{
			`{"choices":[{"delta":{"content":"partial"},"finish_reason":"length"}]}`,
			`[DONE]`,
		}, nil)

		response, err := (&OpenAI{BaseURL: server.URL}).Stream(context.Background(), Request{}, func(Event) {})

		if err != nil || !strings.HasSuffix(response.Message.Text, cutOffTextNote) {
			t.Errorf("text = %q err = %v", response.Message.Text, err)
		}
	})
}

type requestCountKey struct{}

func TestKeepAliveRetry(t *testing.T) {
	t.Run("LO-10 a keep-alive connection the server closed is retried", func(t *testing.T) {
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := r.Context().Value(requestCountKey{}).(*int)
			*count++
			if *count > 1 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			io.Copy(io.Discard, r.Body)
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
		}))
		server.Config.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
			return context.WithValue(ctx, requestCountKey{}, new(int))
		}
		server.Start()
		defer server.Close()
		provider := &OpenAI{BaseURL: server.URL, Client: &http.Client{}}

		for attempt := range 3 {
			response, err := provider.Stream(context.Background(), Request{}, func(Event) {})
			if err != nil || response.Message.Text != "ok" {
				t.Fatalf("attempt %d: text = %q err = %v", attempt, response.Message.Text, err)
			}
		}
	})
}

func TestContextWindow(t *testing.T) {
	t.Run("LO-11 ContextWindow reads n_ctx from llama-server /props", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/props" {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":32000}}`)
		}))
		defer server.Close()

		window := (&OpenAI{BaseURL: server.URL + "/v1"}).ContextWindow(context.Background())

		if window != 32000 {
			t.Errorf("window = %d", window)
		}
	})

	t.Run("LO-12 ContextWindow is 0 when the server has no /props", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()

		if window := (&OpenAI{BaseURL: server.URL + "/v1"}).ContextWindow(context.Background()); window != 0 {
			t.Errorf("window = %d", window)
		}
	})
}

func TestNetworkRetry(t *testing.T) {
	t.Run("LO-13 a request cut by the network on a new connection is sent again", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts == 1 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
		}))
		defer server.Close()
		provider := &OpenAI{BaseURL: server.URL, Client: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}

		response, err := provider.Stream(context.Background(), Request{}, func(Event) {})

		if err != nil || response.Message.Text != "ok" || attempts != 2 {
			t.Errorf("text = %q attempts = %d err = %v", response.Message.Text, attempts, err)
		}
	})
}
