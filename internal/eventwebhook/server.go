package eventwebhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"lumen-agent/internal/auditlog"
	"lumen-agent/internal/config"
	"lumen-agent/internal/discordbot"
)

const (
	modeNow           = "now"
	modeNextHeartbeat = "next-heartbeat"
	maxBodyBytes      = 64 << 10
)

type requestBody struct {
	Text string `json:"text"`
	Mode string `json:"mode"`
}

type responseBody struct {
	Status string `json:"status"`
	Mode   string `json:"mode,omitempty"`
	Error  string `json:"error,omitempty"`
}

func Run(ctx context.Context, cfg config.Config, audit *auditlog.Logger) error {
	if !cfg.EventWebhook.Enabled {
		return nil
	}

	handler, err := Handler(cfg, audit)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.EventWebhook.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("event webhook listen: %w", err)
			return
		}
		errCh <- nil
	}()

	if audit != nil {
		audit.Write("event_webhook_listening", "", map[string]any{
			"addr": cfg.EventWebhook.ListenAddr,
			"path": cfg.EventWebhook.Path,
		})
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("shutdown event webhook: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

func Handler(cfg config.Config, audit *auditlog.Logger) (http.Handler, error) {
	secret, err := cfg.ResolveEventWebhookSecret()
	if err != nil {
		return nil, fmt.Errorf("resolve event webhook secret: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.EventWebhook.Path, newHandler(cfg, secret, audit))
	return mux, nil
}

func newHandler(cfg config.Config, secret string, audit *auditlog.Logger) http.Handler {
	defaultMode := normalizeMode(cfg.EventWebhook.DefaultMode)
	if defaultMode == "" {
		defaultMode = modeNow
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, responseBody{Status: "error", Error: "method must be POST"})
			return
		}

		if !authorized(r, secret) {
			writeJSON(w, http.StatusUnauthorized, responseBody{Status: "error", Error: "unauthorized"})
			return
		}

		payload, err := decodeBody(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, responseBody{Status: "error", Error: err.Error()})
			return
		}

		text := strings.TrimSpace(payload.Text)
		if text == "" {
			text = strings.TrimSpace(r.URL.Query().Get("text"))
		}
		if text == "" {
			writeJSON(w, http.StatusBadRequest, responseBody{Status: "error", Error: "text must not be empty"})
			return
		}

		mode := normalizeMode(payload.Mode)
		if mode == "" {
			mode = normalizeMode(r.URL.Query().Get("mode"))
		}
		if mode == "" {
			mode = defaultMode
		}
		if mode != modeNow && mode != modeNextHeartbeat {
			writeJSON(w, http.StatusBadRequest, responseBody{Status: "error", Error: "mode must be now or next-heartbeat"})
			return
		}

		if err := discordbot.EnqueueSystemEvent(cfg, text, mode); err != nil {
			writeJSON(w, http.StatusInternalServerError, responseBody{Status: "error", Error: err.Error()})
			return
		}

		if audit != nil {
			audit.Write("event_webhook_enqueued", "", map[string]any{
				"mode": mode,
			})
		}

		writeJSON(w, http.StatusAccepted, responseBody{Status: "queued", Mode: mode})
	})
}

func decodeBody(body io.ReadCloser) (requestBody, error) {
	defer body.Close()

	data, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return requestBody{}, fmt.Errorf("read request body")
	}
	if len(data) > maxBodyBytes {
		return requestBody{}, fmt.Errorf("request body is too large")
	}
	if strings.TrimSpace(string(data)) == "" {
		return requestBody{}, nil
	}

	var payload requestBody
	if err := json.Unmarshal(data, &payload); err != nil {
		return requestBody{}, fmt.Errorf("request body must be valid JSON")
	}
	return payload, nil
}

func normalizeMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func authorized(r *http.Request, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return true
	}

	provided := strings.TrimSpace(r.Header.Get("X-Lumen-Webhook-Secret"))
	if provided == "" {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
			provided = strings.TrimSpace(authorization[7:])
		}
	}
	if provided == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func writeJSON(w http.ResponseWriter, status int, payload responseBody) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
