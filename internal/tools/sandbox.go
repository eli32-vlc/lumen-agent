package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type sandboxExecutionContextKey struct{}

type SandboxExecutionContext struct {
	Name string
}

type SandboxCreateOptions struct {
	Name         string
	Release      string
	Architecture string
	AutoStart    bool
}

type SandboxExecOptions struct {
	Name       string
	Command    string
	WorkingDir string
	Timeout    time.Duration
}

type SandboxExecResult struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	Output     string `json:"output"`
}

type SandboxManager interface {
	CreateSandbox(context.Context, SandboxCreateOptions) (BackgroundTaskSandboxInfo, error)
	StartSandbox(context.Context, string) (BackgroundTaskSandboxInfo, error)
	StopSandbox(context.Context, string, bool) (BackgroundTaskSandboxInfo, error)
	DeleteSandbox(context.Context, string, bool) (BackgroundTaskSandboxInfo, error)
	InspectSandbox(context.Context, string) (BackgroundTaskSandboxInfo, error)
	ListSandboxes(context.Context) ([]BackgroundTaskSandboxInfo, error)
	ExecInSandbox(context.Context, SandboxExecOptions) (SandboxExecResult, error)
}

func WithSandboxExecutionContext(ctx context.Context, sandbox SandboxExecutionContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sandboxExecutionContextKey{}, sandbox)
}

func SandboxExecutionContextFromContext(ctx context.Context) (SandboxExecutionContext, bool) {
	if ctx == nil {
		return SandboxExecutionContext{}, false
	}
	sandbox, ok := ctx.Value(sandboxExecutionContextKey{}).(SandboxExecutionContext)
	return sandbox, ok
}

func (r *Registry) SetSandboxManager(manager SandboxManager) {
	r.sandboxes = manager
}

func (r *Registry) registerSandboxTools() {
	r.register(
		"list_sandbox_containers",
		"List available sandbox containers managed by the runtime.",
		objectSchema(map[string]any{}),
		r.handleListSandboxContainers,
	)

	r.register(
		"inspect_sandbox_container",
		"Inspect one sandbox container by name.",
		objectSchema(map[string]any{
			"name": stringSchema("Sandbox container name."),
		}, "name"),
		r.handleInspectSandboxContainer,
	)

	r.register(
		"create_sandbox_container",
		"Create a Debian sandbox container rootfs, optionally starting it immediately.",
		objectSchema(map[string]any{
			"name":         stringSchema("Optional sandbox container name."),
			"release":      stringSchema("Optional Debian release override, for example stable or bookworm."),
			"architecture": stringSchema("Optional Debian architecture override, for example amd64."),
			"auto_start":   booleanSchema("Whether to start the sandbox immediately after creating it."),
		}),
		r.handleCreateSandboxContainer,
	)

	r.register(
		"start_sandbox_container",
		"Start an existing sandbox container by name.",
		objectSchema(map[string]any{
			"name": stringSchema("Sandbox container name."),
		}, "name"),
		r.handleStartSandboxContainer,
	)

	r.register(
		"stop_sandbox_container",
		"Stop a running sandbox container by name.",
		objectSchema(map[string]any{
			"name":  stringSchema("Sandbox container name."),
			"force": booleanSchema("Whether to force-stop the sandbox if a graceful terminate fails."),
		}, "name"),
		r.handleStopSandboxContainer,
	)

	r.register(
		"delete_sandbox_container",
		"Delete a sandbox container and its rootfs directory.",
		objectSchema(map[string]any{
			"name":  stringSchema("Sandbox container name."),
			"force": booleanSchema("Whether to force-stop the sandbox before deletion."),
		}, "name"),
		r.handleDeleteSandboxContainer,
	)
}

func (r *Registry) handleListSandboxContainers(ctx context.Context, _ json.RawMessage) (string, error) {
	if r.sandboxes == nil {
		return "", fmt.Errorf("sandbox manager is not available")
	}
	items, err := r.sandboxes.ListSandboxes(ctx)
	if err != nil {
		return "", err
	}
	return jsonResult(map[string]any{"sandboxes": items})
}

func (r *Registry) handleInspectSandboxContainer(ctx context.Context, payload json.RawMessage) (string, error) {
	if r.sandboxes == nil {
		return "", fmt.Errorf("sandbox manager is not available")
	}
	type args struct {
		Name string `json:"name"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	info, err := r.sandboxes.InspectSandbox(ctx, strings.TrimSpace(input.Name))
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}

func (r *Registry) handleCreateSandboxContainer(ctx context.Context, payload json.RawMessage) (string, error) {
	if r.sandboxes == nil {
		return "", fmt.Errorf("sandbox manager is not available")
	}
	type args struct {
		Name         string `json:"name"`
		Release      string `json:"release"`
		Architecture string `json:"architecture"`
		AutoStart    bool   `json:"auto_start"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	info, err := r.sandboxes.CreateSandbox(ctx, SandboxCreateOptions{
		Name:         strings.TrimSpace(input.Name),
		Release:      strings.TrimSpace(input.Release),
		Architecture: strings.TrimSpace(input.Architecture),
		AutoStart:    input.AutoStart,
	})
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}

func (r *Registry) handleStartSandboxContainer(ctx context.Context, payload json.RawMessage) (string, error) {
	if r.sandboxes == nil {
		return "", fmt.Errorf("sandbox manager is not available")
	}
	type args struct {
		Name string `json:"name"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	info, err := r.sandboxes.StartSandbox(ctx, strings.TrimSpace(input.Name))
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}

func (r *Registry) handleStopSandboxContainer(ctx context.Context, payload json.RawMessage) (string, error) {
	if r.sandboxes == nil {
		return "", fmt.Errorf("sandbox manager is not available")
	}
	type args struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	info, err := r.sandboxes.StopSandbox(ctx, strings.TrimSpace(input.Name), input.Force)
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}

func (r *Registry) handleDeleteSandboxContainer(ctx context.Context, payload json.RawMessage) (string, error) {
	if r.sandboxes == nil {
		return "", fmt.Errorf("sandbox manager is not available")
	}
	type args struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}
	info, err := r.sandboxes.DeleteSandbox(ctx, strings.TrimSpace(input.Name), input.Force)
	if err != nil {
		return "", err
	}
	return jsonResult(info)
}
