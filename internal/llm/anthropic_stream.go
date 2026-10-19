package llm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const cutOffTextNote = "\n\n[answer cut off at the output token limit]"

type anthropicStream struct {
	onEvent  func(Event)
	text     strings.Builder
	thinking strings.Builder
	toolUses []*pendingToolUse
	byIndex  map[int]*pendingToolUse
	// blocks are the thinking blocks, as they came, by index.
	blocks          []*pendingThinking
	thinkingByIndex map[int]*pendingThinking
	cutOff          bool
	refused         bool
	usage           Usage
}

type pendingToolUse struct {
	id   string
	name string
	args strings.Builder
}

type pendingThinking struct {
	kind      string
	text      strings.Builder
	signature string
	data      string
}

func newAnthropicStream(onEvent func(Event)) *anthropicStream {
	return &anthropicStream{onEvent: onEvent, byIndex: map[int]*pendingToolUse{}, thinkingByIndex: map[int]*pendingThinking{}}
}

func (s *anthropicStream) handle(event anthropicEvent) error {
	switch event.Type {
	case "message_start":
		usage := event.Message.Usage
		s.usage.InputTokens = usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens
	case "content_block_start":
		switch event.ContentBlock.Type {
		case "thinking", "redacted_thinking":
			block := &pendingThinking{kind: event.ContentBlock.Type, signature: event.ContentBlock.Signature, data: event.ContentBlock.Data}
			block.text.WriteString(event.ContentBlock.Thinking)
			s.blocks = append(s.blocks, block)
			s.thinkingByIndex[event.Index] = block
		}
		if event.ContentBlock.Type == "tool_use" {
			toolUse := &pendingToolUse{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			s.toolUses = append(s.toolUses, toolUse)
			s.byIndex[event.Index] = toolUse
			s.onEvent(Event{Kind: EventToolCall, Call: len(s.toolUses) - 1, Name: toolUse.name, ID: toolUse.id})
		}
	case "content_block_delta":
		s.handleDelta(event)
	case "message_delta":
		s.usage.OutputTokens = event.Usage.OutputTokens
		s.cutOff = event.Delta.StopReason == "max_tokens"
		s.refused = event.Delta.StopReason == "refusal"
	case "message_stop":
		return errStopSSE
	case "error":
		return fmt.Errorf("provider error: %s", event.Error.Message)
	}
	return nil
}

func (s *anthropicStream) handleDelta(event anthropicEvent) {
	switch event.Delta.Type {
	case "text_delta":
		s.text.WriteString(event.Delta.Text)
		s.onEvent(Event{Kind: EventText, Text: event.Delta.Text})
	case "thinking_delta":
		if block, ok := s.thinkingByIndex[event.Index]; ok {
			block.text.WriteString(event.Delta.Thinking)
		}
		if event.Delta.Thinking != "" {
			s.thinking.WriteString(event.Delta.Thinking)
			s.onEvent(Event{Kind: EventReasoning, Text: event.Delta.Thinking})
		}
	case "signature_delta":
		if block, ok := s.thinkingByIndex[event.Index]; ok {
			block.signature += event.Delta.Signature
		}
	case "input_json_delta":
		if toolUse, ok := s.byIndex[event.Index]; ok {
			toolUse.args.WriteString(event.Delta.PartialJSON)
			s.onEvent(Event{Kind: EventToolCall, Call: slices.Index(s.toolUses, toolUse), Text: event.Delta.PartialJSON})
		}
	}
}

func (s *anthropicStream) message() Message {
	message := Message{Role: RoleAssistant, Text: s.text.String(), Reasoning: s.thinking.String()}
	if s.cutOff && len(s.toolUses) == 0 {
		message.Text += cutOffTextNote
		s.onEvent(Event{Kind: EventText, Text: cutOffTextNote})
	}
	if s.refused {
		noteRefusal(&message, s.onEvent)
	}
	message.Thinking = s.thinkingBlocks()
	for _, toolUse := range s.toolUses {
		message.ToolCalls = append(message.ToolCalls, newToolCall(toolUse.id, toolUse.name, toolUse.args.String()))
	}
	return message
}

// thinkingBlocks are the thinking blocks to send back as they came; one
// without its signature cannot go back, and none is kept then.
func (s *anthropicStream) thinkingBlocks() json.RawMessage {
	var blocks []anthropicBlock
	for _, block := range s.blocks {
		switch {
		case block.kind == "redacted_thinking" && block.data != "":
			blocks = append(blocks, anthropicBlock{Type: block.kind, Data: block.data})
		case block.kind == "thinking" && block.signature != "":
			text := block.text.String()
			blocks = append(blocks, anthropicBlock{Type: block.kind, Thinking: &text, Signature: block.signature})
		default:
			return nil
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		return nil
	}
	return data
}
