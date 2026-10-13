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
	ToolCalls  []ToolCall
	ToolCallID string
	IsError    bool
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
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type EventKind int

const (
	EventText EventKind = iota
	EventReasoning
)

type Event struct {
	Kind EventKind
	Text string
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
