package discordbot

import (
	"context"
	"strings"
	"time"

	"element-orion/internal/agent"
)

const dreamOKToken = "DREAM_OK"

func (s *Service) runDreamLoop(ctx context.Context) {
	if !s.cfg.DreamModeEnabled() {
		return
	}

	if s.withinDreamSleepHours(time.Now()) {
		s.runDreamMaintenance(ctx)
	}

	ticker := time.NewTicker(s.cfg.DreamModeInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.withinDreamSleepHours(time.Now()) {
				continue
			}
			s.runDreamMaintenance(ctx)
		}
	}
}

func (s *Service) runDreamMaintenance(ctx context.Context) {
	stopTyping := func() {}
	if s.cfg.DreamMode.UseIndicator {
		stopTyping = s.startTyping(strings.TrimSpace(s.cfg.Heartbeat.Target.ChannelID))
	}
	defer stopTyping()

	_, err := s.runner.Run(ctx, nil, buildDreamPrompt(), agent.ConversationContext{
		IsDirectMessage: true,
		IsDreamMode:     true,
		LightContext:    s.cfg.DreamMode.LightContext,
		ModelOverride:   s.cfg.DreamModeModel(),
		Now:             time.Now(),
	}, func(event agent.Event) {})
	if err != nil {
		s.audit.Write("error", "", map[string]any{
			"op":    "dream_mode_run",
			"model": s.cfg.DreamModeModel(),
			"error": err.Error(),
		})
		return
	}
	s.audit.Write("dream_mode", "", map[string]any{
		"op":    "dream_mode_run",
		"model": s.cfg.DreamModeModel(),
	})
}

func buildDreamPrompt() string {
	return strings.Join([]string{
		"This is a dream mode maintenance run.",
		"Use this quiet window to review the configured memory directory and improve it.",
		"Read the memory files, organize duplicated or stale details, compact weak summaries, and preserve concrete facts that matter.",
		"Prefer small truthful edits over broad rewrites.",
		"Verify every saved memory file after editing it.",
		"If everything is already in good shape, reply with DREAM_OK.",
		"If you make verified memory improvements, reply with DREAM_OK when finished.",
	}, " ")
}

func (s *Service) withinDreamSleepHours(now time.Time) bool {
	start := strings.TrimSpace(s.cfg.DreamMode.SleepHours.Start)
	end := strings.TrimSpace(s.cfg.DreamMode.SleepHours.End)
	if start == "" || end == "" {
		return false
	}

	location, err := s.cfg.DreamModeLocation()
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "dream_mode_timezone", "error": err.Error()})
		return false
	}

	startMinutes, err := parseHeartbeatClock(start)
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "dream_mode_start_time", "error": err.Error()})
		return false
	}
	endMinutes, err := parseHeartbeatClock(end)
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "dream_mode_end_time", "error": err.Error()})
		return false
	}

	localNow := now.In(location)
	currentMinutes := localNow.Hour()*60 + localNow.Minute()
	if startMinutes == endMinutes {
		return true
	}
	if startMinutes < endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}
