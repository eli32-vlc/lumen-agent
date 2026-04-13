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

	conversation := agent.ConversationContext{
		IsDirectMessage: true,
		IsDreamMode:     true,
		LightContext:    s.cfg.DreamMode.LightContext,
		ModelOverride:   s.cfg.DreamModeModel(),
		Now:             time.Now(),
	}
	estimate := s.runner.EstimateContextUsage(nil, conversation, buildDreamPrompt(), nil)
	s.audit.Write("dream_context", "", map[string]any{
		"model":                s.cfg.DreamModeModel(),
		"system_prompt_tokens": estimate.SystemPromptTokens,
		"total_input_tokens":   estimate.TotalInputTokens,
		"input_budget_tokens":  estimate.InputBudgetTokens,
	})

	_, err := s.runner.Run(ctx, nil, buildDreamPrompt(), conversation, func(event agent.Event) {
		s.logDreamEvent(event)
	})
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

func (s *Service) logDreamEvent(event agent.Event) {
	switch event.Kind {
	case agent.EventToolStarted:
		s.audit.Write("dream_tool_start", "", map[string]any{
			"tool":        event.ToolName,
			"detail":      event.Detail,
			"full_detail": event.FullDetail,
		})
	case agent.EventToolFinished:
		s.audit.Write("dream_tool_done", "", map[string]any{
			"tool":        event.ToolName,
			"detail":      event.Detail,
			"full_detail": event.FullDetail,
			"duration_ms": event.DurationMS,
			"success":     event.Success,
		})
	case agent.EventModelDone:
		data := map[string]any{
			"duration_ms": event.DurationMS,
			"tokens":      event.TokenCount,
			"model":       s.cfg.DreamModeModel(),
		}
		for key, value := range event.Data {
			data[key] = value
		}
		s.audit.Write("dream_model_done", "", data)
	case agent.EventStatus:
		data := map[string]any{
			"message": event.Message,
			"model":   s.cfg.DreamModeModel(),
		}
		if strings.TrimSpace(event.Detail) != "" {
			data["detail"] = event.Detail
		}
		if strings.TrimSpace(event.FullDetail) != "" {
			data["full_detail"] = event.FullDetail
		}
		for key, value := range event.Data {
			data[key] = value
		}
		s.audit.Write("dream_status", "", data)
	case agent.EventAssistant:
		if strings.TrimSpace(event.Message) == "" || strings.TrimSpace(event.Message) == agent.NoReplyToken {
			return
		}
		s.audit.Write("dream_assistant", "", map[string]any{
			"message": event.Message,
			"length":  len(event.Message),
			"model":   s.cfg.DreamModeModel(),
		})
	}
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
