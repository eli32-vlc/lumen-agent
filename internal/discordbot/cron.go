package discordbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lumen-agent/internal/config"
)

type cronJob struct {
	Text      string    `json:"text"`
	DueAt     time.Time `json:"due_at"`
	CreatedAt time.Time `json:"created_at"`
}

type queuedCronJob struct {
	Path string
	Job  cronJob
}

func EnqueueCronJob(cfg config.Config, text string, dueAt time.Time) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("cron job text must not be empty")
	}
	if !cfg.HeartbeatEnabled() {
		return fmt.Errorf("cron jobs require heartbeat delivery; configure heartbeat.target.channel_id and heartbeat.target.user_id")
	}
	if dueAt.IsZero() {
		return fmt.Errorf("cron job due time must not be zero")
	}

	if err := os.MkdirAll(cfg.CronJobsDir(), 0o755); err != nil {
		return fmt.Errorf("create cron jobs dir: %w", err)
	}

	data, err := json.MarshalIndent(cronJob{
		Text:      text,
		DueAt:     dueAt.UTC(),
		CreatedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cron job: %w", err)
	}

	name, err := cronJobFileName()
	if err != nil {
		return err
	}

	path := filepath.Join(cfg.CronJobsDir(), name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cron job: %w", err)
	}

	return nil
}

func ParseCronAt(value string, now time.Time, location *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("cron --at must not be empty")
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
				return time.Time{}, fmt.Errorf("cron --at must be in the future")
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
				return time.Time{}, fmt.Errorf("cron --at must be in the future")
			}
			return parsed.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported cron --at time format")
}

func (s *Service) runCronLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.HeartbeatEventPollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jobs, err := consumeDueCronJobs(s.cfg.CronJobsDir(), time.Now().UTC())
			if err != nil {
				s.audit.Write("error", "", map[string]any{"op": "load_due_cron_jobs", "error": err.Error()})
				continue
			}
			if len(jobs) == 0 {
				continue
			}
			if !s.enqueueHeartbeat(cronJobsToHeartbeatEvents(jobs), true) {
				continue
			}
			if err := acknowledgeCronJobs(jobs); err != nil {
				s.audit.Write("error", "", map[string]any{"op": "ack_due_cron_jobs", "error": err.Error()})
			}
		}
	}
}

func consumeDueCronJobs(dir string, now time.Time) ([]queuedCronJob, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	jobs := make([]queuedCronJob, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var job cronJob
		if err := json.Unmarshal(data, &job); err != nil {
			_ = os.Remove(path)
			continue
		}
		if strings.TrimSpace(job.Text) == "" || job.DueAt.IsZero() {
			_ = os.Remove(path)
			continue
		}
		if job.DueAt.After(now) {
			continue
		}
		jobs = append(jobs, queuedCronJob{Path: path, Job: job})
	}

	return jobs, nil
}

func cronJobsToHeartbeatEvents(jobs []queuedCronJob) []heartbeatSystemEvent {
	if len(jobs) == 0 {
		return nil
	}

	events := make([]heartbeatSystemEvent, 0, len(jobs))
	for _, job := range jobs {
		events = append(events, heartbeatSystemEvent{
			Text:      job.Job.Text,
			Mode:      heartbeatModeNow,
			Source:    "cron",
			DueAt:     job.Job.DueAt,
			CreatedAt: job.Job.CreatedAt,
		})
	}
	return events
}

func acknowledgeCronJobs(jobs []queuedCronJob) error {
	for _, job := range jobs {
		if err := os.Remove(job.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func cronJobFileName() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate cron job id: %w", err)
	}
	return fmt.Sprintf("cron-job-%s-%s", time.Now().UTC().Format("20060102-150405.000000000"), hex.EncodeToString(suffix[:])), nil
}
