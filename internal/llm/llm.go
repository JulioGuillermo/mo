// Package llm speaks to model providers through one provider-neutral
// conversation format, so the agent never sees a wire protocol.
package llm

import (
	"context"
	"encoding/json"
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
	Images     []Image
	ToolCalls  []ToolCall
	ToolCallID string
	IsError    bool
	// Timing is when it happened, to measure models; never sent.
	Timing *Timing `json:",omitempty"`
	// Stopped marks a reply the user stopped while it was written, its
	// text cut where it was; never sent.
	Stopped bool `json:",omitempty"`
}

// Image is a picture sent to the model with a user message.
type Image struct {
	MIME string
	Data []byte
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

const cutOffArgsError = "The arguments of this call were cut off, probably at the output token limit. Split the work into smaller calls."

// newToolCall checks the streamed arguments. Cut-off JSON is replaced by {}
// and reported in ArgsError, so the call still goes back to the provider in a
// valid shape and the model learns what happened.
func newToolCall(id, name, args string) ToolCall {
	call := ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
	switch {
	case args == "":
		call.Args = json.RawMessage("{}")
	case !json.Valid(call.Args):
		call.Args = json.RawMessage("{}")
		call.ArgsError = cutOffArgsError
	}
	return call
}
