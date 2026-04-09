package discordbot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"element-orion/internal/agent"
	"element-orion/internal/config"
	"element-orion/internal/llm"
)

func TestPrepareRunHistoryIsolatesHeartbeatSessions(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Heartbeat: config.HeartbeatConfig{IsolatedSession: true},
		},
	}
	history := []llm.Message{{Role: "user", Content: "remember this"}}

	prepared, previousLen, persist := service.prepareRunHistory(inboundPrompt{Kind: promptKindHeartbeat}, history)
	if prepared != nil {
		t.Fatalf("expected isolated heartbeat history to be cleared, got %#v", prepared)
	}
	if previousLen != 0 {
		t.Fatalf("expected previous history length 0, got %d", previousLen)
	}
	if persist {
		t.Fatal("expected isolated heartbeat history to skip session persistence")
	}
}

func TestSessionKeyUsesSharedGuildScope(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{GuildSessionScope: "channel"},
		},
	}

	keyA := service.sessionKey("guild-1", "channel-1", "user-a")
	keyB := service.sessionKey("guild-1", "channel-1", "user-b")

	if keyA.String() != keyB.String() {
		t.Fatalf("expected shared guild scope to collapse users into one session key, got %q vs %q", keyA.String(), keyB.String())
	}
}

func TestAuthorizeContextUsesDMAllowlist(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{
				AllowDirectMessages: true,
				AllowedDMUserIDs:    []string{"jack"},
			},
		},
	}

	if ok, _ := service.authorizeContext("", "jack"); !ok {
		t.Fatal("expected allowlisted DM user to be authorized")
	}
	if ok, reason := service.authorizeContext("", "someone-else"); ok {
		t.Fatal("expected unlisted DM user to be rejected")
	} else if !strings.Contains(reason, "allowed users") {
		t.Fatalf("unexpected DM rejection reason %q", reason)
	}
}

func TestUserPromptFromMessageFormatsSharedChannelMetadata(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{GuildSessionScope: "channel"},
		},
		application: "bot-1",
	}

	message := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:      "message-1",
		GuildID: "guild-1",
		Content: "hello there",
		Author:  &discordgo.User{ID: "user-1", Username: "jack", GlobalName: "Jack"},
		Member:  &discordgo.Member{Nick: "jack2spiece"},
		Mentions: []*discordgo.User{
			{ID: "bot-1"},
		},
	}}

	prompt := service.userPromptFromMessage(message)

	for _, snippet := range []string{
		"Shared channel message",
		"speaker: jack2spiece",
		"user_id: user-1",
		"mentioned_you: yes",
		"content:\nhello there",
	} {
		if !strings.Contains(prompt.Content, snippet) {
			t.Fatalf("expected prompt to contain %q, got %q", snippet, prompt.Content)
		}
	}
}

func TestUserPromptFromMessageDownloadsAttachmentsAndRewritesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello attachment")
	}))
	defer server.Close()

	attachmentsDir := filepath.Join(t.TempDir(), "incoming")
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{
				DownloadIncomingAttachments: true,
				IncomingAttachmentsDir:      attachmentsDir,
			},
		},
	}

	message := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-1",
		ChannelID: "channel-1",
		Content:   "please inspect " + server.URL + "/note.txt",
		Author:    &discordgo.User{ID: "user-1", Username: "jack"},
		Attachments: []*discordgo.MessageAttachment{{
			ID:       "att-1",
			Filename: "note.txt",
			URL:      server.URL + "/note.txt",
		}},
	}}

	prompt := service.userPromptFromMessage(message)
	wantPath := filepath.Join(attachmentsDir, "channel-1", "message-1", "note.txt")
	if !strings.Contains(prompt.Content, wantPath) {
		t.Fatalf("expected prompt content to reference downloaded attachment path %q, got %q", wantPath, prompt.Content)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read downloaded attachment: %v", err)
	}
	if string(data) != "hello attachment" {
		t.Fatalf("unexpected downloaded attachment contents %q", string(data))
	}
}

func TestTurnAssistantReplySupportsNoReplyToken(t *testing.T) {
	history := []llm.Message{
		{Role: "assistant", Content: "older reply"},
		{Role: "user", Content: "new message"},
		{Role: "assistant", Content: agent.NoReplyToken},
	}

	reply, silent := turnAssistantReply(history, 1)
	if !silent {
		t.Fatal("expected no-reply token to suppress the Discord response")
	}
	if reply != "" {
		t.Fatalf("expected empty reply for no-reply token, got %q", reply)
	}
}

func TestTurnAssistantReplyStripsThinkBlocks(t *testing.T) {
	history := []llm.Message{
		{Role: "assistant", Content: "<think>private</think> hello there"},
	}

	reply, silent := turnAssistantReply(history, 0)
	if silent {
		t.Fatal("expected normal reply, not silence")
	}
	if reply != "hello there" {
		t.Fatalf("expected think block to be stripped, got %q", reply)
	}
}

func TestGuildMemoryShardPathUsesSessionDir(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), ".element-orion")
	guildMemoryRoot := filepath.Join(sessionDir, "guild-memory", "guild-1", "channel-1")

	if err := agent.AppendToMemoryShard(guildMemoryRoot, "what did we talk about today", "we talked about memory", time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AppendToMemoryShard returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(guildMemoryRoot, "2026-03-12-PM.md"))
	if err != nil {
		t.Fatalf("read guild memory shard: %v", err)
	}

	content := string(data)
	for _, snippet := range []string{
		"what did we talk about today",
		"we talked about memory",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected guild shard to contain %q, got %q", snippet, content)
		}
	}
}
