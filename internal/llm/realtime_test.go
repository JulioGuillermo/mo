package llm

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | RT-01 | Happy    | A spoken reply streams its text, calls and speech, and comes with its speech | P0       |
// | RT-02 | Happy    | The session has the system prompt and tools; each reply asks the voice mode  | P0       |
// | RT-03 | Happy    | The next request sends only the results and prompts the server does not hold | P0       |
// | RT-04 | Edge     | A history that does not go on from what the server holds starts a new one, as a transcript | P0 |
// | RT-05 | Edge     | A reply without text keeps its speech; the agent decides what to drop        | P1       |
// | RT-06 | Error    | An error event is an error, and the next request starts a new conversation  | P1       |
// | RT-07 | Happy    | A request that does not speak closes the conversation, freeing the slot     | P0       |
// | RT-08 | Happy    | Speaks tells a model that writes audio from one that does not               | P0       |

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRealtime is llama-server's realtime API as far as mo uses it: it
// logs what comes and answers each response.create with the next script.
type fakeRealtime struct {
	mu          sync.Mutex
	connections int
	received    []map[string]any
	replies     [][]map[string]any
	closed      chan struct{}
}

func (f *fakeRealtime) events(kind string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []map[string]any
	for _, event := range f.received {
		if event["type"] == kind {
			found = append(found, event)
		}
	}
	return found
}

func newFakeRealtime(t *testing.T, replies ...[]map[string]any) (*fakeRealtime, *OpenAI) {
	t.Helper()
	fake := &fakeRealtime{replies: replies, closed: make(chan struct{}, 10)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"omni","architecture":{"input_modalities":["text"],"output_modalities":["text","audio"]}},{"id":"plain"}]}`)
			return
		}
		if r.URL.Path == "/v1/chat/completions" {
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		fake.serve(t, w, r)
	}))
	t.Cleanup(server.Close)
	return fake, &OpenAI{BaseURL: server.URL + "/v1"}
}

func (f *fakeRealtime) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	conn, buffered, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	defer conn.Close()
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + wsAcceptGUID))
	fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		base64.StdEncoding.EncodeToString(sum[:]))
	f.mu.Lock()
	f.connections++
	f.mu.Unlock()
	send := func(event map[string]any) {
		data, _ := json.Marshal(event)
		frame := []byte{0x81, 127}
		frame = binary.BigEndian.AppendUint64(frame, uint64(len(data)))
		conn.Write(append(frame, data...))
	}
	send(map[string]any{"type": "session.created", "session": map[string]any{"audio": map[string]any{"output": map[string]any{"format": map[string]any{"rate": 24000}}}}})
	for {
		event, err := readClientFrame(buffered.Reader)
		if err != nil {
			f.closed <- struct{}{}
			return
		}
		f.mu.Lock()
		f.received = append(f.received, event)
		var reply []map[string]any
		if event["type"] == "response.create" && len(f.replies) > 0 {
			reply, f.replies = f.replies[0], f.replies[1:]
		}
		f.mu.Unlock()
		if event["type"] == "session.update" {
			send(map[string]any{"type": "session.updated"})
		}
		for _, answer := range reply {
			send(answer)
		}
	}
}

func readClientFrame(reader *bufio.Reader) (map[string]any, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	if header[0]&0x0F == wsClose {
		return nil, io.EOF
	}
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		extended := make([]byte, 2)
		io.ReadFull(reader, extended)
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		io.ReadFull(reader, extended)
		length = binary.BigEndian.Uint64(extended)
	}
	mask := make([]byte, 4)
	io.ReadFull(reader, mask)
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	var event map[string]any
	return event, json.Unmarshal(payload, &event)
}

func spokenText(text string, pcm []byte) []map[string]any {
	return []map[string]any{
		{"type": "response.output_text.delta", "delta": text},
		{"type": "response.output_audio.delta", "delta": base64.StdEncoding.EncodeToString(pcm)},
		{"type": "response.done", "response": map[string]any{"usage": map[string]any{"output_tokens": 7}}},
	}
}

func calling() []map[string]any {
	return []map[string]any{
		{"type": "response.output_text.delta", "delta": "Reading it."},
		{"type": "response.output_item.added", "output_index": 1, "item": map[string]any{"type": "function_call", "name": "read", "call_id": "c1"}},
		{"type": "response.function_call_arguments.delta", "output_index": 1, "delta": `{"path":`},
		{"type": "response.function_call_arguments.delta", "output_index": 1, "delta": `"a.txt"}`},
		{"type": "response.done"},
	}
}

func speak(t *testing.T, provider *OpenAI, messages []Message) (Response, []Event, error) {
	t.Helper()
	var events []Event
	request := Request{Model: "omni", System: "be brief", Voice: "text+audio", Messages: messages,
		Tools: []ToolSpec{{Name: "read", Description: "Read a file.", Schema: json.RawMessage(`{"type":"object"}`)}}}
	response, err := provider.Stream(context.Background(), request, func(event Event) { events = append(events, event) })
	return response, events, err
}

func TestRealtime(t *testing.T) {
	prompt := Message{Role: RoleUser, Text: "read a.txt"}

	t.Run("RT-01 a spoken reply streams its text and calls, and comes with its speech", func(t *testing.T) {
		_, provider := newFakeRealtime(t, append(calling()[:4], spokenText("", []byte{1, 2, 3, 4})[1:]...))

		response, events, err := speak(t, provider, []Message{prompt})

		if err != nil {
			t.Fatal(err)
		}
		reply := response.Message
		if reply.Text != "Reading it." || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].ID != "c1" || string(reply.ToolCalls[0].Args) != `{"path":"a.txt"}` {
			t.Errorf("reply = %+v", reply)
		}
		if reply.Speech == nil || string(reply.Speech.PCM) != "\x01\x02\x03\x04" || reply.Speech.Rate != 24000 {
			t.Errorf("speech = %+v", reply.Speech)
		}
		if len(events) != 5 || events[0].Text != "Reading it." || events[1].Name != "read" || events[3].Text != `"a.txt"}` {
			t.Fatalf("events = %+v", events)
		}
		if piece := events[4]; piece.Kind != EventSpeech || string(piece.Speech.PCM) != "\x01\x02\x03\x04" || piece.Speech.Rate != 24000 {
			t.Errorf("speech event = %+v", piece)
		}
		if response.Usage.OutputTokens != 7 || response.Usage.InputTokens == 0 {
			t.Errorf("usage = %+v", response.Usage)
		}
	})

	t.Run("RT-02 the session has the system prompt and tools; each reply asks the voice mode", func(t *testing.T) {
		fake, provider := newFakeRealtime(t, spokenText("hi", []byte{0, 0}), spokenText("hm", nil), spokenText("ok", nil))
		history := []Message{prompt}
		for _, mode := range []string{"text+audio", "audio", "auto"} {
			response, err := provider.Stream(context.Background(), Request{Model: "omni", System: "be brief", Voice: mode, Messages: history,
				Tools: []ToolSpec{{Name: "read", Description: "Read a file.", Schema: json.RawMessage(`{"type":"object"}`)}}}, func(Event) {})
			if err != nil {
				t.Fatal(err)
			}
			history = append(history, response.Message, Message{Role: RoleUser, Text: "again"})
		}

		session, _ := json.Marshal(fake.events("session.update")[0]["session"])
		want := `{"instructions":"be brief","output_modalities":null,"tools":[{"description":"Read a file.","name":"read","parameters":{"type":"object"},"type":"function"}],"type":"realtime"}`
		if string(session) != want {
			t.Errorf("session = %s", session)
		}
		var asked []string
		for _, create := range fake.events("response.create") {
			wire, _ := json.Marshal(create["response"].(map[string]any)["output_modalities"])
			asked = append(asked, string(wire))
		}
		if strings.Join(asked, " ") != `["text","audio"] ["audio"] null` || fake.connections != 1 {
			t.Errorf("asked %v, connections %d", asked, fake.connections)
		}
	})

	t.Run("RT-03 the next request sends only the results and prompts the server does not hold", func(t *testing.T) {
		fake, provider := newFakeRealtime(t, calling(), spokenText("It says hello.", []byte{0, 0}))
		first, _, _ := speak(t, provider, []Message{prompt})

		history := []Message{prompt, first.Message, {Role: RoleTool, ToolCallID: "c1", Text: "hello"}, {Role: RoleUser, Text: "[mo] a note"}}
		second, _, err := speak(t, provider, history)

		items := fake.events("conversation.item.create")
		if err != nil || second.Message.Text != "It says hello." || fake.connections != 1 || len(items) != 3 {
			t.Fatalf("err %v reply %q connections %d items %v", err, second.Message.Text, fake.connections, items)
		}
		result, note := items[1]["item"].(map[string]any), items[2]["item"].(map[string]any)
		if result["type"] != "function_call_output" || result["call_id"] != "c1" || result["output"] != "hello" || note["role"] != "user" {
			t.Errorf("items = %v", items[1:])
		}
	})

	t.Run("RT-04 a history that does not go on from what the server holds starts a new one, as a transcript", func(t *testing.T) {
		fake, provider := newFakeRealtime(t, spokenText("one", []byte{0, 0}), spokenText("two", []byte{0, 0}))
		first, _, _ := speak(t, provider, []Message{prompt})

		history := []Message{{Role: RoleUser, Text: "summary of before"}, first.Message, {Role: RoleUser, Text: "and now?"}}
		_, _, err := speak(t, provider, history)

		items := fake.events("conversation.item.create")
		if err != nil || fake.connections != 2 || len(items) != 3 {
			t.Fatalf("err %v connections %d items %d", err, fake.connections, len(items))
		}
		transcript := items[1]["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
		last := items[2]["item"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
		if !strings.Contains(transcript, "User: summary of before\n\nAssistant: one") || last != "and now?" {
			t.Errorf("transcript %q then %q", transcript, last)
		}
	})

	t.Run("RT-05 a reply without text keeps its speech; the agent decides what to drop", func(t *testing.T) {
		_, provider := newFakeRealtime(t, spokenText("", []byte{9, 9}))

		response, _, err := speak(t, provider, []Message{prompt})

		if err != nil || response.Message.Speech == nil || string(response.Message.Speech.PCM) != "\x09\x09" {
			t.Errorf("err %v speech %+v", err, response.Message.Speech)
		}
	})

	t.Run("RT-06 an error event is an error, and the next request starts a new conversation", func(t *testing.T) {
		fake, provider := newFakeRealtime(t, []map[string]any{{"type": "error", "error": map[string]any{"message": "the context is full"}}}, spokenText("ok", nil))

		_, _, failed := speak(t, provider, []Message{prompt})
		_, _, err := speak(t, provider, []Message{prompt})

		if failed == nil || !strings.Contains(failed.Error(), "the context is full") || err != nil || fake.connections != 2 {
			t.Errorf("failed %v then %v, connections %d", failed, err, fake.connections)
		}
	})

	t.Run("RT-07 a request that does not speak closes the conversation, freeing the slot", func(t *testing.T) {
		fake, provider := newFakeRealtime(t, spokenText("hi", nil))
		speak(t, provider, []Message{prompt})

		_, err := provider.Stream(context.Background(), Request{Model: "omni", Messages: []Message{prompt}}, func(Event) {})

		select {
		case <-fake.closed:
		case <-time.After(2 * time.Second):
			t.Error("the conversation was left open")
		}
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("RT-08 Speaks tells a model that writes audio from one that does not", func(t *testing.T) {
		_, provider := newFakeRealtime(t)

		if !provider.Speaks(context.Background(), "omni") || provider.Speaks(context.Background(), "plain") {
			t.Error("omni should speak and plain should not")
		}
	})
}
