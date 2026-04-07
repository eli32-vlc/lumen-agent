package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ScheduledWakeupInfo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Cron      string    `json:"cron,omitempty"`
	At        time.Time `json:"at,omitempty"`
	Timezone  string    `json:"timezone,omitempty"`
	Recurring bool      `json:"recurring"`
	CreatedAt time.Time `json:"created_at"`
	NextRunAt time.Time `json:"next_run_at,omitempty"`
	LastRunAt time.Time `json:"last_run_at,omitempty"`
}

type ScheduledWakeupCreateOptions struct {
	Text     string
	At       string
	Cron     string
	Timezone string
}

type ScheduledWakeupManager interface {
	ScheduleHeartbeatWakeup(context.Context, ScheduledWakeupCreateOptions) (ScheduledWakeupInfo, error)
	ListScheduledWakeups(context.Context) ([]ScheduledWakeupInfo, error)
	CancelScheduledWakeup(context.Context, string) (ScheduledWakeupInfo, error)
}

func (r *Registry) SetScheduledWakeupManager(manager ScheduledWakeupManager) {
	r.scheduledWakeups = manager
}

func (r *Registry) registerHeartbeatWakeTools() {
	r.register(
		"schedule_heartbeat_wakeup",
		"Schedule a heartbeat wakeup inside the running app. Use either an exact future time or a recurring 5-field cron expression in the chosen local timezone.",
		objectSchema(map[string]any{
			"text":     stringSchema("What the heartbeat run should do when it wakes up."),
			"at":       stringSchema("Optional exact future time. Accepts RFC3339 or local time like 2026-03-27 18:30. Use this for one-shot wakeups."),
			"cron":     stringSchema("Optional recurring 5-field cron expression in the chosen local timezone, for example 0 9 * * 1-5."),
			"timezone": stringSchema("Optional IANA timezone name used for local-style times and cron interpretation. Defaults to the machine local timezone."),
		}, "text"),
		r.handleScheduleHeartbeatWakeup,
	)

	r.register(
		"list_scheduled_wakeups",
		"List scheduled heartbeat wakeups managed by the running app.",
		objectSchema(map[string]any{}),
		r.handleListScheduledWakeups,
	)

	r.register(
		"cancel_scheduled_wakeup",
		"Cancel a scheduled heartbeat wakeup by ID.",
		objectSchema(map[string]any{
			"id": stringSchema("Scheduled wakeup ID."),
		}, "id"),
		r.handleCancelScheduledWakeup,
	)
}

func (r *Registry) handleScheduleHeartbeatWakeup(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Text     string `json:"text"`
		At       string `json:"at"`
		Cron     string `json:"cron"`
		Timezone string `json:"timezone"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.scheduledWakeups == nil {
		return "", fmt.Errorf("scheduled wakeup manager is not available")
	}

	text := strings.TrimSpace(input.Text)
	at := strings.TrimSpace(input.At)
	cron := strings.TrimSpace(input.Cron)
	switch {
	case text == "":
		return "", fmt.Errorf("text must not be empty")
	case at == "" && cron == "":
		return "", fmt.Errorf("either at or cron must be provided")
	case at != "" && cron != "":
		return "", fmt.Errorf("provide either at or cron, not both")
	}

	info, err := r.scheduledWakeups.ScheduleHeartbeatWakeup(ctx, ScheduledWakeupCreateOptions{
		Text:     text,
		At:       at,
		Cron:     cron,
		Timezone: strings.TrimSpace(input.Timezone),
	})
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}

func (r *Registry) handleListScheduledWakeups(ctx context.Context, _ json.RawMessage) (string, error) {
	if r.scheduledWakeups == nil {
		return "", fmt.Errorf("scheduled wakeup manager is not available")
	}
	items, err := r.scheduledWakeups.ListScheduledWakeups(ctx)
	if err != nil {
		return "", err
	}
	return jsonResult(map[string]any{"wakeups": items})
}

func (r *Registry) handleCancelScheduledWakeup(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		ID string `json:"id"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.scheduledWakeups == nil {
		return "", fmt.Errorf("scheduled wakeup manager is not available")
	}
	info, err := r.scheduledWakeups.CancelScheduledWakeup(ctx, strings.TrimSpace(input.ID))
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}
