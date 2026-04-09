package httpaux

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"element-orion/internal/config"
	"element-orion/internal/dashboard"
	"element-orion/internal/eventwebhook"
)

func TestCanShareListenerRequiresMatchingEnabledAddresses(t *testing.T) {
	cfg := config.Config{
		EventWebhook: config.EventWebhookConfig{
			Enabled:    true,
			ListenAddr: "127.0.0.1:8788",
		},
		Dashboard: config.DashboardConfig{
			Enabled:    true,
			ListenAddr: "127.0.0.1:8788",
		},
	}

	if !CanShareListener(cfg) {
		t.Fatal("expected matching enabled listeners to be shareable")
	}

	cfg.Dashboard.ListenAddr = "127.0.0.1:8789"
	if CanShareListener(cfg) {
		t.Fatal("expected different listener addresses to stay separate")
	}

	cfg.Dashboard.Enabled = false
	if CanShareListener(cfg) {
		t.Fatal("expected disabled dashboard to prevent listener sharing")
	}
}

func TestSharedMuxServesWebhookAndDashboard(t *testing.T) {
	cfg := testConfig(t)

	mux := http.NewServeMux()
	webhookHandler, err := eventwebhook.Handler(cfg, nil)
	if err != nil {
		t.Fatalf("eventwebhook.Handler returned error: %v", err)
	}
	mux.Handle("/", dashboard.Handler(cfg))
	mux.Handle(cfg.EventWebhook.Path, webhookHandler)

	webhookReq := httptest.NewRequest(http.MethodPost, cfg.EventWebhook.Path, http.NoBody)
	query := webhookReq.URL.Query()
	query.Set("text", "build failed")
	webhookReq.URL.RawQuery = query.Encode()
	webhookResp := httptest.NewRecorder()
	mux.ServeHTTP(webhookResp, webhookReq)

	if webhookResp.Code != http.StatusAccepted {
		t.Fatalf("expected webhook status %d, got %d", http.StatusAccepted, webhookResp.Code)
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, cfg.Dashboard.Path+"/api/state", nil)
	dashboardResp := httptest.NewRecorder()
	mux.ServeHTTP(dashboardResp, dashboardReq)

	if dashboardResp.Code != http.StatusOK {
		t.Fatalf("expected dashboard status %d, got %d", http.StatusOK, dashboardResp.Code)
	}

	var payload struct {
		Logs []any `json:"logs"`
	}
	if err := json.Unmarshal(dashboardResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()

	root := t.TempDir()
	sessionDir := filepath.Join(root, ".element-orion")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}

	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot: root,
			SessionDir:    sessionDir,
			MemoryDir:     filepath.Join(sessionDir, "memory"),
		},
		Heartbeat: config.HeartbeatConfig{
			Every: "30m",
			Target: config.HeartbeatTargetConfig{
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
		EventWebhook: config.EventWebhookConfig{
			Enabled:     true,
			ListenAddr:  "127.0.0.1:8788",
			Path:        "/event",
			DefaultMode: "now",
		},
		Dashboard: config.DashboardConfig{
			Enabled:    true,
			ListenAddr: "127.0.0.1:8788",
			Path:       "/dashboard",
		},
	}

	return cfg
}
