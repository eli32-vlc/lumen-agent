package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (r *Registry) registerMessagesTools() {
	r.register(
		"read_previous_messages",
		"Retrieve the last 10 messages in the current conversation history, including timestamps and metadata. Use this to verify recent context or when specifically asked about previous messages.",
		objectSchema(map[string]any{
			"limit": integerSchema("Optional number of messages to retrieve. Defaults to 10.", 1),
		}),
		r.handleReadPreviousMessages,
	)
}

func (r *Registry) handleReadPreviousMessages(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Limit int `json:"limit"`
	}

	var a args
	if err := decodeArgs(payload, &a); err != nil {
		return "", err
	}

	if a.Limit <= 0 {
		a.Limit = 10
	}

	runtime, ok := BackgroundTaskRuntimeContextFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("conversation history is not available in the current context")
	}

	history := runtime.History
	if len(history) == 0 {
		return "No messages in history.", nil
	}

	if len(history) > a.Limit {
		history = history[len(history)-a.Limit:]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Last %d messages:\n\n", len(history)))

	for _, msg := range history {
		sb.WriteString(fmt.Sprintf("[%s] **%s**", msg.Timestamp, msg.Role))
		if msg.Name != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", msg.Name))
		}
		sb.WriteString(":\n")

		if msg.Content != "" {
			sb.WriteString(msg.Content)
			sb.WriteString("\n")
		}

		if len(msg.ToolCalls) > 0 {
			sb.WriteString("Tool Calls:\n")
			for _, call := range msg.ToolCalls {
				sb.WriteString(fmt.Sprintf("- %s(%s)\n", call.Function.Name, call.Function.Arguments))
			}
		}

		if msg.ToolCallID != "" {
			sb.WriteString(fmt.Sprintf("Tool Call ID: %s\n", msg.ToolCallID))
		}

		sb.WriteString("\n")
	}

	return sb.String(), nil
}
