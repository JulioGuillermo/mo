package llm

import (
	"fmt"
	"strings"
)

const cutOffTextNote = "\n\n[answer cut off at the output token limit]"

type anthropicStream struct {
	onEvent  func(Event)
	text     strings.Builder
	toolUses []*pendingToolUse
	byIndex  map[int]*pendingToolUse
	cutOff   bool
	usage    Usage
}

type pendingToolUse struct {
	id   string
	name string
	args strings.Builder
}

func newAnthropicStream(onEvent func(Event)) *anthropicStream {
	return &anthropicStream{onEvent: onEvent, byIndex: map[int]*pendingToolUse{}}
}

func (s *anthropicStream) handle(event anthropicEvent) error {
	switch event.Type {
	case "message_start":
		usage := event.Message.Usage
		s.usage.InputTokens = usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens
	case "content_block_start":
		if event.ContentBlock.Type == "tool_use" {
			toolUse := &pendingToolUse{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			s.toolUses = append(s.toolUses, toolUse)
			s.byIndex[event.Index] = toolUse
		}
	case "content_block_delta":
		s.handleDelta(event)
	case "message_delta":
		s.usage.OutputTokens = event.Usage.OutputTokens
		s.cutOff = event.Delta.StopReason == "max_tokens"
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
		s.onEvent(Event{Kind: EventReasoning, Text: event.Delta.Thinking})
	case "input_json_delta":
		if toolUse, ok := s.byIndex[event.Index]; ok {
			toolUse.args.WriteString(event.Delta.PartialJSON)
		}
	}
}

func (s *anthropicStream) message() Message {
	message := Message{Role: RoleAssistant, Text: s.text.String()}
	if s.cutOff && len(s.toolUses) == 0 {
		message.Text += cutOffTextNote
		s.onEvent(Event{Kind: EventText, Text: cutOffTextNote})
	}
	for _, toolUse := range s.toolUses {
		message.ToolCalls = append(message.ToolCalls, newToolCall(toolUse.id, toolUse.name, toolUse.args.String()))
	}
	return message
}
