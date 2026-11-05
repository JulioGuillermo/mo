// Package llm speaks to model providers through one provider-neutral
// conversation format, so the agent never sees a wire protocol.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role Role
	Text string
	// Input is what the user typed, kept when mo sent Text changed from
	// it; never sent to the model.
	Input     string `json:",omitempty"`
	Reasoning string
	// Thinking are the provider's reasoning blocks as they came, signature
	// and all, to send back unchanged; never read.
	Thinking   json.RawMessage `json:",omitempty"`
	Media      []Media         `json:"Images,omitempty"`
	ToolCalls  []ToolCall
	ToolCallID string
	IsError    bool
	// Timing is when it happened, to measure models; never sent.
	Timing *Timing `json:",omitempty"`
	// Stopped marks a reply the user stopped while it was written, its
	// text cut where it was; never sent.
	Stopped bool `json:",omitempty"`
	// Speech is the reply in the model's own voice, when it was asked to
	// speak; never sent nor saved.
	Speech *Speech `json:"-"`
	// SpeechFile names the file Speech was saved to, in the conversation's
	// folder; never sent.
	SpeechFile string `json:",omitempty"`
}

// Speech is mono 16-bit PCM audio.
type Speech struct {
	PCM  []byte
	Rate int
}

// Media is a picture, a recording or a video sent to the model with a user
// message.
type Media struct {
	MIME string
	Data []byte
}

// Kind is image, audio or video, the first part of the MIME type.
func (m Media) Kind() string {
	kind, _, _ := strings.Cut(m.MIME, "/")
	return kind
}

// notTaken is the line sent in place of media the provider does not take.
func (m Media) notTaken() string {
	return fmt.Sprintf("[%s not sent: this model does not take %s]", m.Kind(), m.Kind())
}

type ToolCall struct {
	ID        string
	Name      string
	Args      json.RawMessage
	ArgsError string
	// Extra is what the provider sends with a call to have it back as it
	// was, such as Gemini's thought signature; it is never read.
	Extra json.RawMessage `json:",omitempty"`
}

type ToolSpec struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
	// Effort asks a model that reasons for more or less of it: low to max
	// for Anthropic, none to xhigh for OpenAI's. Empty leaves the model's
	// default, and nothing is sent.
	Effort string
	// NoThinking leaves thinking at the model's default, for the short
	// requests of titles and the auto-mode judge.
	NoThinking bool
	// Voice asks a model that talks to say its reply in its own voice:
	// written and said (text+audio), only said (audio), or as the model
	// chooses (auto). Empty asks for text alone.
	Voice string
}

// Usage is what a reply cost, and how fast the prompt was read and the
// answer written, in tokens per second.
type Usage struct {
	InputTokens    int
	OutputTokens   int
	PromptRate     float64
	GenerationRate float64
}

type EventKind int

const (
	EventText EventKind = iota
	EventReasoning
	// EventToolCall is a piece of a tool call as the model writes it: its
	// name once known, then pieces of its JSON arguments.
	EventToolCall
	// EventPromptProgress says how far the server is in reading the prompt,
	// before the answer starts. Only llama-server sends it.
	EventPromptProgress
	// EventSpeech is a piece of the reply said in the model's voice, as it
	// comes.
	EventSpeech
)

// PromptProgress is how much of the prompt the server has read: Done of
// Total tokens, Cached of them taken from its cache without reading.
type PromptProgress struct {
	Done, Total, Cached int
}

// Share is the part of the prompt read, leaving out what came cached, as
// that is the part that takes time.
func (p PromptProgress) Share() float64 {
	work := p.Total - p.Cached
	if work <= 0 {
		return 1
	}
	return min(1, max(0, float64(p.Done-p.Cached)/float64(work)))
}

type Event struct {
	Kind EventKind
	Text string
	// Call numbers the tool call a piece belongs to, in the reply; Name is
	// its tool and ID its id when the piece names them.
	Call int
	Name string
	ID   string
	// Progress is set on EventPromptProgress.
	Progress PromptProgress
	// Speech is set on EventSpeech.
	Speech *Speech
}

type Response struct {
	Message Message
	Usage   Usage
}

type Provider interface {
	// Stream sends the request and calls onEvent for every text or reasoning
	// delta as it arrives. The returned message holds the full answer.
	Stream(ctx context.Context, req Request, onEvent func(Event)) (Response, error)
}

const (
	cutOffArgsError  = "The arguments of this call were cut off at the output token limit. Split the work into smaller calls."
	invalidArgsError = "The arguments of this call are not valid JSON (%v). Write them again as valid JSON."
)

// newToolCall checks the streamed arguments. Tabs and line breaks written
// raw inside a string, as some local models do, are escaped. JSON that is
// still not valid is replaced by {} and reported in ArgsError, so the call
// still goes back to the provider in a valid shape and the model learns
// what happened.
func newToolCall(id, name, args string) ToolCall {
	call := ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
	if args == "" {
		call.Args = json.RawMessage("{}")
		return call
	}
	if json.Valid(call.Args) {
		return call
	}
	if escaped := escapeRawControls(args); json.Valid([]byte(escaped)) {
		call.Args = json.RawMessage(escaped)
		return call
	}
	var parsed any
	err := json.Unmarshal(call.Args, &parsed)
	call.Args = json.RawMessage("{}")
	call.ArgsError = fmt.Sprintf(invalidArgsError, err)
	return call
}

// escapeRawControls escapes the control characters inside the strings of
// some JSON, which JSON forbids there: a tab becomes \t, a line break \n.
func escapeRawControls(text string) string {
	var out strings.Builder
	inString, escaped := false, false
	for _, char := range text {
		switch {
		case escaped:
			escaped = false
		case inString && char == '\\':
			escaped = true
		case char == '"':
			inString = !inString
		case inString && char < 0x20:
			out.WriteString(escapedControl(char))
			continue
		}
		out.WriteRune(char)
	}
	return out.String()
}

func escapedControl(char rune) string {
	switch char {
	case '\t':
		return `\t`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	}
	return fmt.Sprintf(`\u%04x`, char)
}

// blameCutOff tells the model that the calls whose arguments are not valid
// were cut off, when the provider said the reply was.
func blameCutOff(calls []ToolCall) {
	for i := range calls {
		if calls[i].ArgsError != "" {
			calls[i].ArgsError = cutOffArgsError
		}
	}
}
