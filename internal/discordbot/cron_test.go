package discordbot

import (
	"context"
	"testing"
	"time"

	"lumen-agent/internal/config"
	"lumen-agent/internal/tools"
)

func TestParseScheduledWakeAtAcceptsLocalTime(t *testing.T) {
	now := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	parsed, err := parseScheduledWakeAt("2026-03-27 18:30", now, time.UTC)
	if err != nil {
		t.Fatalf("parseScheduledWakeAt returned error: %v", err)
	}
	if got, want := parsed, time.Date(2026, 3, 27, 18, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("expected parsed time %v, got %v", want, got)
	}
}

func TestParseScheduledWakeAtRejectsPastTime(t *testing.T) {
	now := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	if _, err := parseScheduledWakeAt("2026-03-27 07:59", now, time.UTC); err == nil {
		t.Fatal("expected parseScheduledWakeAt to reject past times")
	}
}

func TestParseScheduledWakeCronComputesNextRun(t *testing.T) {
	now := time.Date(2026, 3, 27, 8, 15, 0, 0, time.UTC)
	_, cronExpr, nextRun, err := parseScheduledWakeCron("0 9 * * 1-5", now, time.UTC)
	if err != nil {
		t.Fatalf("parseScheduledWakeCron returned error: %v", err)
	}
	if cronExpr != "0 9 * * 1-5" {
		t.Fatalf("expected normalized cron, got %q", cronExpr)
	}
	if want := time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC); !nextRun.Equal(want) {
		t.Fatalf("expected next run %v, got %v", want, nextRun)
	}
}

func TestScheduledWakeupManagerSchedulesAndCancels(t *testing.T) {
	manager := newScheduledWakeupManager(config.Config{
		Heartbeat: config.HeartbeatConfig{
			Every: "30m",
			Target: config.HeartbeatTargetConfig{
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
	})

	info, err := manager.ScheduleHeartbeatWakeup(context.Background(), scheduledWakeupOptions("Check deploy", "", "0 9 * * 1-5"))
	if err != nil {
		t.Fatalf("ScheduleHeartbeatWakeup returned error: %v", err)
	}
	if !info.Recurring {
		t.Fatalf("expected recurring wakeup, got %#v", info)
	}

	items, err := manager.ListScheduledWakeups(context.Background())
	if err != nil {
		t.Fatalf("ListScheduledWakeups returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one wakeup, got %d", len(items))
	}

	canceled, err := manager.CancelScheduledWakeup(context.Background(), info.ID)
	if err != nil {
		t.Fatalf("CancelScheduledWakeup returned error: %v", err)
	}
	if canceled.ID != info.ID {
		t.Fatalf("expected canceled wakeup %q, got %q", info.ID, canceled.ID)
	}
}

func TestScheduledWakeupManagerAcknowledgeDeliveredReschedulesRecurring(t *testing.T) {
	manager := newScheduledWakeupManager(config.Config{
		Heartbeat: config.HeartbeatConfig{
			Every: "30m",
			Target: config.HeartbeatTargetConfig{
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
	})

	info, err := manager.ScheduleHeartbeatWakeup(context.Background(), scheduledWakeupOptions("Check deploy", "", "* * * * *"))
	if err != nil {
		t.Fatalf("ScheduleHeartbeatWakeup returned error: %v", err)
	}

	manager.mu.Lock()
	manager.items[info.ID].info.NextRunAt = time.Now().Add(-time.Second).UTC()
	manager.mu.Unlock()

	due := manager.dueWakeups(time.Now())
	if len(due) != 1 || due[0].ID != info.ID {
		t.Fatalf("expected wakeup %q to be due, got %#v", info.ID, due)
	}

	manager.acknowledgeDelivered([]string{info.ID}, time.Now())

	items, err := manager.ListScheduledWakeups(context.Background())
	if err != nil {
		t.Fatalf("ListScheduledWakeups returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected recurring wakeup to remain scheduled, got %#v", items)
	}
	if !items[0].NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("expected recurring wakeup to move to the future, got %#v", items[0])
	}
}

func TestScheduledWakeupManagerAcknowledgeDeliveredRemovesOneShot(t *testing.T) {
	manager := newScheduledWakeupManager(config.Config{
		Heartbeat: config.HeartbeatConfig{
			Every: "30m",
			Target: config.HeartbeatTargetConfig{
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
	})

	info, err := manager.ScheduleHeartbeatWakeup(context.Background(), scheduledWakeupOptions("One shot", time.Now().Add(time.Hour).Format(time.RFC3339), ""))
	if err != nil {
		t.Fatalf("ScheduleHeartbeatWakeup returned error: %v", err)
	}

	manager.mu.Lock()
	manager.items[info.ID].info.NextRunAt = time.Now().Add(-time.Second).UTC()
	manager.mu.Unlock()

	manager.acknowledgeDelivered([]string{info.ID}, time.Now())

	items, err := manager.ListScheduledWakeups(context.Background())
	if err != nil {
		t.Fatalf("ListScheduledWakeups returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected one-shot wakeup to be removed, got %#v", items)
	}
}

func scheduledWakeupOptions(text string, at string, cron string) tools.ScheduledWakeupCreateOptions {
	return tools.ScheduledWakeupCreateOptions{
		Text: text,
		At:   at,
		Cron: cron,
	}
}
