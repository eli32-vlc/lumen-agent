package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen-agent/internal/config"
)

func TestHandleWriteFileReturnsPreview(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 4096,
			},
		},
	}

	result, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"line1\nline2","overwrite":true}`))
	if err != nil {
		t.Fatalf("handleWriteFile returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["content_preview"] != "line1\nline2" {
		t.Fatalf("unexpected content preview %#v", parsed["content_preview"])
	}
	if parsed["line_count"] != float64(2) {
		t.Fatalf("unexpected line count %#v", parsed["line_count"])
	}
}

func TestHandleDeletePathReturnsParentPreview(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 4096,
			},
		},
	}

	dirResult, err := registry.handleMkdir(context.Background(), json.RawMessage(`{"path":"work"}`))
	if err != nil {
		t.Fatalf("handleMkdir returned error: %v", err)
	}
	if !strings.Contains(dirResult, `"parent_entries_preview"`) {
		t.Fatalf("expected mkdir result to include parent preview, got %s", dirResult)
	}

	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"work/keep.txt","content":"keep","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile keep.txt returned error: %v", err)
	}
	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"work/drop.txt","content":"drop","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile drop.txt returned error: %v", err)
	}

	result, err := registry.handleDeletePath(context.Background(), json.RawMessage(`{"path":"work/drop.txt"}`))
	if err != nil {
		t.Fatalf("handleDeletePath returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["deleted_type"] != "file" {
		t.Fatalf("unexpected deleted type %#v", parsed["deleted_type"])
	}
	if parsed["parent_path"] != filepath.ToSlash("work") {
		t.Fatalf("unexpected parent path %#v", parsed["parent_path"])
	}

	entries, ok := parsed["parent_entries_preview"].([]any)
	if !ok {
		t.Fatalf("expected parent_entries_preview array, got %#v", parsed["parent_entries_preview"])
	}
	if len(entries) != 1 || entries[0] != "keep.txt" {
		t.Fatalf("unexpected parent entries %#v", entries)
	}
}

func TestHandleReadFileReturnsLineStats(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 4096,
			},
		},
	}

	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"line1\nline2\nline3","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile returned error: %v", err)
	}

	result, err := registry.handleReadFile(context.Background(), json.RawMessage(`{"path":"notes.txt","start_line":2,"end_line":2}`))
	if err != nil {
		t.Fatalf("handleReadFile returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["total_lines"] != float64(3) {
		t.Fatalf("unexpected total_lines %#v", parsed["total_lines"])
	}
	if parsed["truncated"] != true {
		t.Fatalf("expected truncated=true, got %#v", parsed["truncated"])
	}
	if parsed["returned_end_line"] != float64(2) {
		t.Fatalf("unexpected returned_end_line %#v", parsed["returned_end_line"])
	}
}

func TestHandleReadFileRespectsMaxBytes(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 32,
			},
		},
	}

	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"alpha\nbeta\ngamma\n","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile returned error: %v", err)
	}

	result, err := registry.handleReadFile(context.Background(), json.RawMessage(`{"path":"notes.txt","max_bytes":10}`))
	if err != nil {
		t.Fatalf("handleReadFile returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["bytes_returned"] != float64(10) {
		t.Fatalf("unexpected bytes_returned %#v", parsed["bytes_returned"])
	}
	if parsed["applied_max_bytes"] != float64(10) {
		t.Fatalf("unexpected applied_max_bytes %#v", parsed["applied_max_bytes"])
	}
	if parsed["truncated_by_max"] != true {
		t.Fatalf("expected truncated_by_max=true, got %#v", parsed["truncated_by_max"])
	}
	if parsed["next_start_line"] != float64(2) {
		t.Fatalf("unexpected next_start_line %#v", parsed["next_start_line"])
	}
}

func TestHandleReadFileClampsRequestedMaxBytesToRuntimeLimit(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 12,
			},
		},
	}

	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"0123456789abcdef\n","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile returned error: %v", err)
	}

	result, err := registry.handleReadFile(context.Background(), json.RawMessage(`{"path":"notes.txt","max_bytes":1000}`))
	if err != nil {
		t.Fatalf("handleReadFile returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["applied_max_bytes"] != float64(12) {
		t.Fatalf("expected applied_max_bytes to clamp to 12, got %#v", parsed["applied_max_bytes"])
	}
	if parsed["partial_last_line"] != true {
		t.Fatalf("expected partial_last_line=true, got %#v", parsed["partial_last_line"])
	}
	if parsed["next_start_line"] != float64(1) {
		t.Fatalf("expected next_start_line to stay on the partial line, got %#v", parsed["next_start_line"])
	}
}

func TestHandleListDirReturnsCounts(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 4096,
			},
		},
	}

	if _, err := registry.handleMkdir(context.Background(), json.RawMessage(`{"path":"nested"}`)); err != nil {
		t.Fatalf("handleMkdir returned error: %v", err)
	}
	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"root.txt","content":"hello","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile returned error: %v", err)
	}

	result, err := registry.handleListDir(context.Background(), json.RawMessage(`{"path":".","recursive":false,"include_hidden":true}`))
	if err != nil {
		t.Fatalf("handleListDir returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["entry_count"] != float64(2) {
		t.Fatalf("unexpected entry_count %#v", parsed["entry_count"])
	}
	if parsed["dir_count"] != float64(1) {
		t.Fatalf("unexpected dir_count %#v", parsed["dir_count"])
	}
	if parsed["file_count"] != float64(1) {
		t.Fatalf("unexpected file_count %#v", parsed["file_count"])
	}
}

func TestHandleGrepSearchReturnsMatchCountAndPathType(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{
		root: root,
		cfg: config.Config{
			App: config.AppConfig{WorkspaceRoot: root},
			Tools: config.ToolsConfig{
				MaxFileBytes:          1 << 20,
				MaxSearchResults:      20,
				MaxCommandOutputBytes: 4096,
			},
		},
	}

	if _, err := registry.handleWriteFile(context.Background(), json.RawMessage(`{"path":"a.txt","content":"alpha\nbeta\nalpha","overwrite":true}`)); err != nil {
		t.Fatalf("handleWriteFile returned error: %v", err)
	}

	result, err := registry.handleGrepSearch(context.Background(), json.RawMessage(`{"pattern":"alpha","path":".","max_results":10,"case_sensitive":true}`))
	if err != nil {
		t.Fatalf("handleGrepSearch returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["searched_path_type"] != "dir" {
		t.Fatalf("unexpected searched_path_type %#v", parsed["searched_path_type"])
	}
	if parsed["match_count"] != float64(2) {
		t.Fatalf("unexpected match_count %#v", parsed["match_count"])
	}
}

func TestHandleReadFileRejectsProtectedConfigFile(t *testing.T) {
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
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}
	cfg.SetSourcePath(protected)

	registry := &Registry{root: root, cfg: cfg}

	_, err := registry.handleReadFile(context.Background(), json.RawMessage(`{"path":"config/lumen.yaml"}`))
	if err == nil {
		t.Fatal("expected protected config file read to be blocked")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleGrepSearchSkipsProtectedConfigFile(t *testing.T) {
	root := t.TempDir()
	protected := filepath.Join(root, "config", "lumen.yaml")
	if err := os.MkdirAll(filepath.Dir(protected), 0o755); err != nil {
		t.Fatalf("mkdir protected dir: %v", err)
	}
	if err := os.WriteFile(protected, []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("write protected file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("write notes file: %v", err)
	}

	cfg := config.Config{
		App: config.AppConfig{WorkspaceRoot: root},
		Tools: config.ToolsConfig{
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}
	cfg.SetSourcePath(protected)

	registry := &Registry{root: root, cfg: cfg}

	result, err := registry.handleGrepSearch(context.Background(), json.RawMessage(`{"pattern":"needle","path":".","max_results":10,"case_sensitive":true}`))
	if err != nil {
		t.Fatalf("handleGrepSearch returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if parsed["match_count"] != float64(1) {
		t.Fatalf("expected only non-protected match, got %#v", parsed["match_count"])
	}

	matches, ok := parsed["matches"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("unexpected matches %#v", parsed["matches"])
	}

	match, ok := matches[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected match payload %#v", matches[0])
	}
	if match["path"] != "notes.txt" {
		t.Fatalf("expected only notes.txt match, got %#v", match["path"])
	}
}

func TestHandleDeletePathRejectsParentDirectoryContainingProtectedConfig(t *testing.T) {
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
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}
	cfg.SetSourcePath(protected)

	registry := &Registry{root: root, cfg: cfg}

	_, err := registry.handleDeletePath(context.Background(), json.RawMessage(`{"path":"config","recursive":true}`))
	if err == nil {
		t.Fatal("expected protected config parent delete to be blocked")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Fatalf("unexpected error: %v", err)
	}
}
