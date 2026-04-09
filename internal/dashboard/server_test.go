package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"element-orion/internal/auditlog"
	"element-orion/internal/config"
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

func TestBuildConfigStateSummarizesHeartbeatAndMCP(t *testing.T) {
	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot: "/workspace/lumen",
			SessionDir:    "/workspace/lumen/.element-orion",
		},
		LLM: config.LLMConfig{
			Model: "gpt-5.4",
		},
		Heartbeat: config.HeartbeatConfig{
			Every:             "30m",
			Model:             "gpt-heartbeat",
			LightContext:      true,
			IsolatedSession:   true,
			ShowOK:            false,
			ShowAlerts:        true,
			UseIndicator:      true,
			EventPollInterval: "5s",
			ActiveHours: config.HeartbeatActiveHoursConfig{
				Timezone: "Australia/Brisbane",
				Start:    "09:00",
				End:      "17:00",
			},
			Target: config.HeartbeatTargetConfig{
				ChannelID: "123",
				UserID:    "456",
			},
		},
		BackgroundTasks: config.BackgroundTasksConfig{
			DefaultMinRuntime:  "90s",
			InjectCurrentTime:  true,
			MaxEventLogEntries: 300,
			Sandbox: config.BackgroundTaskSandboxConfig{
				Enabled:     true,
				Provider:    "nspawn",
				Force:       false,
				AutoCleanup: true,
			},
		},
		Dashboard: config.DashboardConfig{
			Enabled:    true,
			ListenAddr: "127.0.0.1:8788",
			Path:       "/dashboard",
		},
		EventWebhook: config.EventWebhookConfig{
			Enabled:     true,
			ListenAddr:  "127.0.0.1:8787",
			Path:        "/event",
			DefaultMode: "next-heartbeat",
		},
		MCP: config.MCPConfig{
			Servers: []config.MCPServerConfig{
				{
					Name:      "exa",
					Enabled:   true,
					Transport: "http",
					Endpoint:  "https://mcp.exa.ai/mcp",
				},
				{
					Name:      "local",
					Enabled:   false,
					Transport: "stdio",
					Command:   "npx",
					Args:      []string{"-y", "exa-mcp-server"},
				},
			},
		},
	}

	state := buildConfigState(cfg)

	if len(state.Sections) != 5 {
		t.Fatalf("expected 5 config sections, got %d", len(state.Sections))
	}

	heartbeat := findConfigSection(state.Sections, "Heartbeat")
	if got := findConfigItem(heartbeat.Items, "Enabled"); got != "yes" {
		t.Fatalf("expected heartbeat enabled yes, got %q", got)
	}
	if got := findConfigItem(heartbeat.Items, "Context"); got != "light / isolated" {
		t.Fatalf("expected heartbeat context summary, got %q", got)
	}
	if got := findConfigItem(heartbeat.Items, "Active hours"); got != "09:00-17:00 (Australia/Brisbane)" {
		t.Fatalf("expected active hours summary, got %q", got)
	}
	if got := findConfigItem(heartbeat.Items, "Target"); got != "channel + user" {
		t.Fatalf("expected heartbeat target summary, got %q", got)
	}

	mcp := findConfigSection(state.Sections, "MCP")
	if got := findConfigItem(mcp.Items, "Servers"); got != "2 configured / 1 enabled" {
		t.Fatalf("expected MCP count summary, got %q", got)
	}
	if got := findConfigItem(mcp.Items, "exa"); got != "enabled · http · https://mcp.exa.ai/mcp" {
		t.Fatalf("expected exa endpoint summary, got %q", got)
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

func findConfigSection(sections []configSectionState, title string) configSectionState {
	for _, section := range sections {
		if section.Title == title {
			return section
		}
	}
	return configSectionState{}
}

func findConfigItem(items []configItemState, key string) string {
	for _, item := range items {
		if item.Key == key {
			return item.Value
		}
	}
	return ""
}
