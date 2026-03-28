package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"lumen-agent/internal/llm"
)

func TestHandleCompactContextReturnsSignal(t *testing.T) {
	registry := &Registry{}
	ctx := WithBackgroundTaskRuntimeContext(context.Background(), BackgroundTaskRuntimeContext{
		History: []llm.Message{
			{Role: "user", Content: "hello"},
		},
		RequestedAt: time.Now(),
	})

	result, err := registry.handleCompactContext(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleCompactContext returned error: %v", err)
	}

	var signal CompactContextSignal
	if err := json.Unmarshal([]byte(result), &signal); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !signal.CompactNow {
		t.Fatalf("expected compact_now=true, got %#v", signal)
	}
}
