package heartbeatstate

import (
	"strings"
	"testing"
	"time"
)

func TestApplyUserMessageTracksTopicAndTime(t *testing.T) {
	now := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	state := ApplyUserMessage(State{}, "check in about the launch plan", now)

	if got := state.LastUserMessageAt; !got.Equal(now.UTC()) {
		t.Fatalf("expected last user message time %v, got %v", now.UTC(), got)
	}
	if state.LastTopic != "check in about the launch plan" {
		t.Fatalf("unexpected last topic %q", state.LastTopic)
	}
}

func TestApplyBotMessageTracksProactiveCooldown(t *testing.T) {
	now := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)
	state := ApplyBotMessage(State{}, "morning. what's the one thing we need to finish?", now, true, 3*time.Hour)

	if got := state.LastBotMessageAt; !got.Equal(now.UTC()) {
		t.Fatalf("expected last bot message time %v, got %v", now.UTC(), got)
	}
	if got := state.LastProactiveMessageAt; !got.Equal(now.UTC()) {
		t.Fatalf("expected last proactive message time %v, got %v", now.UTC(), got)
	}
	if state.ProactiveCountToday != 1 {
		t.Fatalf("expected proactive count 1, got %d", state.ProactiveCountToday)
	}
	if state.ProactiveCountDate != "2026-04-01" {
		t.Fatalf("unexpected proactive count date %q", state.ProactiveCountDate)
	}
	if got := state.NextEarliestNudgeAt; !got.Equal(now.UTC().Add(3 * time.Hour)) {
		t.Fatalf("expected next earliest nudge %v, got %v", now.UTC().Add(3*time.Hour), got)
	}
}

func TestApplyBotMessageResetsDailyCounter(t *testing.T) {
	now := time.Date(2026, 4, 2, 1, 0, 0, 0, time.UTC)
	state := State{ProactiveCountToday: 4, ProactiveCountDate: "2026-04-01"}
	state = ApplyBotMessage(state, "new day nudge", now, true, time.Hour)

	if state.ProactiveCountToday != 1 {
		t.Fatalf("expected proactive count to reset to 1, got %d", state.ProactiveCountToday)
	}
}

func TestPromptLinesIncludeTrackedFields(t *testing.T) {
	lines := PromptLines(State{
		LastTopic:           "draft the investor update",
		LastBotMessage:      "still thinking about the investor note",
		ProactiveCountToday: 2,
		ProactiveCountDate:  "2026-04-01",
	})

	joined := strings.Join(lines, "\n")
	for _, snippet := range []string{
		"Heartbeat state file: present",
		"Heartbeat proactive count today: 2 (date=2026-04-01 UTC)",
		"Heartbeat last topic: draft the investor update",
		"Heartbeat last bot message: still thinking about the investor note",
	} {
		if !strings.Contains(joined, snippet) {
			t.Fatalf("expected prompt lines to contain %q", snippet)
		}
	}
}
