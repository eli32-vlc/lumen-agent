package discordbot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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

func onePixelPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
	return buf.Bytes()
}

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

func TestIsOwnMessageUsesConnectedIdentity(t *testing.T) {
	service := &Service{
		application: "self-user",
	}

	if !service.isOwnMessage(&discordgo.MessageCreate{Message: &discordgo.Message{
		Author: &discordgo.User{ID: "self-user"},
	}}) {
		t.Fatal("expected connected account message to be treated as our own")
	}

	if service.isOwnMessage(&discordgo.MessageCreate{Message: &discordgo.Message{
		Author: &discordgo.User{ID: "someone-else"},
	}}) {
		t.Fatal("expected another user's message to not be treated as our own")
	}
}

func TestShouldReplyToPromptInUserModeEvenWhenConfigFlagIsOff(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{
				TokenMode:      "user",
				ReplyToMessage: false,
			},
		},
	}

	if !service.shouldReplyToPrompt(inboundPrompt{MessageID: "msg-1"}) {
		t.Fatal("expected user mode replies to reference the triggering message")
	}
}

func TestShouldReplyToPromptRespectsBotModeFlag(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{
				TokenMode:      "bot",
				ReplyToMessage: false,
			},
		},
	}

	if service.shouldReplyToPrompt(inboundPrompt{MessageID: "msg-1"}) {
		t.Fatal("expected bot mode to keep reply references disabled when config flag is off")
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

func TestSessionKeyUsesSharedGroupDMScope(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{AllowGroupDirectMessages: true},
		},
		channelTypes: map[string]discordgo.ChannelType{
			"channel-1": discordgo.ChannelTypeGroupDM,
		},
	}

	keyA := service.sessionKey("", "channel-1", "user-a")
	keyB := service.sessionKey("", "channel-1", "user-b")

	if keyA.String() != keyB.String() {
		t.Fatalf("expected shared group DM scope to collapse users into one session key, got %q vs %q", keyA.String(), keyB.String())
	}
}

func TestAuthorizeMessageContextAllowsGroupDMWhenEnabled(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{AllowGroupDirectMessages: true},
		},
		channelTypes: map[string]discordgo.ChannelType{
			"channel-1": discordgo.ChannelTypeGroupDM,
		},
	}

	message := &discordgo.MessageCreate{Message: &discordgo.Message{
		ChannelID: "channel-1",
		Author:    &discordgo.User{ID: "user-1"},
	}}

	if ok, reason := service.authorizeMessageContext(message); !ok {
		t.Fatalf("expected group DM message to be authorized, got reason %q", reason)
	}
}

func TestUserPromptFromMessageFormatsSharedGroupDMMetadata(t *testing.T) {
	service := &Service{
		cfg: config.Config{
			Discord: config.DiscordConfig{AllowGroupDirectMessages: true},
		},
		application: "bot-1",
		channelTypes: map[string]discordgo.ChannelType{
			"channel-1": discordgo.ChannelTypeGroupDM,
		},
	}

	message := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-1",
		ChannelID: "channel-1",
		Content:   "hey from the group",
		Author:    &discordgo.User{ID: "user-1", Username: "jack", GlobalName: "Jack"},
	}}

	prompt := service.userPromptFromMessage(message)
	for _, snippet := range []string{
		"Shared channel message",
		"speaker: Jack",
		"user_id: user-1",
		"content:\nhey from the group",
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

func TestUserPromptFromMessageWithVisionEnabledDownloadsAndBuildsImageParts(t *testing.T) {
	pngBytes := onePixelPNGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()

	attachmentsDir := filepath.Join(t.TempDir(), "incoming")
	service := &Service{
		cfg: config.Config{
			LLM: config.LLMConfig{
				VisionEnabled: true,
			},
			Discord: config.DiscordConfig{
				DownloadIncomingAttachments: false,
				IncomingAttachmentsDir:      attachmentsDir,
			},
		},
	}

	message := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-vision",
		ChannelID: "channel-1",
		Content:   "what is in " + server.URL + "/image.png",
		Author:    &discordgo.User{ID: "user-1", Username: "jack"},
		Attachments: []*discordgo.MessageAttachment{{
			ID:          "att-vision",
			Filename:    "image.png",
			URL:         server.URL + "/image.png",
			ContentType: "image/png",
		}},
	}}

	prompt := service.userPromptFromMessage(message)
	wantPath := filepath.Join(attachmentsDir, "channel-1", "message-vision", "image.png")
	if strings.Contains(prompt.Content, server.URL+"/image.png") {
		t.Fatalf("expected prompt content to replace the remote image URL, got %q", prompt.Content)
	}
	if !strings.Contains(prompt.Content, wantPath) {
		t.Fatalf("expected prompt content to reference downloaded image path %q, got %q", wantPath, prompt.Content)
	}
	if len(prompt.UserParts) != 2 {
		t.Fatalf("expected text + image multimodal parts, got %#v", prompt.UserParts)
	}
	if prompt.UserParts[1].Type != llm.ContentPartImageURL {
		t.Fatalf("expected second part to be image_url, got %#v", prompt.UserParts[1])
	}
	if !strings.HasPrefix(prompt.UserParts[1].ImageURL, "data:image/jpeg;base64,") {
		t.Fatalf("expected image URL part to be jpeg data URL, got %#v", prompt.UserParts[1])
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected downloaded image at %q: %v", wantPath, err)
	}
}

func TestUserPromptFromMessageWithoutVisionDownloadsImageButDoesNotBuildParts(t *testing.T) {
	pngBytes := onePixelPNGBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()

	attachmentsDir := filepath.Join(t.TempDir(), "incoming")
	service := &Service{
		cfg: config.Config{
			LLM: config.LLMConfig{
				VisionEnabled: false,
			},
			Discord: config.DiscordConfig{
				DownloadIncomingAttachments: false,
				IncomingAttachmentsDir:      attachmentsDir,
			},
		},
	}

	message := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-no-vision",
		ChannelID: "channel-1",
		Content:   "check " + server.URL + "/image.png with tools",
		Author:    &discordgo.User{ID: "user-1", Username: "jack"},
		Attachments: []*discordgo.MessageAttachment{{
			ID:          "att-no-vision",
			Filename:    "image.png",
			URL:         server.URL + "/image.png",
			ContentType: "image/png",
		}},
	}}

	prompt := service.userPromptFromMessage(message)
	wantPath := filepath.Join(attachmentsDir, "channel-1", "message-no-vision", "image.png")
	if !strings.Contains(prompt.Content, wantPath) {
		t.Fatalf("expected prompt content to reference downloaded image path %q, got %q", wantPath, prompt.Content)
	}
	if len(prompt.UserParts) != 0 {
		t.Fatalf("expected no multimodal parts when vision is disabled, got %#v", prompt.UserParts)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected downloaded image at %q: %v", wantPath, err)
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

func TestClassifySilentTurnDetectsNoReplyToken(t *testing.T) {
	history := []llm.Message{
		{Role: "assistant", Content: "older reply"},
		{Role: "user", Content: "new message"},
		{Role: "assistant", Content: agent.NoReplyToken},
	}

	reason := classifySilentTurn(history, 1, "", true)
	if reason != "no_reply_token" {
		t.Fatalf("expected no_reply_token, got %q", reason)
	}
}

func TestClassifySilentTurnDetectsEmptyReply(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "new message"},
		{Role: "assistant", Content: ""},
	}

	reason := classifySilentTurn(history, 0, "", false)
	if reason != "empty_reply" {
		t.Fatalf("expected empty_reply, got %q", reason)
	}
}

func TestClassifySilentTurnDetectsMissingAssistantMessage(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "new message"},
		{Role: "tool", Content: "done"},
	}

	reason := classifySilentTurn(history, 0, "", false)
	if reason != "no_assistant_message" {
		t.Fatalf("expected no_assistant_message, got %q", reason)
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
