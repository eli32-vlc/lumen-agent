package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen-agent/internal/auditlog"
	"lumen-agent/internal/config"
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
	if state.Summary.RecentTokens != 321 {
		t.Fatalf("expected 321 recent tokens, got %d", state.Summary.RecentTokens)
	}
	if state.Summary.ModelCalls != 1 {
		t.Fatalf("expected 1 model call, got %d", state.Summary.ModelCalls)
	}
	if state.Summary.RecentToolCalls != 1 {
		t.Fatalf("expected 1 recent tool call, got %d", state.Summary.RecentToolCalls)
	}
	if state.Summary.ActiveNodes != 4 {
		t.Fatalf("expected 4 active nodes, got %d", state.Summary.ActiveNodes)
	}
	if state.Summary.ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", state.Summary.ActiveSessions)
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

func TestBuildStateCountsFailuresAndBackgroundEvents(t *testing.T) {
	now := time.Date(2026, 4, 2, 8, 0, 0, 0, time.UTC)
	entries := []auditlog.Entry{
		{
			Time:      now.Add(-2 * time.Second).Format(time.RFC3339),
			Kind:      "background_model_done",
			SessionID: "task-1",
			Data:      map[string]any{"tokens": 120},
		},
		{
			Time:      now.Add(-1 * time.Second).Format(time.RFC3339),
			Kind:      "background_tool_done",
			SessionID: "task-1",
			Data: map[string]any{
				"tool":    "write_file",
				"success": false,
			},
		},
	}

	state := BuildState(entries, now, 8*time.Second, 20, 20)

	if state.Summary.ToolFailures != 1 {
		t.Fatalf("expected 1 tool failure, got %d", state.Summary.ToolFailures)
	}
	if state.Summary.BackgroundEvents != 2 {
		t.Fatalf("expected 2 background events, got %d", state.Summary.BackgroundEvents)
	}
}

func TestBuildMemoryStateSummarizesMemoryConfig(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"MEMORY.md":         "curated",
		"2026-04-02-AM.md":  "one",
		"2026-04-02-PM.md":  "two",
		"notes.txt":         "misc",
		"2026-04-01-AM.md":  "older",
		"2026-04-01-PM.tmp": "ignore",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cfg := config.Config{
		App: config.AppConfig{
			MemoryDir:           root,
			LoadAllMemoryShards: false,
			HistoryCompaction: config.AppHistoryCompactionConfig{
				Enabled:                true,
				PreserveRecentMessages: 9,
			},
		},
		LLM: config.LLMConfig{
			ContextWindowTokens: 12000,
			MaxTokens:           3000,
		},
	}

	state := buildMemoryState(cfg)

	if !state.Available {
		t.Fatal("expected memory state to be available")
	}
	if state.LoadMode != "current + previous" {
		t.Fatalf("unexpected load mode %q", state.LoadMode)
	}
	if state.FileCount != 6 {
		t.Fatalf("expected 6 files, got %d", state.FileCount)
	}
	if state.ShardCount != 3 {
		t.Fatalf("expected 3 shards, got %d", state.ShardCount)
	}
	if state.LoadedShards != 2 {
		t.Fatalf("expected 2 loaded shards, got %d", state.LoadedShards)
	}
	if !state.HasCuratedMemory {
		t.Fatal("expected curated memory marker")
	}
	if !state.CompactionEnabled {
		t.Fatal("expected compaction to be enabled")
	}
	if state.CompactionTriggerToken == 0 || state.CompactionTargetToken == 0 {
		t.Fatal("expected derived compaction thresholds")
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
