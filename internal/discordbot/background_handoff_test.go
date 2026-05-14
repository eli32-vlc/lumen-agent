package discordbot

import (
	"testing"

	"element-orion/internal/config"
)

func TestEnqueueBackgroundTaskUpdateAddsToBatch(t *testing.T) {
	service := &Service{
		cfg: configForSharedGuildTests(),
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

	// Check that the notification was added to the batch
	service.batchMu.Lock()
	batch, exists := service.backgroundNotificationBatches["channel-1"]
	service.batchMu.Unlock()

	if !exists {
		t.Fatal("expected background notification to be added to batch")
	}

	if len(batch.notifications) != 1 {
		t.Fatalf("expected 1 notification in batch, got %d", len(batch.notifications))
	}

	notification := batch.notifications[0]
	if notification.task == nil {
		t.Fatal("expected task to be set")
	}
	if notification.task.ID != "task-1" {
		t.Fatalf("expected task ID 'task-1', got %q", notification.task.ID)
	}
	if notification.task.GuildID != "guild-1" {
		t.Fatalf("expected GuildID 'guild-1', got %q", notification.task.GuildID)
	}
	if notification.outcome != "finished" {
		t.Fatalf("expected outcome 'finished', got %q", notification.outcome)
	}
	if notification.reply != "all done" {
		t.Fatalf("expected reply 'all done', got %q", notification.reply)
	}
	if notification.err != nil {
		t.Fatalf("expected no error, got %v", notification.err)
	}
}

func configForSharedGuildTests() config.Config {
	return config.Config{
		Discord: config.DiscordConfig{
			GuildSessionScope: "channel",
		},
	}
}
