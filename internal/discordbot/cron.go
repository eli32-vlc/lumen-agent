package discordbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"element-orion/internal/config"
	"element-orion/internal/tools"
)

const scheduledWakeupSource = "scheduled-wakeup"

type scheduledWakeup struct {
	info     tools.ScheduledWakeupInfo
	location *time.Location
	schedule cron.Schedule
}

type scheduledWakeupManager struct {
	cfg   config.Config
	mu    sync.RWMutex
	items map[string]*scheduledWakeup
}

func newScheduledWakeupManager(cfg config.Config) *scheduledWakeupManager {
	return &scheduledWakeupManager{
		cfg:   cfg,
		items: map[string]*scheduledWakeup{},
	}
}

func (m *scheduledWakeupManager) ScheduleHeartbeatWakeup(_ context.Context, options tools.ScheduledWakeupCreateOptions) (tools.ScheduledWakeupInfo, error) {
	if !m.cfg.HeartbeatEnabled() {
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("heartbeat wakeups require heartbeat delivery; configure heartbeat.target.channel_id and heartbeat.target.user_id")
	}

	now := time.Now()
	location, err := loadWakeupLocation(strings.TrimSpace(options.Timezone))
	if err != nil {
		return tools.ScheduledWakeupInfo{}, err
	}

	info := tools.ScheduledWakeupInfo{
		ID:        scheduledWakeupID(),
		Text:      strings.TrimSpace(options.Text),
		Timezone:  location.String(),
		CreatedAt: now.UTC(),
	}
	if info.Text == "" {
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("text must not be empty")
	}

	job := &scheduledWakeup{
		info:     info,
		location: location,
	}

	atValue := strings.TrimSpace(options.At)
	cronValue := strings.TrimSpace(options.Cron)
	switch {
	case atValue == "" && cronValue == "":
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("either at or cron must be provided")
	case atValue != "" && cronValue != "":
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("provide either at or cron, not both")
	case atValue != "":
		dueAt, err := parseScheduledWakeAt(atValue, now, location)
		if err != nil {
			return tools.ScheduledWakeupInfo{}, err
		}
		job.info.At = dueAt.UTC()
		job.info.NextRunAt = dueAt.UTC()
	default:
		schedule, normalizedCron, nextRunAt, err := parseScheduledWakeCron(cronValue, now, location)
		if err != nil {
			return tools.ScheduledWakeupInfo{}, err
		}
		job.schedule = schedule
		job.info.Cron = normalizedCron
		job.info.Recurring = true
		job.info.NextRunAt = nextRunAt.UTC()
	}

	m.mu.Lock()
	m.items[job.info.ID] = job
	m.mu.Unlock()

	return cloneScheduledWakeupInfo(job.info), nil
}

func (m *scheduledWakeupManager) ListScheduledWakeups(_ context.Context) ([]tools.ScheduledWakeupInfo, error) {
	m.mu.RLock()
	items := make([]tools.ScheduledWakeupInfo, 0, len(m.items))
	for _, item := range m.items {
		items = append(items, cloneScheduledWakeupInfo(item.info))
	}
	m.mu.RUnlock()

	slices.SortFunc(items, func(a, b tools.ScheduledWakeupInfo) int {
		switch {
		case a.NextRunAt.Before(b.NextRunAt):
			return -1
		case a.NextRunAt.After(b.NextRunAt):
			return 1
		default:
			return strings.Compare(a.ID, b.ID)
		}
	})
	return items, nil
}

func (m *scheduledWakeupManager) CancelScheduledWakeup(_ context.Context, id string) (tools.ScheduledWakeupInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("id must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[id]
	if !ok {
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("scheduled wakeup %q was not found", id)
	}
	delete(m.items, id)
	return cloneScheduledWakeupInfo(item.info), nil
}

func (m *scheduledWakeupManager) dueWakeups(now time.Time) []tools.ScheduledWakeupInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	due := make([]tools.ScheduledWakeupInfo, 0)
	for _, item := range m.items {
		if item.info.NextRunAt.IsZero() || item.info.NextRunAt.After(now.UTC()) {
			continue
		}
		due = append(due, cloneScheduledWakeupInfo(item.info))
	}

	slices.SortFunc(due, func(a, b tools.ScheduledWakeupInfo) int {
		switch {
		case a.NextRunAt.Before(b.NextRunAt):
			return -1
		case a.NextRunAt.After(b.NextRunAt):
			return 1
		default:
			return strings.Compare(a.ID, b.ID)
		}
	})
	return due
}

func (m *scheduledWakeupManager) acknowledgeDelivered(ids []string, deliveredAt time.Time) {
	if len(ids) == 0 {
		return
	}
	deliveredAt = deliveredAt.UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		item, ok := m.items[id]
		if !ok {
			continue
		}
		item.info.LastRunAt = item.info.NextRunAt.UTC()
		if item.schedule == nil {
			delete(m.items, id)
			continue
		}

		base := item.info.NextRunAt
		if deliveredAt.After(base) {
			base = deliveredAt
		}
		nextRun := item.schedule.Next(base.In(item.location))
		if nextRun.IsZero() {
			delete(m.items, id)
			continue
		}
		item.info.NextRunAt = nextRun.UTC()
	}
}

func parseScheduledWakeAt(value string, now time.Time, location *time.Location) (time.Time, error) {
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

func parseScheduledWakeCron(value string, now time.Time, location *time.Location) (cron.Schedule, string, time.Time, error) {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if trimmed == "" {
		return nil, "", time.Time{}, fmt.Errorf("cron must not be empty")
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(trimmed)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("parse cron: %w", err)
	}

	base := now.In(location)
	nextRunAt := schedule.Next(base)
	if nextRunAt.IsZero() {
		return nil, "", time.Time{}, fmt.Errorf("cron did not produce a future run time")
	}
	return schedule, trimmed, nextRunAt, nil
}

func loadWakeupLocation(name string) (*time.Location, error) {
	if name == "" {
		return time.Local, nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}
	return location, nil
}

func cloneScheduledWakeupInfo(info tools.ScheduledWakeupInfo) tools.ScheduledWakeupInfo {
	return info
}

func scheduledWakeupID() string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("wake-%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("wake-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix[:]))
}

func (s *Service) runScheduledWakeupLoop(ctx context.Context) {
	if s.scheduledWakeups == nil || !s.cfg.HeartbeatEnabled() {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			due := s.scheduledWakeups.dueWakeups(time.Now())
			if len(due) == 0 {
				continue
			}

			events := make([]heartbeatSystemEvent, 0, len(due))
			deliveredIDs := make([]string, 0, len(due))
			for _, item := range due {
				events = append(events, heartbeatSystemEvent{
					Text:      item.Text,
					Mode:      heartbeatModeNow,
					Source:    scheduledWakeupSource,
					DueAt:     item.NextRunAt,
					CreatedAt: item.CreatedAt,
				})
				deliveredIDs = append(deliveredIDs, item.ID)
			}

			if !s.enqueueHeartbeat(events, true) {
				continue
			}
			s.scheduledWakeups.acknowledgeDelivered(deliveredIDs, time.Now())
		}
	}
}
