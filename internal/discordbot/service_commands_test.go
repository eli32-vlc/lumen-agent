package discordbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen-agent/internal/config"
	"lumen-agent/internal/llm"
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
	service := &Service{
		cfg: config.Config{
			LLM: config.LLMConfig{
				ContextWindowTokens: 32000,
				MaxTokens:           4000,
			},
			App: config.AppConfig{
				HistoryCompaction: config.AppHistoryCompactionConfig{
					Enabled:                true,
					TriggerTokens:          12000,
					TargetTokens:           8000,
					PreserveRecentMessages: 12,
				},
			},
		},
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
		},
	}

	report := service.statusReport(key)
	for _, snippet := range []string{
		"Lumen status",
		"Background tasks: 1 queued, 1 running, 1 completed, 1 failed, 1 canceled",
		"Context window: 32000 tokens",
		"Input budget: 28000 tokens",
		"Current session: sess-1",
		"Current history: 1 messages, ~",
	} {
		if !contains(report, snippet) {
			t.Fatalf("expected report to contain %q, got:\n%s", snippet, report)
		}
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
