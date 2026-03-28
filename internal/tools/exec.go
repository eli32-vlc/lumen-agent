package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

func (r *Registry) registerExecTool() {
	r.register(
		"exec_command",
		"Run a shell command inside the workspace and capture its output.",
		objectSchema(map[string]any{
			"command":         stringSchema("Shell command to execute."),
			"working_dir":     stringSchema("Optional working directory inside the workspace."),
			"timeout_seconds": integerSchema("Optional timeout override in seconds.", 1),
		}, "command"),
		r.handleExecCommand,
	)
}

func (r *Registry) handleExecCommand(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Command        string `json:"command"`
		WorkingDir     string `json:"working_dir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	if strings.TrimSpace(input.Command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	if !r.allowedCommand(input.Command) {
		return "", fmt.Errorf("command %q is not allowed by tools.allowed_commands", commandHead(input.Command))
	}
	if blocked := r.blockedCommandReference(input.Command); blocked != "" {
		return "", fmt.Errorf("command access to %s is locked because it may contain secrets", blocked)
	}

	workingDir := r.root
	if strings.TrimSpace(input.WorkingDir) != "" {
		resolved, err := r.resolvePath(input.WorkingDir)
		if err != nil {
			return "", err
		}
		workingDir = resolved
	}

	timeout := r.cfg.ExecTimeout()
	if input.TimeoutSeconds > 0 {
		timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}

	if sandbox, ok := SandboxExecutionContextFromContext(ctx); ok && strings.TrimSpace(sandbox.Name) != "" {
		if r.sandboxes == nil {
			return "", fmt.Errorf("sandbox %q is active but no sandbox manager is configured", sandbox.Name)
		}
		result, err := r.sandboxes.ExecInSandbox(ctx, SandboxExecOptions{
			Name:       strings.TrimSpace(sandbox.Name),
			Command:    input.Command,
			WorkingDir: workingDir,
			Timeout:    timeout,
		})
		if err != nil {
			return "", err
		}
		return jsonResult(map[string]any{
			"command":           result.Command,
			"command_head":      commandHead(result.Command),
			"working_dir":       filepath.ToSlash(r.relPath(workingDir)),
			"exit_code":         result.ExitCode,
			"duration_ms":       result.DurationMS,
			"timed_out":         result.TimedOut,
			"truncated":         false,
			"output_line_count": outputLineCount(result.Output),
			"last_output_line":  lastNonEmptyLine(result.Output),
			"output":            result.Output,
			"sandbox":           sandbox.Name,
		})
	}

	timedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(timedCtx, r.cfg.Tools.ExecShell, "-lc", input.Command)
	cmd.Dir = workingDir

	buffer := newLimitedBuffer(r.cfg.Tools.MaxCommandOutputBytes)
	cmd.Stdout = buffer
	cmd.Stderr = buffer

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	timedOut := errors.Is(timedCtx.Err(), context.DeadlineExceeded)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if timedOut {
			exitCode = -1
		} else {
			return "", fmt.Errorf("run command: %w", err)
		}
	}

	return jsonResult(map[string]any{
		"command":           input.Command,
		"command_head":      commandHead(input.Command),
		"working_dir":       filepath.ToSlash(r.relPath(workingDir)),
		"exit_code":         exitCode,
		"duration_ms":       duration.Milliseconds(),
		"timed_out":         timedOut,
		"truncated":         buffer.Truncated(),
		"output_line_count": outputLineCount(buffer.String()),
		"last_output_line":  lastNonEmptyLine(buffer.String()),
		"output":            buffer.String(),
	})
}

func (r *Registry) allowedCommand(command string) bool {
	if len(r.cfg.Tools.AllowedCommands) == 0 {
		return true
	}

	head := commandHead(command)
	for _, allowed := range r.cfg.Tools.AllowedCommands {
		if head == allowed {
			return true
		}
	}

	return false
}

func commandHead(command string) string {
	tokens := splitCommandTokens(command)
	for _, token := range tokens {
		if strings.Contains(token, "=") && !strings.HasPrefix(token, "./") && !strings.HasPrefix(token, "/") {
			continue
		}
		return token
	}
	return ""
}

func splitCommandTokens(input string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range input {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case unicode.IsSpace(r) && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()

	return tokens
}

type limitedBuffer struct {
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	if limit <= 0 {
		limit = 64 << 10
	}
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.truncated {
		return len(p), nil
	}

	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}

	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}

	return b.buffer.Write(p)
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

func outputLineCount(output string) int {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return 0
	}
	return strings.Count(output, "\n") + 1
}

func lastNonEmptyLine(output string) string {
	lines := strings.Split(output, "\n")
	for idx := len(lines) - 1; idx >= 0; idx-- {
		line := strings.TrimSpace(lines[idx])
		if line != "" {
			return line
		}
	}
	return ""
}

func (r *Registry) blockedCommandReference(command string) string {
	protected := r.protectedConfigPath()
	if protected == "" {
		return ""
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	candidates := []string{
		protected,
		filepath.ToSlash(protected),
		filepath.Base(protected),
	}

	if rel, err := filepath.Rel(r.root, protected); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		candidates = append(candidates, rel, filepath.ToSlash(rel))
	}

	lowerCommand := strings.ToLower(command)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.Contains(lowerCommand, strings.ToLower(candidate)) {
			return candidate
		}
	}

	return ""
}
