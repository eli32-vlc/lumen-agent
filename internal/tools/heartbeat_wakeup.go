package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type scheduledHeartbeatWakeup struct {
	Text      string    `json:"text"`
	DueAt     time.Time `json:"due_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *Registry) registerHeartbeatWakeTools() {
	r.register(
		"schedule_heartbeat_wakeup",
		"Schedule a precise one-shot heartbeat wakeup for a future local or RFC3339 time.",
		objectSchema(map[string]any{
			"text":     stringSchema("What the heartbeat run should do when it wakes up."),
			"at":       stringSchema("Exact future time. Accepts RFC3339 or local time like 2026-03-27 18:30."),
			"timezone": stringSchema("Optional IANA timezone name used for local-style times. Defaults to the machine local timezone."),
		}, "text", "at"),
		r.handleScheduleHeartbeatWakeup,
	)
}

func (r *Registry) handleScheduleHeartbeatWakeup(_ context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Text     string `json:"text"`
		At       string `json:"at"`
		Timezone string `json:"timezone"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	text := strings.TrimSpace(input.Text)
	if text == "" {
		return "", fmt.Errorf("text must not be empty")
	}
	if !r.cfg.HeartbeatEnabled() {
		return "", fmt.Errorf("heartbeat wakeups require heartbeat delivery; configure heartbeat.target.channel_id and heartbeat.target.user_id")
	}

	location := time.Local
	if zone := strings.TrimSpace(input.Timezone); zone != "" {
		loaded, err := time.LoadLocation(zone)
		if err != nil {
			return "", fmt.Errorf("load timezone: %w", err)
		}
		location = loaded
	}

	dueAt, err := parseHeartbeatWakeAt(input.At, time.Now(), location)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(r.cfg.CronJobsDir(), 0o755); err != nil {
		return "", fmt.Errorf("create cron jobs dir: %w", err)
	}

	job := scheduledHeartbeatWakeup{
		Text:      text,
		DueAt:     dueAt.UTC(),
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode heartbeat wakeup: %w", err)
	}

	name, err := heartbeatWakeupFileName()
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.cfg.CronJobsDir(), name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write heartbeat wakeup: %w", err)
	}

	return jsonResult(map[string]any{
		"text":               text,
		"scheduled_at":       dueAt.UTC(),
		"scheduled_at_local": dueAt.In(location).Format("2006-01-02 15:04:05 MST"),
		"scheduled_at_utc":   dueAt.UTC().Format(time.RFC3339),
		"timezone":           location.String(),
		"path":               path,
	})
}

func parseHeartbeatWakeAt(value string, now time.Time, location *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("at must not be empty")
	}
	if location == nil {
		location = time.Local
	}

	layoutsWithZone := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04Z07:00",
	}
	for _, layout := range layoutsWithZone {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			if !parsed.After(now) {
				return time.Time{}, fmt.Errorf("at must be in the future")
			}
			return parsed.UTC(), nil
		}
	}

	localLayouts := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
	}
	for _, layout := range localLayouts {
		if parsed, err := time.ParseInLocation(layout, trimmed, location); err == nil {
			if !parsed.After(now.In(location)) {
				return time.Time{}, fmt.Errorf("at must be in the future")
			}
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format")
}

func heartbeatWakeupFileName() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate heartbeat wakeup id: %w", err)
	}
	return fmt.Sprintf("cron-job-%s-%s", time.Now().UTC().Format("20060102-150405.000000000"), hex.EncodeToString(suffix[:])), nil
}
