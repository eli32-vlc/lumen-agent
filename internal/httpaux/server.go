package httpaux

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"element-orion/internal/auditlog"
	"element-orion/internal/config"
	"element-orion/internal/dashboard"
	"element-orion/internal/eventwebhook"
)

func CanShareListener(cfg config.Config) bool {
	if !cfg.EventWebhook.Enabled || !cfg.Dashboard.Enabled {
		return false
	}

	return strings.EqualFold(
		strings.TrimSpace(cfg.EventWebhook.ListenAddr),
		strings.TrimSpace(cfg.Dashboard.ListenAddr),
	)
}

func Run(ctx context.Context, cfg config.Config, audit *auditlog.Logger) error {
	if !CanShareListener(cfg) {
		return nil
	}

	webhookHandler, err := eventwebhook.Handler(cfg, audit)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", dashboard.Handler(cfg))
	mux.Handle(cfg.EventWebhook.Path, webhookHandler)

	listenAddr := strings.TrimSpace(cfg.Dashboard.ListenAddr)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf(
			"httpaux: listening on %s (dashboard=%s, webhook=%s)",
			listenAddr,
			cfg.Dashboard.Path,
			cfg.EventWebhook.Path,
		)
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("shared http listen: %w", err)
			return
		}
		errCh <- nil
	}()

	if audit != nil {
		audit.Write("dashboard_listening", "", map[string]any{
			"addr": listenAddr,
			"path": cfg.Dashboard.Path,
			"mode": "shared",
		})
		audit.Write("event_webhook_listening", "", map[string]any{
			"addr": listenAddr,
			"path": cfg.EventWebhook.Path,
			"mode": "shared",
		})
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("shutdown shared http server: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}
