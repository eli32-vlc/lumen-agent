package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeBackgroundTaskManager struct{}

func (fakeBackgroundTaskManager) StartBackgroundTask(_ context.Context, options BackgroundTaskStartOptions) (BackgroundTaskInfo, error) {
	return BackgroundTaskInfo{ID: "task-1", Status: "queued", Prompt: options.Prompt, CreatedAt: time.Now()}, nil
}

func (fakeBackgroundTaskManager) ListBackgroundTasks(_ context.Context, status string, limit int) ([]BackgroundTaskInfo, error) {
	return []BackgroundTaskInfo{{ID: "task-1", Status: status, Prompt: "hello"}}, nil
}

func (fakeBackgroundTaskManager) GetBackgroundTask(_ context.Context, id string) (BackgroundTaskInfo, error) {
	return BackgroundTaskInfo{ID: id, Status: "running", Prompt: "hello"}, nil
}

func (fakeBackgroundTaskManager) GetBackgroundTaskLogs(_ context.Context, id string, limit int) ([]BackgroundTaskEvent, error) {
	return []BackgroundTaskEvent{{Kind: "status", Message: "running", Time: time.Now()}}, nil
}

func (fakeBackgroundTaskManager) CancelBackgroundTask(_ context.Context, id string) (BackgroundTaskInfo, error) {
	return BackgroundTaskInfo{ID: id, Status: "canceled", Prompt: "hello"}, nil
}

func TestHandleStartBackgroundTask(t *testing.T) {
	registry := &Registry{backgroundTasks: fakeBackgroundTaskManager{}}
	result, err := registry.handleStartBackgroundTask(context.Background(), json.RawMessage(`{"prompt":"do work"}`))
	if err != nil {
		t.Fatalf("handleStartBackgroundTask returned error: %v", err)
	}
	if !strings.Contains(result, `"id": "task-1"`) {
		t.Fatalf("expected task id in result, got %s", result)
	}
}

func TestHandleStartBackgroundTaskRejectsNestedBackgroundTask(t *testing.T) {
	registry := &Registry{backgroundTasks: fakeBackgroundTaskManager{}}
	_, err := registry.handleStartBackgroundTask(WithBackgroundTaskContext(context.Background()), json.RawMessage(`{"prompt":"do work"}`))
	if err == nil {
		t.Fatal("expected nested background task to be rejected")
	}
	if !strings.Contains(err.Error(), "nested background tasks are not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleGetBackgroundTaskIncludesEvents(t *testing.T) {
	registry := &Registry{backgroundTasks: fakeBackgroundTaskManager{}}
	result, err := registry.handleGetBackgroundTask(context.Background(), json.RawMessage(`{"id":"task-1","include_events":true}`))
	if err != nil {
		t.Fatalf("handleGetBackgroundTask returned error: %v", err)
	}
	if !strings.Contains(result, `"events"`) {
		t.Fatalf("expected events in result, got %s", result)
	}
}
