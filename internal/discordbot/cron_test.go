package discordbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen-agent/internal/config"
)

func TestParseCronAtAcceptsLocalTime(t *testing.T) {
	now := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	parsed, err := ParseCronAt("2026-03-27 18:30", now, time.UTC)
	if err != nil {
		t.Fatalf("ParseCronAt returned error: %v", err)
	}
	if got, want := parsed, time.Date(2026, 3, 27, 18, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("expected parsed time %v, got %v", want, got)
	}
}

func TestParseCronAtRejectsPastTime(t *testing.T) {
	now := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	if _, err := ParseCronAt("2026-03-27 07:59", now, time.UTC); err == nil {
		t.Fatal("expected ParseCronAt to reject past times")
	}
}

func TestConsumeDueCronJobsKeepsFutureJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.json")
	job := cronJob{
		Text:      "Remind me later",
		DueAt:     time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal cron job: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write cron job: %v", err)
	}

	loaded, err := consumeDueCronJobs(dir, time.Date(2026, 3, 27, 8, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("consumeDueCronJobs returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected no due jobs yet, got %d", len(loaded))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected future cron job to remain on disk: %v", err)
	}
}

func TestEnqueueCronJobWritesJobFile(t *testing.T) {
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

	dueAt := time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC)
	if err := EnqueueCronJob(cfg, "Remind me once", dueAt); err != nil {
		t.Fatalf("EnqueueCronJob returned error: %v", err)
	}

	entries, err := os.ReadDir(cfg.CronJobsDir())
	if err != nil {
		t.Fatalf("read cron jobs dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one cron job file, got %d", len(entries))
	}
}
