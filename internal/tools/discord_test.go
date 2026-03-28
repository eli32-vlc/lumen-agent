package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen-agent/internal/config"
)

func TestHandleSendDiscordFileUsesSessionChannelContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", req.Method)
		}
		if req.URL.Path != "/channels/channel-123/messages" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		if auth := req.Header.Get("Authorization"); auth != "Bot test-token" {
			t.Fatalf("unexpected authorization header %q", auth)
		}

		if err := req.ParseMultipartForm(2 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}

		payloadJSON := req.FormValue("payload_json")
		if payloadJSON == "" {
			t.Fatal("payload_json should be present")
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			t.Fatalf("decode payload_json: %v", err)
		}
		if payload["content"] != "file attached" {
			t.Fatalf("unexpected content %v", payload["content"])
		}

		files := req.MultipartForm.File["files[0]"]
		if len(files) != 1 {
			t.Fatalf("expected one file upload, got %d", len(files))
		}

		uploaded, err := files[0].Open()
		if err != nil {
			t.Fatalf("open uploaded file: %v", err)
		}
		defer uploaded.Close()

		data, err := io.ReadAll(uploaded)
		if err != nil {
			t.Fatalf("read uploaded file: %v", err)
		}
		if string(data) != "hello" {
			t.Fatalf("unexpected uploaded contents %q", string(data))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-1","channel_id":"channel-123","attachments":[{"id":"att-1","filename":"note.txt","size":5,"url":"https://cdn.example.test/file"}]}`))
	}))
	defer server.Close()

	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes: 1024,
			},
			Discord: config.DiscordConfig{BotToken: "test-token"},
		},
		discordAPIBase: server.URL,
		discordClient:  server.Client(),
	}

	ctx := WithDiscordToolContext(context.Background(), DiscordToolContext{ChannelID: "channel-123"})
	result, err := registry.handleSendDiscordFile(ctx, json.RawMessage(`{"path":"note.txt","message":"file attached"}`))
	if err != nil {
		t.Fatalf("handleSendDiscordFile returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode result JSON: %v", err)
	}
	if decoded["channel_id"] != "channel-123" {
		t.Fatalf("unexpected channel_id %v", decoded["channel_id"])
	}
	if decoded["message_id"] != "msg-1" {
		t.Fatalf("unexpected message_id %v", decoded["message_id"])
	}
}

func TestHandleSendDiscordFileRequiresChannelWhenNoContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes: 1024,
			},
			Discord: config.DiscordConfig{BotToken: "test-token"},
		},
	}

	_, err := registry.handleSendDiscordFile(context.Background(), json.RawMessage(`{"path":"note.txt"}`))
	if err == nil {
		t.Fatal("expected error when no channel_id or session channel context is available")
	}
	if !strings.Contains(err.Error(), "channel_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleSendDiscordMessageUsesSessionChannelContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", req.Method)
		}
		if req.URL.Path != "/channels/channel-123/messages" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request JSON: %v", err)
		}
		if payload["content"] != "hello discord" {
			t.Fatalf("unexpected content %v", payload["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-9","channel_id":"channel-123"}`))
	}))
	defer server.Close()

	registry := &Registry{
		cfg: config.Config{
			Discord: config.DiscordConfig{BotToken: "test-token"},
		},
		discordAPIBase: server.URL,
		discordClient:  server.Client(),
	}

	ctx := WithDiscordToolContext(context.Background(), DiscordToolContext{ChannelID: "channel-123"})
	result, err := registry.handleSendDiscordMessage(ctx, json.RawMessage(`{"content":"hello discord"}`))
	if err != nil {
		t.Fatalf("handleSendDiscordMessage returned error: %v", err)
	}

	if !strings.Contains(result, `"message_id": "msg-9"`) {
		t.Fatalf("expected Discord message id in result, got %s", result)
	}
}

func TestHandleSendDiscordMessageOpensAllowlistedDM(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		switch req.URL.Path {
		case "/users/@me/channels":
			if req.Method != http.MethodPost {
				t.Fatalf("unexpected method %s for DM open", req.Method)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read DM request body: %v", err)
			}
			if !strings.Contains(string(body), `"recipient_id":"jack"`) {
				t.Fatalf("unexpected DM open body %s", string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"dm-123"}`))
		case "/channels/dm-123/messages":
			if req.Method != http.MethodPost {
				t.Fatalf("unexpected method %s for DM message", req.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg-10","channel_id":"dm-123"}`))
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
	}))
	defer server.Close()

	registry := &Registry{
		cfg: config.Config{
			Discord: config.DiscordConfig{
				BotToken:            "test-token",
				AllowDirectMessages: true,
				AllowedDMUserIDs:    []string{"jack"},
			},
		},
		discordAPIBase: server.URL,
		discordClient:  server.Client(),
	}

	result, err := registry.handleSendDiscordMessage(context.Background(), json.RawMessage(`{"content":"hello jack","user_id":"jack"}`))
	if err != nil {
		t.Fatalf("handleSendDiscordMessage returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected two Discord requests, got %d", requests)
	}
	if !strings.Contains(result, `"channel_id": "dm-123"`) {
		t.Fatalf("expected DM channel id in result, got %s", result)
	}
}

func TestHandleAddDiscordReactionUsesCurrentMessageContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPut {
			t.Fatalf("unexpected method %s", req.Method)
		}
		if req.URL.Path != "/channels/channel-123/messages/message-123/reactions/👍/@me" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	registry := &Registry{
		cfg: config.Config{
			Discord: config.DiscordConfig{BotToken: "test-token"},
		},
		discordAPIBase: server.URL,
		discordClient:  server.Client(),
	}

	ctx := WithDiscordToolContext(context.Background(), DiscordToolContext{
		ChannelID: "channel-123",
		MessageID: "message-123",
	})
	result, err := registry.handleAddDiscordReaction(ctx, json.RawMessage(`{"emoji":"👍"}`))
	if err != nil {
		t.Fatalf("handleAddDiscordReaction returned error: %v", err)
	}
	if !strings.Contains(result, `"message_id": "message-123"`) {
		t.Fatalf("expected message id in result, got %s", result)
	}
}

func TestAuthorizeDiscordRESTPathAllowsCurrentGuildAndChannel(t *testing.T) {
	registry := &Registry{
		cfg: config.Config{
			Discord: config.DiscordConfig{
				AllowedGuildIDs:           []string{"guild-1"},
				AllowedOutboundChannelIDs: []string{"channel-2"},
			},
		},
	}

	ctx := WithDiscordToolContext(context.Background(), DiscordToolContext{
		GuildID:   "guild-1",
		ChannelID: "channel-1",
	})

	if err := registry.authorizeDiscordRESTPath(ctx, "/guilds/guild-1/bans/user-1", nil); err != nil {
		t.Fatalf("expected guild path to be allowed, got %v", err)
	}
	if err := registry.authorizeDiscordRESTPath(ctx, "/channels/channel-2/messages", nil); err != nil {
		t.Fatalf("expected allowlisted channel path to be allowed, got %v", err)
	}
}
