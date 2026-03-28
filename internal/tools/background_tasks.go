package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"lumen-agent/internal/llm"
)

type backgroundTaskContextKey struct{}
type backgroundTaskRuntimeContextKey struct{}

type BackgroundTaskRuntimeContext struct {
	History     []llm.Message
	RequestedAt time.Time
}

type BackgroundTaskEvent struct {
	Kind       string    `json:"kind"`
	Message    string    `json:"message,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	FullDetail string    `json:"full_detail,omitempty"`
	Time       time.Time `json:"time"`
}

type BackgroundTaskSandboxInfo struct {
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	State        string    `json:"state,omitempty"`
	RootfsDir    string    `json:"rootfs_dir,omitempty"`
	WorkspaceDir string    `json:"workspace_dir,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
}

type BackgroundTaskInfo struct {
	ID                string                     `json:"id"`
	Status            string                     `json:"status"`
	Prompt            string                     `json:"prompt"`
	Result            string                     `json:"result,omitempty"`
	Error             string                     `json:"error,omitempty"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	StartedAt         time.Time                  `json:"started_at,omitempty"`
	CompletedAt       time.Time                  `json:"completed_at,omitempty"`
	MinRuntimeSeconds int64                      `json:"min_runtime_seconds,omitempty"`
	Sandbox           *BackgroundTaskSandboxInfo `json:"sandbox,omitempty"`
	Events            []BackgroundTaskEvent      `json:"events,omitempty"`
}

type BackgroundTaskStartOptions struct {
	Prompt        string
	ModelOverride string
	LightContext  bool
	History       []llm.Message
	RequestedAt   time.Time
	MinRuntime    time.Duration
	Sandbox       bool
}

type BackgroundTaskManager interface {
	StartBackgroundTask(context.Context, BackgroundTaskStartOptions) (BackgroundTaskInfo, error)
	ListBackgroundTasks(context.Context, string, int) ([]BackgroundTaskInfo, error)
	GetBackgroundTask(context.Context, string) (BackgroundTaskInfo, error)
	GetBackgroundTaskLogs(context.Context, string, int) ([]BackgroundTaskEvent, error)
	CancelBackgroundTask(context.Context, string) (BackgroundTaskInfo, error)
}

func WithBackgroundTaskContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, backgroundTaskContextKey{}, true)
}

func IsBackgroundTaskContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(backgroundTaskContextKey{}).(bool)
	return active
}

func WithBackgroundTaskRuntimeContext(ctx context.Context, runtime BackgroundTaskRuntimeContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime.History = append([]llm.Message(nil), runtime.History...)
	return context.WithValue(ctx, backgroundTaskRuntimeContextKey{}, runtime)
}

func BackgroundTaskRuntimeContextFromContext(ctx context.Context) (BackgroundTaskRuntimeContext, bool) {
	if ctx == nil {
		return BackgroundTaskRuntimeContext{}, false
	}
	runtime, ok := ctx.Value(backgroundTaskRuntimeContextKey{}).(BackgroundTaskRuntimeContext)
	if !ok {
		return BackgroundTaskRuntimeContext{}, false
	}
	runtime.History = append([]llm.Message(nil), runtime.History...)
	return runtime, true
}

func (r *Registry) SetBackgroundTaskManager(manager BackgroundTaskManager) {
	r.backgroundTasks = manager
}

func (r *Registry) registerBackgroundTaskTools() {
	r.register(
		"start_background_task",
		"Start a background sub-agent task that runs without typing indicators and posts a notification in Discord when finished.",
		objectSchema(map[string]any{
			"prompt":         stringSchema("Task prompt for the background sub-agent."),
			"model_override": stringSchema("Optional model override for the background sub-agent."),
			"light_context":  booleanSchema("Whether to use a lighter context snapshot."),
			"min_runtime":    stringSchema("Optional minimum runtime duration like 30s or 5m. The task should keep working until it reaches this runtime or is genuinely blocked."),
			"sandbox":        booleanSchema("Whether to run the background sub-agent's shell commands inside a fresh sandboxed container when sandboxing is enabled."),
		}, "prompt"),
		r.handleStartBackgroundTask,
	)

	r.register(
		"list_background_tasks",
		"List background sub-agent tasks visible from the current Discord context.",
		objectSchema(map[string]any{
			"status": stringSchema("Optional status filter: queued, running, completed, failed, or canceled."),
			"limit":  integerSchema("Optional max number of tasks to return.", 1),
		}),
		r.handleListBackgroundTasks,
	)

	r.register(
		"get_background_task",
		"Get the status and latest result for one background sub-agent task.",
		objectSchema(map[string]any{
			"id":             stringSchema("Background task ID."),
			"include_events": booleanSchema("Whether to include recent background-task events and tool output summaries."),
			"event_limit":    integerSchema("Optional max number of recent events to include when include_events is true.", 1),
		}, "id"),
		r.handleGetBackgroundTask,
	)

	r.register(
		"get_background_task_logs",
		"Get detailed background sub-agent event logs, including tool-call output, for one task.",
		objectSchema(map[string]any{
			"id":    stringSchema("Background task ID."),
			"limit": integerSchema("Optional max number of recent events to include.", 1),
		}, "id"),
		r.handleGetBackgroundTaskLogs,
	)

	r.register(
		"cancel_background_task",
		"Cancel a running background sub-agent task.",
		objectSchema(map[string]any{
			"id": stringSchema("Background task ID."),
		}, "id"),
		r.handleCancelBackgroundTask,
	)
}

func (r *Registry) handleStartBackgroundTask(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Prompt        string `json:"prompt"`
		ModelOverride string `json:"model_override"`
		LightContext  bool   `json:"light_context"`
		MinRuntime    string `json:"min_runtime"`
		Sandbox       bool   `json:"sandbox"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.backgroundTasks == nil {
		return "", fmt.Errorf("background task manager is not available")
	}
	if IsBackgroundTaskContext(ctx) {
		return "", fmt.Errorf("nested background tasks are not allowed; continue the work in the current background task instead")
	}

	runtime, _ := BackgroundTaskRuntimeContextFromContext(ctx)
	minRuntime := time.Duration(0)
	if value := strings.TrimSpace(input.MinRuntime); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return "", fmt.Errorf("parse min_runtime: %w", err)
		}
		minRuntime = parsed
	}

	info, err := r.backgroundTasks.StartBackgroundTask(ctx, BackgroundTaskStartOptions{
		Prompt:        strings.TrimSpace(input.Prompt),
		ModelOverride: strings.TrimSpace(input.ModelOverride),
		LightContext:  input.LightContext,
		History:       runtime.History,
		RequestedAt:   runtime.RequestedAt,
		MinRuntime:    minRuntime,
		Sandbox:       input.Sandbox,
	})
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}

func (r *Registry) handleListBackgroundTasks(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.backgroundTasks == nil {
		return "", fmt.Errorf("background task manager is not available")
	}

	items, err := r.backgroundTasks.ListBackgroundTasks(ctx, strings.TrimSpace(strings.ToLower(input.Status)), input.Limit)
	if err != nil {
		return "", err
	}
	return jsonResult(map[string]any{"tasks": items})
}

func (r *Registry) handleGetBackgroundTask(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		ID            string `json:"id"`
		IncludeEvents bool   `json:"include_events"`
		EventLimit    int    `json:"event_limit"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.backgroundTasks == nil {
		return "", fmt.Errorf("background task manager is not available")
	}

	info, err := r.backgroundTasks.GetBackgroundTask(ctx, strings.TrimSpace(input.ID))
	if err != nil {
		return "", err
	}
	if input.IncludeEvents {
		events, err := r.backgroundTasks.GetBackgroundTaskLogs(ctx, strings.TrimSpace(input.ID), input.EventLimit)
		if err != nil {
			return "", err
		}
		info.Events = events
	}
	return jsonResult(info)
}

func (r *Registry) handleGetBackgroundTaskLogs(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.backgroundTasks == nil {
		return "", fmt.Errorf("background task manager is not available")
	}

	events, err := r.backgroundTasks.GetBackgroundTaskLogs(ctx, strings.TrimSpace(input.ID), input.Limit)
	if err != nil {
		return "", err
	}
	return jsonResult(map[string]any{
		"id":     strings.TrimSpace(input.ID),
		"events": events,
	})
}

func (r *Registry) handleCancelBackgroundTask(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		ID string `json:"id"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	if r.backgroundTasks == nil {
		return "", fmt.Errorf("background task manager is not available")
	}

	info, err := r.backgroundTasks.CancelBackgroundTask(ctx, strings.TrimSpace(input.ID))
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}
