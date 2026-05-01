package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"element-orion/internal/llm"
)

func TestHandleReadPreviousMessages(t *testing.T) {
	r := &Registry{}
	history := []llm.Message{
		{Role: "user", Content: "Hello", Timestamp: "2023-01-01T00:00:00Z"},
		{Role: "assistant", Content: "Hi there!", Timestamp: "2023-01-01T00:00:01Z"},
		{Role: "user", Content: "How are you?", Timestamp: "2023-01-01T00:00:02Z"},
	}

	ctx := WithBackgroundTaskRuntimeContext(context.Background(), BackgroundTaskRuntimeContext{
		History:     history,
		RequestedAt: time.Now(),
	})

	t.Run("default limit", func(t *testing.T) {
		result, err := r.handleReadPreviousMessages(ctx, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(result, "Last 3 messages:") {
			t.Errorf("expected Last 3 messages:, got: %s", result)
		}
		if !strings.Contains(result, "Hello") || !strings.Contains(result, "Hi there!") || !strings.Contains(result, "How are you?") {
			t.Errorf("result missing expected messages: %s", result)
		}
	})

	t.Run("with limit", func(t *testing.T) {
		result, err := r.handleReadPreviousMessages(ctx, json.RawMessage(`{"limit": 1}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(result, "Last 1 messages:") {
			t.Errorf("expected Last 1 messages:, got: %s", result)
		}
		if strings.Contains(result, "Hello") || strings.Contains(result, "Hi there!") {
			t.Errorf("result should not contain earlier messages: %s", result)
		}
		if !strings.Contains(result, "How are you?") {
			t.Errorf("result missing expected message: %s", result)
		}
	})

	t.Run("no history", func(t *testing.T) {
		emptyCtx := WithBackgroundTaskRuntimeContext(context.Background(), BackgroundTaskRuntimeContext{
			History:     []llm.Message{},
			RequestedAt: time.Now(),
		})
		result, err := r.handleReadPreviousMessages(emptyCtx, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "No messages in history." {
			t.Errorf("expected No messages in history., got: %s", result)
		}
	})

	t.Run("no context", func(t *testing.T) {
		_, err := r.handleReadPreviousMessages(context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected error when context is missing history")
		}
	})
}
