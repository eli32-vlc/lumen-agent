package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type reminderItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type reminderStore struct {
	Items []reminderItem `json:"items"`
}

func (r *Registry) registerReminderTool() {
	r.register(
		"reminders",
		"Manage temporary reminder notes saved by the app runtime. Supports add, list, read, and delete actions.",
		objectSchema(map[string]any{
			"action":  stringSchema("Action to perform: add, list, read, or delete."),
			"id":      stringSchema("Reminder ID for read or delete."),
			"title":   stringSchema("Optional short title for add."),
			"content": stringSchema("Reminder content for add."),
		}, "action"),
		r.handleReminders,
	)
}

func (r *Registry) handleReminders(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Action  string `json:"action"`
		ID      string `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return "", fmt.Errorf("action is required")
	}

	release, err := r.ensureLockManager().Acquire(ctx, "reminders")
	if err != nil {
		return "", err
	}
	defer release()

	storePath, err := r.remindersPath()
	if err != nil {
		return "", err
	}
	store, err := loadReminderStore(storePath)
	if err != nil {
		return "", err
	}

	switch action {
	case "add":
		content := strings.TrimSpace(input.Content)
		if content == "" {
			return "", fmt.Errorf("content is required for add")
		}

		item := reminderItem{
			ID:        reminderID(),
			Title:     strings.TrimSpace(input.Title),
			Content:   content,
			CreatedAt: time.Now().UTC(),
		}
		store.Items = append([]reminderItem{item}, store.Items...)
		if err := saveReminderStore(storePath, store); err != nil {
			return "", err
		}
		return jsonResult(map[string]any{
			"action":   action,
			"reminder": item,
			"count":    len(store.Items),
		})
	case "list":
		return jsonResult(map[string]any{
			"action":    action,
			"reminders": store.Items,
			"count":     len(store.Items),
		})
	case "read":
		item, err := findReminder(store, input.ID)
		if err != nil {
			return "", err
		}
		return jsonResult(map[string]any{
			"action":   action,
			"reminder": item,
		})
	case "delete":
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return "", fmt.Errorf("id is required for delete")
		}

		index := slices.IndexFunc(store.Items, func(item reminderItem) bool {
			return item.ID == id
		})
		if index < 0 {
			return "", fmt.Errorf("reminder %q not found", id)
		}

		removed := store.Items[index]
		store.Items = append(store.Items[:index], store.Items[index+1:]...)
		if err := saveReminderStore(storePath, store); err != nil {
			return "", err
		}
		return jsonResult(map[string]any{
			"action":   action,
			"reminder": removed,
			"count":    len(store.Items),
		})
	default:
		return "", fmt.Errorf("unsupported action %q; use add, list, read, or delete", input.Action)
	}
}

func (r *Registry) remindersPath() (string, error) {
	sessionDir := strings.TrimSpace(r.cfg.App.SessionDir)
	if sessionDir == "" {
		return "", fmt.Errorf("app session_dir is not configured")
	}
	return filepath.Join(sessionDir, "reminders.json"), nil
}

func loadReminderStore(path string) (reminderStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return reminderStore{}, nil
		}
		return reminderStore{}, fmt.Errorf("read reminder store: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return reminderStore{}, nil
	}

	var store reminderStore
	if err := json.Unmarshal(data, &store); err != nil {
		return reminderStore{}, fmt.Errorf("decode reminder store: %w", err)
	}
	return store, nil
}

func saveReminderStore(path string, store reminderStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create reminder store dir: %w", err)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode reminder store: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write reminder store: %w", err)
	}
	return nil
}

func findReminder(store reminderStore, id string) (reminderItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return reminderItem{}, fmt.Errorf("id is required for read")
	}
	for _, item := range store.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return reminderItem{}, fmt.Errorf("reminder %q not found", id)
}

func reminderID() string {
	var bytes [6]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("rem-%d", time.Now().UTC().UnixNano())
	}
	return "rem-" + hex.EncodeToString(bytes[:])
}
