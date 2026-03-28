package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen-agent/internal/config"
)

func TestParseHeartbeatWakeAtAcceptsLocalTime(t *testing.T) {
	now := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	parsed, err := parseHeartbeatWakeAt("2026-03-27 18:30", now, time.UTC)
	if err != nil {
		t.Fatalf("parseHeartbeatWakeAt returned error: %v", err)
	}
	if want := time.Date(2026, 3, 27, 18, 30, 0, 0, time.UTC); !parsed.Equal(want) {
		t.Fatalf("expected %v, got %v", want, parsed)
	}
}

func TestHandleScheduleHeartbeatWakeupWritesJobFile(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		App: config.AppConfig{
			SessionDir: root,
		},
		Heartbeat: config.HeartbeatConfig{
			Every: "30m",
			Target: config.HeartbeatTargetConfig{
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
	}

	registry, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	payload, err := json.Marshal(map[string]any{
		"text": "Morning check-in",
		"at":   time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	result, err := registry.handleScheduleHeartbeatWakeup(context.Background(), payload)
	if err != nil {
		t.Fatalf("handleScheduleHeartbeatWakeup returned error: %v", err)
	}
	if !strings.Contains(result, "Morning check-in") {
		t.Fatalf("expected result to include scheduled text, got %q", result)
	}

	entries, err := os.ReadDir(filepath.Join(root, "cron-jobs"))
	if err != nil {
		t.Fatalf("read cron jobs dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one cron job, got %d", len(entries))
	}
}
