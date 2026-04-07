package discordbot

import (
	"context"
	"fmt"

	"lumen-agent/internal/tools"
)

func (s *Service) ScheduleHeartbeatWakeup(ctx context.Context, options tools.ScheduledWakeupCreateOptions) (tools.ScheduledWakeupInfo, error) {
	if s == nil || s.scheduledWakeups == nil {
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("scheduled wakeup manager is not available")
	}
	return s.scheduledWakeups.ScheduleHeartbeatWakeup(ctx, options)
}

func (s *Service) ListScheduledWakeups(ctx context.Context) ([]tools.ScheduledWakeupInfo, error) {
	if s == nil || s.scheduledWakeups == nil {
		return nil, fmt.Errorf("scheduled wakeup manager is not available")
	}
	return s.scheduledWakeups.ListScheduledWakeups(ctx)
}

func (s *Service) CancelScheduledWakeup(ctx context.Context, id string) (tools.ScheduledWakeupInfo, error) {
	if s == nil || s.scheduledWakeups == nil {
		return tools.ScheduledWakeupInfo{}, fmt.Errorf("scheduled wakeup manager is not available")
	}
	return s.scheduledWakeups.CancelScheduledWakeup(ctx, id)
}
