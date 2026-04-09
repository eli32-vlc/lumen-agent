package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"element-orion/internal/config"
)

func TestHandleExecCommandReturnsAutonomyMetadata(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				ExecShell:             "/bin/zsh",
				ExecTimeout:           "2s",
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 4096,
			},
		},
	}

	result, err := registry.handleExecCommand(context.Background(), json.RawMessage(`{"command":"printf 'alpha\nbeta\n'","timeout_seconds":1}`))
	if err != nil {
		t.Fatalf("handleExecCommand returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["command_head"] != "printf" {
		t.Fatalf("unexpected command_head %#v", parsed["command_head"])
	}
	if parsed["output_line_count"] != float64(2) {
		t.Fatalf("unexpected output_line_count %#v", parsed["output_line_count"])
	}
	if parsed["last_output_line"] != "beta" {
		t.Fatalf("unexpected last_output_line %#v", parsed["last_output_line"])
	}
}

func TestHandleExecCommandRejectsProtectedConfigReference(t *testing.T) {
	root := t.TempDir()
	protected := filepath.Join(root, "config", "lumen.yaml")
	if err := os.MkdirAll(filepath.Dir(protected), 0o755); err != nil {
		t.Fatalf("mkdir protected dir: %v", err)
	}
	if err := os.WriteFile(protected, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write protected file: %v", err)
	}

	cfg := config.Config{
		App: config.AppConfig{WorkspaceRoot: root},
		Tools: config.ToolsConfig{
			ExecShell:             "/bin/zsh",
			ExecTimeout:           "2s",
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}
	cfg.SetSourcePath(protected)

	registry := &Registry{root: root, cfg: cfg}

	_, err := registry.handleExecCommand(context.Background(), json.RawMessage(`{"command":"cat config/lumen.yaml","timeout_seconds":1}`))
	if err == nil {
		t.Fatal("expected protected config command to be blocked")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Fatalf("unexpected error: %v", err)
	}
}
