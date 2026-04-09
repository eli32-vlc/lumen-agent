package discordbot

import (
	"context"
	"strings"
	"testing"

	"element-orion/internal/config"
)

func TestEnqueueBackgroundTaskUpdateQueuesInternalPrompt(t *testing.T) {
	key := sessionKey{GuildID: "guild-1", ChannelID: "channel-1", UserID: ""}
	session := &sessionState{
		Key:     key,
		Queue:   make(chan inboundPrompt, 1),
		Context: context.Background(),
	}
	service := &Service{
		cfg: configForSharedGuildTests(),
		sessions: map[string]*sessionState{
			key.String(): session,
		},
	}

	task := &backgroundTask{
		ID:            "task-1",
		Prompt:        "analyze repo",
		GuildID:       "guild-1",
		ChannelID:     "channel-1",
		UserID:        "user-1",
		SpawnMessages: 6,
		SpawnTokens:   554,
	}

	if err := service.enqueueBackgroundTaskUpdate(task, "finished", "all done", nil); err != nil {
		t.Fatalf("enqueueBackgroundTaskUpdate returned error: %v", err)
	}

	select {
	case prompt := <-session.Queue:
		if prompt.Kind != promptKindBackground {
			t.Fatalf("expected background prompt kind, got %q", prompt.Kind)
		}
		for _, snippet := range []string{
			"Internal system event for the dom agent.",
			"Background worker status: finished",
			"Worker final reply:",
			"all done",
		} {
			if !strings.Contains(prompt.Content, snippet) {
				t.Fatalf("expected prompt to contain %q, got:\n%s", snippet, prompt.Content)
			}
		}
	default:
		t.Fatal("expected background handoff prompt to be queued")
	}
}

func configForSharedGuildTests() config.Config {
	return config.Config{
		Discord: config.DiscordConfig{
			GuildSessionScope: "channel",
		},
	}
}
