package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type CompactContextSignal struct {
	CompactNow  bool   `json:"compact_now"`
	Reason      string `json:"reason,omitempty"`
	RequestedAt string `json:"requested_at,omitempty"`
}

func (r *Registry) registerContextCompactionTool() {
	r.register(
		"compact_context",
		"Compact the active conversation history when context is getting crowded. Use this when the session is long and older turns should be summarized so important recent work stays in-window.",
		objectSchema(map[string]any{}),
		r.handleCompactContext,
	)
}

func (r *Registry) handleCompactContext(ctx context.Context, _ json.RawMessage) (string, error) {
	if _, ok := BackgroundTaskRuntimeContextFromContext(ctx); !ok {
		return "", fmt.Errorf("conversation runtime context is not available")
	}
	return jsonResult(CompactContextSignal{
		CompactNow:  true,
		Reason:      "runner should compact the active conversation history now",
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
	})
}
