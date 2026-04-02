package dashboard

import (
	"testing"
	"time"

	"lumen-agent/internal/auditlog"
)

func TestBuildStateMarksActiveNodesAndEdges(t *testing.T) {
	now := time.Date(2026, 4, 2, 8, 0, 0, 0, time.UTC)
	entries := []auditlog.Entry{
		{
			Time:      now.Add(-2 * time.Second).Format(time.RFC3339),
			Kind:      "turn_start",
			SessionID: "session-1",
			Data:      map[string]any{"kind": "user"},
		},
		{
			Time:      now.Add(-1 * time.Second).Format(time.RFC3339),
			Kind:      "model_done",
			SessionID: "session-1",
			Data:      map[string]any{"tokens": 321},
		},
		{
			Time:      now.Add(-500 * time.Millisecond).Format(time.RFC3339),
			Kind:      "tool_done",
			SessionID: "session-1",
			Data: map[string]any{
				"tool":        "read_file",
				"detail":      "ok",
				"full_detail": "full output",
				"duration_ms": 42,
				"success":     true,
			},
		},
	}

	state := BuildState(entries, now, 8*time.Second, 50, 50)

	if !findNode(state.Nodes, "discord").Active {
		t.Fatal("expected discord node to be active")
	}
	if !findNode(state.Nodes, "agent").Active {
		t.Fatal("expected agent node to be active")
	}
	if !findNode(state.Nodes, "llms").Active {
		t.Fatal("expected llms node to be active")
	}
	if !findNode(state.Nodes, "tool").Active {
		t.Fatal("expected tool node to be active")
	}
	if !findEdge(state.Edges, "discord-agent").Active {
		t.Fatal("expected discord-agent edge to be active")
	}
	if !findEdge(state.Edges, "agent-llms").Active {
		t.Fatal("expected agent-llms edge to be active")
	}
	if !findEdge(state.Edges, "llms-tool").Active {
		t.Fatal("expected llms-tool edge to be active")
	}
	if len(state.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(state.ToolCalls))
	}
	if state.ToolCalls[0].Tool != "read_file" {
		t.Fatalf("expected tool read_file, got %q", state.ToolCalls[0].Tool)
	}
	if state.ToolCalls[0].Success == nil || !*state.ToolCalls[0].Success {
		t.Fatal("expected successful tool call")
	}
}

func TestBuildStateIgnoresExpiredActivity(t *testing.T) {
	now := time.Date(2026, 4, 2, 8, 0, 0, 0, time.UTC)
	entries := []auditlog.Entry{
		{
			Time:      now.Add(-30 * time.Second).Format(time.RFC3339),
			Kind:      "turn_start",
			SessionID: "session-1",
		},
	}

	state := BuildState(entries, now, 8*time.Second, 10, 10)
	if findNode(state.Nodes, "discord").Active {
		t.Fatal("expected discord node to stay inactive for expired activity")
	}
	if findEdge(state.Edges, "discord-agent").Active {
		t.Fatal("expected discord-agent edge to stay inactive for expired activity")
	}
}

func findNode(nodes []nodeState, id string) nodeState {
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	return nodeState{}
}

func findEdge(edges []edgeState, id string) edgeState {
	for _, edge := range edges {
		if edge.ID == id {
			return edge
		}
	}
	return edgeState{}
}
