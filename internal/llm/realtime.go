package llm

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	voiceReplayNote = "[mo] The conversation so far, where you are the assistant:"
	voiceReplayEnd  = "[mo] End of the conversation so far. Answer what comes next as the assistant, without these labels."
)

// voiceSession is the realtime conversation the server keeps for a model
// that talks, with what it holds, so a request sends only what is new.
type voiceSession struct {
	mu     sync.Mutex
	socket *webSocket
	model  string
	system string
	tools  string
	// held are the fingerprints of the messages the server holds.
	held         []string
	outputRate   int
	outputTokens int
}

type realtimeTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type realtimeEvent struct {
	Type        string `json:"type"`
	Delta       string `json:"delta"`
	OutputIndex int    `json:"output_index"`
	Item        struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		CallID string `json:"call_id"`
	} `json:"item"`
	Session struct {
		Audio struct {
			Output struct {
				Format struct {
					Rate int `json:"rate"`
				} `json:"format"`
			} `json:"output"`
		} `json:"audio"`
	} `json:"session"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Response struct {
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"response"`
}

// streamSpoken answers through the realtime API, where the model writes its
// reply and says it in its own voice. The conversation stays open on the
// server between requests; one that does not go on from what it holds
// starts a new one, with the history so far as a transcript.
func (p *OpenAI) streamSpoken(ctx context.Context, req Request, onEvent func(Event)) (Response, error) {
	session := &p.voice
	session.mu.Lock()
	defer session.mu.Unlock()
	tools := realtimeTools(req.Tools)
	toolsKey, _ := json.Marshal(tools)
	news, goesOn := session.newMessages(req, string(toolsKey))
	if !goesOn {
		session.close()
		if err := p.openVoice(ctx, req, tools); err != nil {
			session.close()
			return Response{}, fmt.Errorf("the realtime API: %w", err)
		}
		session.model, session.system, session.tools = req.Model, req.System, string(toolsKey)
		news = voiceReplay(req.Messages)
	}
	socket := session.socket
	stopReading := context.AfterFunc(ctx, func() {
		session.send(map[string]any{"type": "response.cancel"})
		socket.conn.SetReadDeadline(time.Now())
	})
	defer stopReading()
	reply, err := session.answer(news, req.MaxTokens, onEvent)
	if err != nil {
		session.close()
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, err
	}
	session.held = append(fingerprints(req.Messages), fingerprint(reply))
	usage := Usage{InputTokens: guessedTokens(req, string(toolsKey)), OutputTokens: session.outputTokens}
	return Response{Message: reply, Usage: usage}, nil
}

// guessedTokens is about how many tokens the request takes, as the realtime
// API does not count them: four bytes each.
func guessedTokens(req Request, tools string) int {
	size := len(req.System) + len(tools)
	for _, message := range req.Messages {
		size += len(message.Text)
		for _, call := range message.ToolCalls {
			size += len(call.Name) + len(call.Args)
		}
	}
	return size / 4
}

func (p *OpenAI) openVoice(ctx context.Context, req Request, tools []realtimeTool) error {
	headers, err := p.headers(ctx)
	if err != nil {
		return err
	}
	socket, err := dialWebSocket(ctx, p.url("/realtime"), headers)
	if err != nil {
		return err
	}
	session := &p.voice
	session.socket = socket
	stopDialing := context.AfterFunc(ctx, func() { socket.conn.SetDeadline(time.Now()) })
	defer stopDialing()
	created, err := session.expect("session.created")
	if err != nil {
		return err
	}
	session.outputRate = created.Session.Audio.Output.Format.Rate
	update := map[string]any{"type": "realtime", "instructions": req.System, "output_modalities": []string{"text", "audio"}}
	if len(tools) > 0 {
		update["tools"] = tools
	}
	if err := session.send(map[string]any{"type": "session.update", "session": update}); err != nil {
		return err
	}
	_, err = session.expect("session.updated")
	return err
}

// newMessages are the messages of req the server does not hold yet, when
// req goes on from what it holds with only results and prompts.
func (s *voiceSession) newMessages(req Request, tools string) ([]Message, bool) {
	if s.socket == nil || s.model != req.Model || s.system != req.System || s.tools != tools || len(req.Messages) <= len(s.held) {
		return nil, false
	}
	for i, held := range s.held {
		if fingerprint(req.Messages[i]) != held {
			return nil, false
		}
	}
	news := req.Messages[len(s.held):]
	for _, message := range news {
		if message.Role == RoleAssistant {
			return nil, false
		}
	}
	return news, true
}

// answer sends the messages and asks for a reply, which it reads whole.
func (s *voiceSession) answer(messages []Message, maxTokens int, onEvent func(Event)) (Message, error) {
	for _, message := range messages {
		if err := s.send(realtimeItem(message)); err != nil {
			return Message{}, err
		}
	}
	create := map[string]any{"type": "response.create"}
	if maxTokens > 0 {
		create["response"] = map[string]any{"max_output_tokens": maxTokens}
	}
	if err := s.send(create); err != nil {
		return Message{}, err
	}
	return s.readReply(onEvent)
}

type spokenCall struct {
	id, name string
	args     strings.Builder
}

// readReply reads the reply as it comes; one without text keeps no speech.
func (s *voiceSession) readReply(onEvent func(Event)) (Message, error) {
	var text strings.Builder
	var pcm []byte
	var calls []*spokenCall
	callAt := map[int]int{}
	for {
		event, err := s.receive()
		if err != nil {
			return Message{}, err
		}
		switch event.Type {
		case "response.output_text.delta":
			text.WriteString(event.Delta)
			onEvent(Event{Kind: EventText, Text: event.Delta})
		case "response.output_audio.delta":
			chunk, err := base64.StdEncoding.DecodeString(event.Delta)
			if err != nil {
				return Message{}, fmt.Errorf("unreadable audio from the server: %w", err)
			}
			pcm = append(pcm, chunk...)
		case "response.output_item.added":
			if event.Item.Type != "function_call" {
				continue
			}
			call := &spokenCall{id: event.Item.CallID, name: event.Item.Name}
			if call.id == "" {
				call.id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), len(calls))
			}
			callAt[event.OutputIndex] = len(calls)
			onEvent(Event{Kind: EventToolCall, Call: len(calls), ID: call.id, Name: call.name})
			calls = append(calls, call)
		case "response.function_call_arguments.delta":
			index, known := callAt[event.OutputIndex]
			if !known {
				continue
			}
			calls[index].args.WriteString(event.Delta)
			onEvent(Event{Kind: EventToolCall, Call: index, Text: event.Delta})
		case "response.done":
			s.outputTokens = event.Response.Usage.OutputTokens
			reply := Message{Role: RoleAssistant, Text: text.String()}
			for _, call := range calls {
				reply.ToolCalls = append(reply.ToolCalls, newToolCall(call.id, call.name, call.args.String()))
			}
			if len(pcm) > 0 && strings.TrimSpace(reply.Text) != "" {
				reply.Speech = &Speech{PCM: pcm, Rate: s.outputRate}
			}
			return reply, nil
		}
	}
}

func (s *voiceSession) send(event map[string]any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.socket.Send(data)
}

// receive reads the next event; an error event is an error.
func (s *voiceSession) receive() (realtimeEvent, error) {
	data, err := s.socket.Receive()
	if err != nil {
		return realtimeEvent{}, err
	}
	var event realtimeEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return realtimeEvent{}, fmt.Errorf("unreadable event from the server: %w", err)
	}
	if event.Type == "error" {
		return realtimeEvent{}, errors.New(event.Error.Message)
	}
	return event, nil
}

// expect reads events until one of kind.
func (s *voiceSession) expect(kind string) (realtimeEvent, error) {
	for {
		event, err := s.receive()
		if err != nil || event.Type == kind {
			return event, err
		}
	}
}

// end closes the conversation, which holds a slot of the server while open.
func (s *voiceSession) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.close()
}

func (s *voiceSession) close() {
	if s.socket != nil {
		s.socket.Close()
	}
	s.socket, s.held = nil, nil
}

func realtimeTools(specs []ToolSpec) []realtimeTool {
	var tools []realtimeTool
	for _, spec := range specs {
		tools = append(tools, realtimeTool{Type: "function", Name: spec.Name, Description: spec.Description, Parameters: spec.Schema})
	}
	return tools
}

// realtimeItem is a prompt as a user message, or a result as the output of
// its call; the media in a prompt go as a line saying they were not sent.
func realtimeItem(message Message) map[string]any {
	if message.Role == RoleTool {
		return map[string]any{"type": "conversation.item.create", "item": map[string]any{
			"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Text}}
	}
	text := message.Text
	for _, media := range message.Media {
		text += "\n" + media.notTaken()
	}
	content := []map[string]any{{"type": "input_text", "text": text}}
	return map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "message", "role": "user", "content": content}}
}

// voiceReplay is the history as prompts, for a new conversation on the
// server to go on from: the prompts after the last reply as they are, and
// what comes before them as one transcript.
func voiceReplay(messages []Message) []Message {
	prompts := len(messages)
	for prompts > 0 && messages[prompts-1].Role == RoleUser {
		prompts--
	}
	if prompts == len(messages) {
		return []Message{voiceTranscript(messages)}
	}
	if prompts == 0 {
		return messages
	}
	return append([]Message{voiceTranscript(messages[:prompts])}, messages[prompts:]...)
}

func voiceTranscript(messages []Message) Message {
	var transcript strings.Builder
	transcript.WriteString(voiceReplayNote)
	for _, message := range messages {
		switch message.Role {
		case RoleUser:
			fmt.Fprintf(&transcript, "\n\nUser: %s", message.Text)
		case RoleAssistant:
			if strings.TrimSpace(message.Text) != "" {
				fmt.Fprintf(&transcript, "\n\nAssistant: %s", message.Text)
			}
			for _, call := range message.ToolCalls {
				fmt.Fprintf(&transcript, "\n\nAssistant called %s %s", call.Name, call.Args)
			}
		case RoleTool:
			fmt.Fprintf(&transcript, "\n\nResult: %s", message.Text)
		}
	}
	transcript.WriteString("\n\n" + voiceReplayEnd)
	return Message{Role: RoleUser, Text: transcript.String()}
}

// fingerprint tells a message from another by what the server holds of it.
func fingerprint(message Message) string {
	held := []string{string(message.Role), message.ToolCallID}
	if message.Role == RoleAssistant {
		for _, call := range message.ToolCalls {
			held = append(held, call.ID)
		}
	} else {
		held = append(held, message.Text)
	}
	sum := sha256.Sum256([]byte(strings.Join(held, "\x00")))
	return string(sum[:])
}

func fingerprints(messages []Message) []string {
	var held []string
	for _, message := range messages {
		held = append(held, fingerprint(message))
	}
	return held
}
