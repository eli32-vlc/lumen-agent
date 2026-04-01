package heartbeatstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lumen-agent/internal/config"
)

const maxStoredMessageRunes = 280

type State struct {
	LastProactiveMessageAt time.Time `json:"last_proactive_message_at,omitempty"`
	ProactiveCountToday    int       `json:"proactive_count_today,omitempty"`
	ProactiveCountDate     string    `json:"proactive_count_date,omitempty"`
	LastUserMessageAt      time.Time `json:"last_user_message_at,omitempty"`
	LastTopic              string    `json:"last_topic,omitempty"`
	LastBotMessage         string    `json:"last_bot_message,omitempty"`
	LastBotMessageAt       time.Time `json:"last_bot_message_at,omitempty"`
	NextEarliestNudgeAt    time.Time `json:"next_earliest_nudge_at,omitempty"`
}

func Load(cfg config.Config) (State, error) {
	path := cfg.HeartbeatStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read heartbeat state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode heartbeat state: %w", err)
	}

	return state, nil
}

func Save(cfg config.Config, state State) error {
	path := cfg.HeartbeatStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create heartbeat state dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode heartbeat state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write heartbeat state: %w", err)
	}
	return nil
}

func ApplyUserMessage(state State, content string, now time.Time) State {
	state.LastUserMessageAt = now.UTC()
	if topic := compactText(content); topic != "" {
		state.LastTopic = topic
	}
	return state
}

func ApplyBotMessage(state State, content string, now time.Time, proactive bool, cooldown time.Duration) State {
	utcNow := now.UTC()
	state.LastBotMessageAt = utcNow
	if message := compactText(content); message != "" {
		state.LastBotMessage = message
	}

	if proactive {
		dayKey := utcNow.Format("2006-01-02")
		if state.ProactiveCountDate != dayKey {
			state.ProactiveCountDate = dayKey
			state.ProactiveCountToday = 0
		}
		state.LastProactiveMessageAt = utcNow
		state.ProactiveCountToday++
		if cooldown > 0 {
			state.NextEarliestNudgeAt = utcNow.Add(cooldown)
		}
	}

	return state
}

func PromptLines(state State) []string {
	lines := []string{
		"Heartbeat state file: present",
		"Heartbeat last proactive message at: " + formatTime(state.LastProactiveMessageAt),
		"Heartbeat proactive count today: " + formatCount(state.ProactiveCountToday, state.ProactiveCountDate),
		"Heartbeat last user message at: " + formatTime(state.LastUserMessageAt),
		"Heartbeat last topic: " + fallback(state.LastTopic),
		"Heartbeat last bot message at: " + formatTime(state.LastBotMessageAt),
		"Heartbeat last bot message: " + fallback(state.LastBotMessage),
		"Heartbeat next earliest nudge at: " + formatTime(state.NextEarliestNudgeAt),
	}
	return lines
}

func compactText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxStoredMessageRunes {
		return strings.TrimSpace(string(runes[:maxStoredMessageRunes])) + "..."
	}
	return value
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "none"
	}
	local := value.In(time.Local).Format("2006-01-02 15:04 MST")
	return local + " / " + value.UTC().Format(time.RFC3339)
}

func formatCount(count int, day string) string {
	if count <= 0 {
		return "0"
	}
	day = strings.TrimSpace(day)
	if day == "" {
		return fmt.Sprintf("%d", count)
	}
	return fmt.Sprintf("%d (date=%s UTC)", count, day)
}

func fallback(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
}
