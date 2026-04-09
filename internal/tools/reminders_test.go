package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"element-orion/internal/config"
)

func TestReminderToolLifecycle(t *testing.T) {
	cfg := config.Config{
		App: config.AppConfig{
			SessionDir: t.TempDir(),
		},
	}

	registry, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	addPayload, err := json.Marshal(map[string]any{
		"action":  "add",
		"title":   "Follow up",
		"content": "Check token usage tomorrow",
	})
	if err != nil {
		t.Fatalf("marshal add payload: %v", err)
	}

	addResult, err := registry.handleReminders(context.Background(), addPayload)
	if err != nil {
		t.Fatalf("handleReminders add returned error: %v", err)
	}
	if !strings.Contains(addResult, "Follow up") {
		t.Fatalf("expected add result to include title, got %q", addResult)
	}

	store, err := loadReminderStore(filepath.Join(cfg.App.SessionDir, "reminders.json"))
	if err != nil {
		t.Fatalf("loadReminderStore returned error: %v", err)
	}
	if len(store.Items) != 1 {
		t.Fatalf("expected one reminder, got %d", len(store.Items))
	}

	listPayload, err := json.Marshal(map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("marshal list payload: %v", err)
	}
	listResult, err := registry.handleReminders(context.Background(), listPayload)
	if err != nil {
		t.Fatalf("handleReminders list returned error: %v", err)
	}
	if !strings.Contains(listResult, "Check token usage tomorrow") {
		t.Fatalf("expected list result to include content, got %q", listResult)
	}

	readPayload, err := json.Marshal(map[string]any{"action": "read", "id": store.Items[0].ID})
	if err != nil {
		t.Fatalf("marshal read payload: %v", err)
	}
	readResult, err := registry.handleReminders(context.Background(), readPayload)
	if err != nil {
		t.Fatalf("handleReminders read returned error: %v", err)
	}
	if !strings.Contains(readResult, store.Items[0].ID) {
		t.Fatalf("expected read result to include reminder id, got %q", readResult)
	}

	deletePayload, err := json.Marshal(map[string]any{"action": "delete", "id": store.Items[0].ID})
	if err != nil {
		t.Fatalf("marshal delete payload: %v", err)
	}
	deleteResult, err := registry.handleReminders(context.Background(), deletePayload)
	if err != nil {
		t.Fatalf("handleReminders delete returned error: %v", err)
	}
	if !strings.Contains(deleteResult, "delete") {
		t.Fatalf("expected delete result to mention action, got %q", deleteResult)
	}
}

func TestReminderReadRequiresID(t *testing.T) {
	registry := &Registry{
		cfg: config.Config{
			App: config.AppConfig{
				SessionDir: t.TempDir(),
			},
		},
		locks: newResourceLockManager(),
	}

	payload := json.RawMessage(`{"action":"read"}`)
	_, err := registry.handleReminders(context.Background(), payload)
	if err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected id required error, got %v", err)
	}
}
