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
	Role       Role
	Text       string
	Reasoning  string
	Images     []Image
	ToolCalls  []ToolCall
	ToolCallID string
	IsError    bool
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
)

type Event struct {
	Kind EventKind
	Text string
	// Call numbers the tool call a piece belongs to, in the reply, and
	// Name is its tool when the piece names it.
	Call int
	Name string
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
