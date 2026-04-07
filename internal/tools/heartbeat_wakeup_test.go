package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lumen-agent/internal/config"
)

type scheduledWakeupManagerStub struct {
	scheduled []ScheduledWakeupInfo
	canceled  []string
	lastInput ScheduledWakeupCreateOptions
}

func (s *scheduledWakeupManagerStub) ScheduleHeartbeatWakeup(_ context.Context, input ScheduledWakeupCreateOptions) (ScheduledWakeupInfo, error) {
	s.lastInput = input
	info := ScheduledWakeupInfo{
		ID:        "wake-1",
		Text:      input.Text,
		Cron:      input.Cron,
		Timezone:  input.Timezone,
		Recurring: strings.TrimSpace(input.Cron) != "",
		CreatedAt: time.Now().UTC(),
		NextRunAt: time.Now().Add(time.Hour).UTC(),
	}
	if strings.TrimSpace(input.At) != "" {
		info.At = info.NextRunAt
	}
	s.scheduled = append(s.scheduled, info)
	return info, nil
}

func (s *scheduledWakeupManagerStub) ListScheduledWakeups(_ context.Context) ([]ScheduledWakeupInfo, error) {
	return append([]ScheduledWakeupInfo(nil), s.scheduled...), nil
}

func (s *scheduledWakeupManagerStub) CancelScheduledWakeup(_ context.Context, id string) (ScheduledWakeupInfo, error) {
	s.canceled = append(s.canceled, id)
	return ScheduledWakeupInfo{ID: id}, nil
}

func TestHandleScheduleHeartbeatWakeupUsesManagerForOneShot(t *testing.T) {
	registry, err := NewRegistry(config.Config{})
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	manager := &scheduledWakeupManagerStub{}
	registry.SetScheduledWakeupManager(manager)

	payload, err := json.Marshal(map[string]any{
		"text":     "Morning check-in",
		"at":       "2026-03-27T10:00:00Z",
		"timezone": "Australia/Brisbane",
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
	if manager.lastInput.At != "2026-03-27T10:00:00Z" {
		t.Fatalf("expected manager to receive at input, got %#v", manager.lastInput)
	}
}

func TestHandleScheduleHeartbeatWakeupUsesManagerForCron(t *testing.T) {
	registry, err := NewRegistry(config.Config{})
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	manager := &scheduledWakeupManagerStub{}
	registry.SetScheduledWakeupManager(manager)

	payload, err := json.Marshal(map[string]any{
		"text": "Weekday standup",
		"cron": "0 9 * * 1-5",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	result, err := registry.handleScheduleHeartbeatWakeup(context.Background(), payload)
	if err != nil {
		t.Fatalf("handleScheduleHeartbeatWakeup returned error: %v", err)
	}
	if !strings.Contains(result, "0 9 * * 1-5") {
		t.Fatalf("expected result to include cron expression, got %q", result)
	}
	if manager.lastInput.Cron != "0 9 * * 1-5" {
		t.Fatalf("expected manager to receive cron input, got %#v", manager.lastInput)
	}
}

func TestHandleListAndCancelScheduledWakeups(t *testing.T) {
	registry, err := NewRegistry(config.Config{})
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	manager := &scheduledWakeupManagerStub{
		scheduled: []ScheduledWakeupInfo{{ID: "wake-1", Text: "Ping"}},
	}
	registry.SetScheduledWakeupManager(manager)

	listResult, err := registry.handleListScheduledWakeups(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleListScheduledWakeups returned error: %v", err)
	}
	if !strings.Contains(listResult, "wake-1") {
		t.Fatalf("expected list result to contain wakeup id, got %q", listResult)
	}

	payload, err := json.Marshal(map[string]any{"id": "wake-1"})
	if err != nil {
		t.Fatalf("marshal cancel payload: %v", err)
	}
	cancelResult, err := registry.handleCancelScheduledWakeup(context.Background(), payload)
	if err != nil {
		t.Fatalf("handleCancelScheduledWakeup returned error: %v", err)
	}
	if !strings.Contains(cancelResult, "wake-1") {
		t.Fatalf("expected cancel result to contain wakeup id, got %q", cancelResult)
	}
	if len(manager.canceled) != 1 || manager.canceled[0] != "wake-1" {
		t.Fatalf("expected wake-1 to be canceled, got %#v", manager.canceled)
	}
}
