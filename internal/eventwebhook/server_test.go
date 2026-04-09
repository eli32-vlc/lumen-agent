package eventwebhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"element-orion/internal/config"
)

type queuedEvent struct {
	Text string `json:"text"`
	Mode string `json:"mode"`
}

func TestHandlerQueuesEventWithDefaultMode(t *testing.T) {
	cfg := testConfig(t)
	handler := newHandler(cfg, "", nil)

	request := httptest.NewRequest(http.MethodPost, cfg.EventWebhook.Path, strings.NewReader(`{"text":"Build failed"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}

	events := readQueuedEvents(t, cfg)
	if len(events) != 1 {
		t.Fatalf("expected 1 queued event, got %d", len(events))
	}
	if events[0].Text != "Build failed" {
		t.Fatalf("expected queued text %q, got %q", "Build failed", events[0].Text)
	}
	if events[0].Mode != modeNow {
		t.Fatalf("expected queued mode %q, got %q", modeNow, events[0].Mode)
	}
}

func TestHandlerRejectsMissingSecret(t *testing.T) {
	cfg := testConfig(t)
	handler := newHandler(cfg, "top-secret", nil)

	request := httptest.NewRequest(http.MethodPost, cfg.EventWebhook.Path, strings.NewReader(`{"text":"Deploy done"}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}

	events := readQueuedEvents(t, cfg)
	if len(events) != 0 {
		t.Fatalf("expected no queued events, got %d", len(events))
	}
}

func TestHandlerAcceptsBearerToken(t *testing.T) {
	cfg := testConfig(t)
	handler := newHandler(cfg, "top-secret", nil)

	request := httptest.NewRequest(http.MethodPost, cfg.EventWebhook.Path, strings.NewReader(`{"text":"Deploy done","mode":"next-heartbeat"}`))
	request.Header.Set("Authorization", "Bearer top-secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}

	events := readQueuedEvents(t, cfg)
	if len(events) != 1 {
		t.Fatalf("expected 1 queued event, got %d", len(events))
	}
	if events[0].Mode != modeNextHeartbeat {
		t.Fatalf("expected queued mode %q, got %q", modeNextHeartbeat, events[0].Mode)
	}
}

func TestHandlerRejectsInvalidMode(t *testing.T) {
	cfg := testConfig(t)
	handler := newHandler(cfg, "", nil)

	request := httptest.NewRequest(http.MethodPost, cfg.EventWebhook.Path, strings.NewReader(`{"text":"Deploy done","mode":"later"}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()

	sessionDir := filepath.Join(t.TempDir(), ".element-orion")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}

	return config.Config{
		App: config.AppConfig{
			SessionDir: sessionDir,
		},
		Heartbeat: config.HeartbeatConfig{
			Every: "30m",
			Target: config.HeartbeatTargetConfig{
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
		EventWebhook: config.EventWebhookConfig{
			Path:        "/event",
			DefaultMode: modeNow,
		},
	}
}

func readQueuedEvents(t *testing.T, cfg config.Config) []queuedEvent {
	t.Helper()

	dir := cfg.HeartbeatEventsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read events dir: %v", err)
	}

	events := make([]queuedEvent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read event file: %v", err)
		}
		var event queuedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("decode event file: %v", err)
		}
		events = append(events, event)
	}

	return events
}
