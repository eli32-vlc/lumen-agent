package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"lumen-agent/internal/config"
	"lumen-agent/internal/llm"
)

type Handler func(context.Context, json.RawMessage) (string, error)

type Tool struct {
	Definition llm.ToolDefinition
	Handler    Handler
}

type Registry struct {
	root             string
	cfg              config.Config
	tools            map[string]Tool
	names            []string
	close            []func() error
	discordAPIBase   string
	discordClient    *http.Client
	gifAPIBase       string
	gifClient        *http.Client
	rssClient        *http.Client
	backgroundTasks  BackgroundTaskManager
	scheduledWakeups ScheduledWakeupManager
	sandboxes        SandboxManager
	locks            *resourceLockManager
}

func NewRegistry(cfg config.Config) (*Registry, error) {
	registry := &Registry{
		root:           cfg.App.WorkspaceRoot,
		cfg:            cfg,
		tools:          map[string]Tool{},
		names:          []string{},
		discordAPIBase: "https://discord.com/api/v10",
		discordClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		gifAPIBase: "https://api.giphy.com/v1/gifs",
		gifClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rssClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		locks: newResourceLockManager(),
	}

	registry.registerFilesystemTools()
	registry.registerExecTool()
	registry.registerDiscordTool()
	registry.registerContextCompactionTool()
	registry.registerBackgroundTaskTools()
	registry.registerHeartbeatWakeTools()
	registry.registerSandboxTools()
	registry.registerGIFTool()
	registry.registerWebInfoTools()
	registry.registerRSSTools()
	registry.registerReminderTool()
	if err := registry.registerMCPTools(context.Background()); err != nil {
		_ = registry.Close()
		return nil, err
	}

	if len(registry.tools) == 0 {
		return nil, fmt.Errorf("no tools are enabled in the current config")
	}

	slices.Sort(registry.names)
	return registry, nil
}

func (r *Registry) Close() error {
	var errs []string
	for i := len(r.close) - 1; i >= 0; i-- {
		if err := r.close[i](); err != nil {
			errs = append(errs, err.Error())
		}
	}
	r.close = nil
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("close registry resources: %s", strings.Join(errs, "; "))
}

func (r *Registry) Definitions() []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, 0, len(r.names))
	for _, name := range r.names {
		definitions = append(definitions, r.tools[name].Definition)
	}
	return definitions
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}

	names := make([]string, len(r.names))
	copy(names, r.names)
	return names
}

func (r *Registry) Execute(ctx context.Context, call llm.ToolCall) (string, error) {
	tool, ok := r.tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", call.Function.Name)
	}

	payload := strings.TrimSpace(call.Function.Arguments)
	if payload == "" {
		payload = "{}"
	}

	return tool.Handler(ctx, json.RawMessage(payload))
}

func (r *Registry) ensureLockManager() *resourceLockManager {
	if r.locks == nil {
		r.locks = newResourceLockManager()
	}
	return r.locks
}

func (r *Registry) register(name string, description string, parameters map[string]any, handler Handler) {
	if !r.cfg.ToolEnabled(name) {
		return
	}

	r.tools[name] = Tool{
		Definition: llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  normalizeSchema(parameters),
			},
		},
		Handler: handler,
	}
	r.names = append(r.names, name)
}

func (r *Registry) registerAlways(name string, description string, parameters map[string]any, handler Handler) error {
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}

	r.tools[name] = Tool{
		Definition: llm.ToolDefinition{
			Type: "function",
			Function: llm.ToolFunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  normalizeSchema(parameters),
			},
		},
		Handler: handler,
	}
	r.names = append(r.names, name)
	return nil
}

func (r *Registry) resolvePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.root, candidate)
	}

	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}

	rel, err := filepath.Rel(r.root, absPath)
	if err != nil {
		return "", fmt.Errorf("verify path %q: %w", path, err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace root", path)
	}

	return absPath, nil
}

func (r *Registry) protectedConfigPath() string {
	sourcePath := strings.TrimSpace(r.cfg.SourcePath())
	if sourcePath == "" {
		return ""
	}

	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return filepath.Clean(sourcePath)
	}
	return filepath.Clean(absPath)
}

func (r *Registry) isProtectedPath(path string) bool {
	protected := r.protectedConfigPath()
	if protected == "" {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path) == protected
	}

	return filepath.Clean(absPath) == protected
}

func (r *Registry) ensurePathAccessible(path string) error {
	if !r.isProtectedPath(path) {
		return nil
	}

	return fmt.Errorf("access to %s is locked because it may contain secrets", r.describePath(path))
}

func (r *Registry) ensurePathMutationAllowed(path string) error {
	if !r.pathTouchesProtectedPath(path) {
		return nil
	}

	return fmt.Errorf("access to %s is locked because it may contain secrets", r.describePath(path))
}

func (r *Registry) pathTouchesProtectedPath(path string) bool {
	protected := r.protectedConfigPath()
	if protected == "" {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = filepath.Clean(path)
	}
	absPath = filepath.Clean(absPath)
	if absPath == protected {
		return true
	}

	rel, err := filepath.Rel(absPath, protected)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (r *Registry) describePath(path string) string {
	if rel, err := filepath.Rel(r.root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}

	protected := r.protectedConfigPath()
	if filepath.Clean(path) == protected {
		return filepath.Base(protected)
	}

	return filepath.Base(path)
}

func (r *Registry) relPath(path string) string {
	rel, err := filepath.Rel(r.root, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func decodeArgs(payload json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func jsonResult(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode tool result: %w", err)
	}
	return string(data), nil
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	requiredFields := make([]string, 0, len(required))
	requiredFields = append(requiredFields, required...)

	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             requiredFields,
		"additionalProperties": false,
	}
}

func normalizeSchema(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}

	normalized := make(map[string]any, len(value))
	for key, item := range value {
		switch typed := item.(type) {
		case map[string]any:
			normalized[key] = normalizeSchema(typed)
		case []map[string]any:
			items := make([]map[string]any, len(typed))
			for i, entry := range typed {
				items[i] = normalizeSchema(entry)
			}
			normalized[key] = items
		case []string:
			items := make([]string, len(typed))
			copy(items, typed)
			normalized[key] = items
		case []any:
			items := make([]any, len(typed))
			for i, entry := range typed {
				if nested, ok := entry.(map[string]any); ok {
					items[i] = normalizeSchema(nested)
					continue
				}
				items[i] = entry
			}
			normalized[key] = items
		case nil:
			if key == "required" {
				normalized[key] = []string{}
				continue
			}
			normalized[key] = nil
		default:
			normalized[key] = item
		}
	}

	return normalized
}

func stringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func integerSchema(description string, minimum int) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
		"minimum":     minimum,
	}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}
