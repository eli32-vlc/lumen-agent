package discordbot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"element-orion/internal/agent"
	"element-orion/internal/config"
	"element-orion/internal/llm"
	"element-orion/internal/tools"
)

func TestIsEmergencyStopCommand(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "exact", content: "/stop", want: true},
		{name: "with spacing", content: "   /stop   now", want: true},
		{name: "case insensitive", content: "/STOP", want: true},
		{name: "different command", content: "/start", want: false},
		{name: "plain message", content: "please stop", want: false},
		{name: "empty", content: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmergencyStopCommand(tt.content); got != tt.want {
				t.Fatalf("isEmergencyStopCommand(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestStopSessionCancelsAndRemoves(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	key := sessionKey{GuildID: "guild", ChannelID: "channel", UserID: "user"}
	service := &Service{
		sessions: map[string]*sessionState{
			key.String(): {
				Key:     key,
				Context: ctx,
				Cancel:  cancel,
			},
		},
	}

	if stopped := service.stopSession(key); !stopped {
		t.Fatal("expected stopSession to report an active session")
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected stopSession to cancel the session context")
	}

	if session := service.lookupSession(key); session != nil {
		t.Fatal("expected stopSession to remove the session from the map")
	}

	if stopped := service.stopSession(key); stopped {
		t.Fatal("expected stopSession to report false for a missing session")
	}
}

func TestSessionStillActiveChecksCurrentMappedSession(t *testing.T) {
	key := sessionKey{GuildID: "guild", ChannelID: "channel", UserID: "user"}
	active := &sessionState{Key: key}
	replaced := &sessionState{Key: key}
	service := &Service{
		sessions: map[string]*sessionState{
			key.String(): active,
		},
	}

	if !service.sessionStillActive(active) {
		t.Fatal("expected mapped session to be active")
	}
	if service.sessionStillActive(replaced) {
		t.Fatal("did not expect a different session instance with the same key to be active")
	}

	delete(service.sessions, key.String())
	if service.sessionStillActive(active) {
		t.Fatal("did not expect removed session to be active")
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped deadline", err: fmt.Errorf("run failed: %w", context.DeadlineExceeded), want: true},
		{name: "client timeout string", err: errors.New("send request: Post https://api.example.invalid: context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), want: true},
		{name: "gateway timeout", err: errors.New("API error (504): upstream timeout"), want: true},
		{name: "non timeout", err: errors.New("decode response: invalid character"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTimeoutError(tt.err); got != tt.want {
				t.Fatalf("isTimeoutError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestStatusReportIncludesContextAndTaskCounts(t *testing.T) {
	key := sessionKey{GuildID: "guild", ChannelID: "channel", UserID: "user"}
	workspace := t.TempDir()
	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot: workspace,
			SessionDir:    filepath.Join(workspace, ".element-orion"),
			MemoryDir:     filepath.Join(workspace, ".element-orion", "memory"),
			HistoryCompaction: config.AppHistoryCompactionConfig{
				Enabled:                true,
				TriggerTokens:          12000,
				TargetTokens:           8000,
				PreserveRecentMessages: 12,
			},
		},
		LLM: config.LLMConfig{
			ContextWindowTokens: 32000,
			MaxTokens:           4000,
		},
	}
	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()
	service := &Service{
		cfg:    cfg,
		runner: agent.NewRunner(cfg, nil, registry),
		sessions: map[string]*sessionState{
			key.String(): {
				ID:        "sess-1",
				Key:       key,
				Queue:     make(chan inboundPrompt, 4),
				History:   []llm.Message{{Role: "user", Content: "hello world"}},
				UpdatedAt: time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC),
			},
		},
		tasks: map[string]*backgroundTask{
			"queued":   {Status: backgroundTaskQueued},
			"running":  {Status: backgroundTaskRunning},
			"failed":   {Status: backgroundTaskFailed},
			"done":     {Status: backgroundTaskCompleted},
			"canceled": {Status: backgroundTaskCanceled},
			"worker": {
				Status:        backgroundTaskRunning,
				ChannelID:     key.ChannelID,
				CreatedAt:     time.Date(2026, 3, 29, 10, 1, 0, 0, time.UTC),
				SpawnMessages: 6,
				SpawnTokens:   554,
				History: []llm.Message{
					{Role: "user", Content: "worker snapshot"},
					{Role: "assistant", Content: "worker doing more"},
				},
			},
		},
	}

	report := service.statusReport(key)
	for _, snippet := range []string{
		"## Element Orion Check-In",
		"**Context**",
		"```text",
		"🛠️ Background jobs: 3 active (1 queued, 2 running), 1 done, 1 failed, 1 canceled",
		"🤖 worker: running, separate from this chat",
		"started with 6 msgs (~554 tok)",
		"merge-back: finish/fail handoff only",
		"🧠 Context usage: ",
		"▕",
		"**Background**",
		"**Chat**",
		"💬 1 open",
		"🧠 1 saved msgs",
		"📦 base+memory ~",
		"live history ~",
	} {
		if !contains(report, snippet) {
			t.Fatalf("expected report to contain %q, got:\n%s", snippet, report)
		}
	}
}

func TestStatusReportWithoutSessionIsFriendly(t *testing.T) {
	key := sessionKey{GuildID: "guild", ChannelID: "channel", UserID: "user"}
	workspace := t.TempDir()
	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot: workspace,
			SessionDir:    filepath.Join(workspace, ".element-orion"),
			MemoryDir:     filepath.Join(workspace, ".element-orion", "memory"),
		},
		LLM: config.LLMConfig{
			ContextWindowTokens: 1000000,
			MaxTokens:           64000,
		},
	}
	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()
	service := &Service{
		cfg:      cfg,
		runner:   agent.NewRunner(cfg, nil, registry),
		sessions: map[string]*sessionState{},
		tasks:    map[string]*backgroundTask{},
	}

	report := service.statusReport(key)
	for _, snippet := range []string{
		"## Element Orion Check-In",
		"🧠 Context usage: ",
		"🛠️ Background jobs: none",
		"💤 no active chat in this channel",
	} {
		if !contains(report, snippet) {
			t.Fatalf("expected report to contain %q, got:\n%s", snippet, report)
		}
	}
}

func TestMemoryReportShowsShardSummary(t *testing.T) {
	key := sessionKey{GuildID: "", ChannelID: "dm-channel", UserID: "user"}
	workspace := t.TempDir()
	memoryRoot := filepath.Join(workspace, ".element-orion", "memory")
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatalf("mkdir memory root: %v", err)
	}

	curatedPath := filepath.Join(memoryRoot, "MEMORY.md")
	currentPath := filepath.Join(memoryRoot, "2026-03-29-PM.md")
	previousPath := filepath.Join(memoryRoot, "2026-03-29-AM.md")
	olderPath := filepath.Join(memoryRoot, "2026-03-28-PM.md")
	for _, item := range []struct {
		path    string
		content string
		modTime time.Time
	}{
		{curatedPath, "curated memory", time.Date(2026, 3, 29, 8, 30, 0, 0, time.UTC)},
		{currentPath, "current shard", time.Date(2026, 3, 29, 3, 11, 0, 0, time.UTC)},
		{previousPath, "previous shard", time.Date(2026, 3, 29, 2, 58, 0, 0, time.UTC)},
		{olderPath, "older shard", time.Date(2026, 3, 28, 22, 14, 0, 0, time.UTC)},
	} {
		if err := os.WriteFile(item.path, []byte(item.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", item.path, err)
		}
		if err := os.Chtimes(item.path, item.modTime, item.modTime); err != nil {
			t.Fatalf("chtimes %s: %v", item.path, err)
		}
	}

	service := &Service{
		cfg: config.Config{
			App: config.AppConfig{
				WorkspaceRoot:       workspace,
				SessionDir:          filepath.Join(workspace, ".element-orion"),
				MemoryDir:           memoryRoot,
				LoadAllMemoryShards: false,
			},
		},
	}

	originalLocal := time.Local
	time.Local = time.FixedZone("AEST", 10*60*60)
	defer func() { time.Local = originalLocal }()

	report := service.memoryReport(key)
	for _, snippet := range []string{
		"## 🧠 Memory",
		"**Memory**",
		"```text",
		"Status      enabled",
		"Root        ./.element-orion/memory",
		"Shards      3 total, 2 loaded now, current + previous half-day",
		"Range       2026-03-29 08:14 AEST -> 2026-03-29 13:11 AEST",
		"Size        ",
		"Prompt cost ~",
		"Last write  2026-03-29 13:11 AEST",
		"Mode        append-only shard memory",
	} {
		if !contains(report, snippet) {
			t.Fatalf("expected memory report to contain %q, got:\n%s", snippet, report)
		}
	}
}

func TestMemoryReportUsesSharedGuildMemoryRoot(t *testing.T) {
	key := sessionKey{GuildID: "guild-1", ChannelID: "channel-1", UserID: "user-1"}
	workspace := t.TempDir()
	sessionDir := filepath.Join(workspace, ".element-orion")
	guildMemoryRoot := filepath.Join(sessionDir, "guild-memory", "guild-1", "channel-1")
	if err := os.MkdirAll(guildMemoryRoot, 0o755); err != nil {
		t.Fatalf("mkdir guild memory root: %v", err)
	}
	path := filepath.Join(guildMemoryRoot, "2026-03-29-PM.md")
	if err := os.WriteFile(path, []byte("shared shard"), 0o644); err != nil {
		t.Fatalf("write guild shard: %v", err)
	}

	service := &Service{
		cfg: config.Config{
			App: config.AppConfig{
				WorkspaceRoot: workspace,
				SessionDir:    sessionDir,
				MemoryDir:     filepath.Join(sessionDir, "memory"),
			},
			Discord: config.DiscordConfig{
				GuildSessionScope: "channel",
			},
		},
	}

	report := service.memoryReport(key)
	if !contains(report, "Root        ./.element-orion/guild-memory/guild-1/channel-1") {
		t.Fatalf("expected guild memory root in report, got:\n%s", report)
	}
}

func TestCompactSessionForKeyCompactsHistory(t *testing.T) {
	key := sessionKey{GuildID: "guild", ChannelID: "channel", UserID: "user"}
	service := &Service{
		cfg: config.Config{
			App: config.AppConfig{
				HistoryCompaction: config.AppHistoryCompactionConfig{
					Enabled:                true,
					TriggerTokens:          20,
					TargetTokens:           10,
					PreserveRecentMessages: 2,
				},
			},
		},
		sessions: map[string]*sessionState{
			key.String(): {
				ID:       "sess-compact",
				Key:      key,
				FilePath: t.TempDir() + "/session.json",
				Queue:    make(chan inboundPrompt, 1),
				RunLock:  &sync.Mutex{},
				History: []llm.Message{
					{Role: "user", Content: "first " + strings.Repeat("hello ", 20)},
					{Role: "assistant", Content: "second " + strings.Repeat("world ", 20)},
					{Role: "user", Content: "keep user"},
					{Role: "assistant", Content: "keep assistant"},
				},
			},
		},
	}

	message, err := service.compactSessionForKey(key)
	if err != nil {
		t.Fatalf("compactSessionForKey returned error: %v", err)
	}
	if !contains(message, "Compacted session `sess-compact`") {
		t.Fatalf("unexpected compact message: %s", message)
	}
	history, _ := service.sessions[key.String()].snapshotForRun()
	if len(history) != 3 {
		t.Fatalf("expected compacted history length 3, got %d", len(history))
	}
	if history[0].Role != "system" {
		t.Fatalf("expected first history item to be system summary, got %#v", history[0])
	}
}

func contains(haystack string, needle string) bool {
	return strings.Contains(haystack, needle)
}
