package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"lumen-agent/internal/config"
	"lumen-agent/internal/tools"
)

type processState struct {
	cmd  *exec.Cmd
	done chan error
}

type Manager struct {
	cfg config.Config

	mu     sync.Mutex
	active map[string]*processState
}

func NewManager(cfg config.Config) *Manager {
	return &Manager{
		cfg:    cfg,
		active: map[string]*processState{},
	}
}

func (m *Manager) CreateSandbox(ctx context.Context, options tools.SandboxCreateOptions) (tools.BackgroundTaskSandboxInfo, error) {
	if err := m.requirePrivilegeForSandbox("debootstrap"); err != nil {
		return tools.BackgroundTaskSandboxInfo{}, err
	}

	name := sanitizeSandboxName(options.Name)
	if name == "" {
		name = sanitizeSandboxName("sandbox-" + time.Now().UTC().Format("20060102-150405"))
	}

	info := m.baseInfo(name)
	if err := os.MkdirAll(filepath.Dir(info.RootfsDir), 0o755); err != nil {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("create sandbox parent dir: %w", err)
	}
	if needsBootstrap(info.RootfsDir) {
		setupCtx, cancel := context.WithTimeout(ctx, m.cfg.BackgroundTaskSandboxSetupTimeout())
		defer cancel()
		if err := m.runCommand(setupCtx, "debootstrap",
			"--arch="+m.architecture(options.Architecture),
			m.release(options.Release),
			info.RootfsDir,
			m.mirror(),
		); err != nil {
			return tools.BackgroundTaskSandboxInfo{}, err
		}
	}

	info.State = "created"
	if options.AutoStart {
		return m.StartSandbox(ctx, name)
	}
	return info, nil
}

func (m *Manager) StartSandbox(ctx context.Context, name string) (tools.BackgroundTaskSandboxInfo, error) {
	if err := m.requirePrivilegeForSandbox("systemd-nspawn"); err != nil {
		return tools.BackgroundTaskSandboxInfo{}, err
	}

	name = sanitizeSandboxName(name)
	if name == "" {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("sandbox name must not be empty")
	}

	info := m.baseInfo(name)
	if needsBootstrap(info.RootfsDir) {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("sandbox %q has no rootfs yet; create it first", name)
	}

	if running, ok := m.activeProcess(name); ok && running.cmd.Process != nil {
		info.State = "running"
		return info, nil
	}

	logPath := filepath.Join(filepath.Dir(info.RootfsDir), "nspawn.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("create sandbox log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("open sandbox log: %w", err)
	}

	args := []string{
		"--quiet",
		"--boot",
		"--directory=" + info.RootfsDir,
		"--machine=" + name,
		"--bind=" + m.cfg.App.WorkspaceRoot + ":" + m.cfg.App.WorkspaceRoot,
	}
	cmdName, cmdArgs := m.commandSpec("systemd-nspawn", args...)
	cmd := exec.CommandContext(context.Background(), cmdName, cmdArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("start sandbox %q: %w", name, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
		_ = logFile.Close()
		m.clearActive(name)
	}()
	m.setActive(name, &processState{cmd: cmd, done: done})

	if err := m.waitForRunning(ctx, name, done); err != nil {
		_, _ = m.StopSandbox(context.Background(), name, true)
		return tools.BackgroundTaskSandboxInfo{}, err
	}

	info.State = "running"
	info.StartedAt = time.Now().UTC()
	return info, nil
}

func (m *Manager) StopSandbox(ctx context.Context, name string, force bool) (tools.BackgroundTaskSandboxInfo, error) {
	name = sanitizeSandboxName(name)
	if name == "" {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("sandbox name must not be empty")
	}
	info := m.baseInfo(name)

	err := m.runCommand(ctx, "machinectl", "terminate", name)
	if err != nil && force {
		if proc, ok := m.activeProcess(name); ok && proc.cmd.Process != nil {
			_ = proc.cmd.Process.Kill()
			err = nil
		}
	}
	if err != nil {
		return tools.BackgroundTaskSandboxInfo{}, err
	}

	if proc, ok := m.activeProcess(name); ok {
		select {
		case <-ctx.Done():
			return tools.BackgroundTaskSandboxInfo{}, ctx.Err()
		case <-time.After(5 * time.Second):
		case <-proc.done:
		}
	}

	info.State = "stopped"
	return info, nil
}

func (m *Manager) DeleteSandbox(ctx context.Context, name string, force bool) (tools.BackgroundTaskSandboxInfo, error) {
	name = sanitizeSandboxName(name)
	if name == "" {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("sandbox name must not be empty")
	}
	info := m.baseInfo(name)
	if _, err := m.StopSandbox(ctx, name, force); err != nil && !force {
		return tools.BackgroundTaskSandboxInfo{}, err
	}
	if err := os.RemoveAll(filepath.Dir(info.RootfsDir)); err != nil {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("delete sandbox %q: %w", name, err)
	}
	info.State = "deleted"
	return info, nil
}

func (m *Manager) InspectSandbox(ctx context.Context, name string) (tools.BackgroundTaskSandboxInfo, error) {
	name = sanitizeSandboxName(name)
	if name == "" {
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("sandbox name must not be empty")
	}
	info := m.baseInfo(name)
	if _, err := os.Stat(info.RootfsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("sandbox %q was not found", name)
		}
		return tools.BackgroundTaskSandboxInfo{}, fmt.Errorf("stat sandbox %q: %w", name, err)
	}

	if running, ok := m.activeProcess(name); ok && running.cmd.Process != nil {
		info.State = "running"
		return info, nil
	}

	if err := m.runCommand(ctx, "machinectl", "show", name); err == nil {
		info.State = "running"
		return info, nil
	}

	info.State = "stopped"
	return info, nil
}

func (m *Manager) ListSandboxes(ctx context.Context) ([]tools.BackgroundTaskSandboxInfo, error) {
	root := m.cfg.BackgroundTasks.Sandbox.MachinesDir
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sandbox dir: %w", err)
	}

	items := make([]tools.BackgroundTaskSandboxInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := m.InspectSandbox(ctx, entry.Name())
		if err != nil {
			continue
		}
		items = append(items, info)
	}
	return items, nil
}

func (m *Manager) ExecInSandbox(ctx context.Context, options tools.SandboxExecOptions) (tools.SandboxExecResult, error) {
	name := sanitizeSandboxName(options.Name)
	if name == "" {
		return tools.SandboxExecResult{}, fmt.Errorf("sandbox name must not be empty")
	}
	if _, err := m.InspectSandbox(ctx, name); err != nil {
		return tools.SandboxExecResult{}, err
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = m.cfg.ExecTimeout()
	}
	timedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := strings.TrimSpace(options.Command)
	workingDir := strings.TrimSpace(options.WorkingDir)
	if workingDir == "" {
		workingDir = m.cfg.App.WorkspaceRoot
	}
	shellCommand := fmt.Sprintf("cd %s && %s", shellQuote(workingDir), command)

	cmdName, cmdArgs := m.commandSpec("systemd-run",
		"--quiet",
		"--wait",
		"--pipe",
		"--collect",
		"--machine="+name,
		"/bin/sh",
		"-lc",
		shellCommand,
	)
	cmd := exec.CommandContext(timedCtx, cmdName, cmdArgs...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

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
			return tools.SandboxExecResult{}, fmt.Errorf("run sandbox command in %q: %w", name, err)
		}
	}

	return tools.SandboxExecResult{
		Command:    command,
		ExitCode:   exitCode,
		DurationMS: duration.Milliseconds(),
		TimedOut:   timedOut,
		Output:     output.String(),
	}, nil
}

func (m *Manager) waitForRunning(ctx context.Context, name string, done chan error) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("sandbox %q did not become ready before timeout", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err != nil {
				return fmt.Errorf("sandbox %q exited during startup: %w", name, err)
			}
			return fmt.Errorf("sandbox %q exited before becoming ready", name)
		case <-time.After(500 * time.Millisecond):
		}

		checkCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := m.runCommand(checkCtx, "machinectl", "show", name)
		cancel()
		if err == nil {
			return nil
		}
	}
}

func (m *Manager) runCommand(ctx context.Context, name string, args ...string) error {
	cmdName, cmdArgs := m.commandSpec(name, args...)
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		display := strings.TrimSpace(strings.Join(append([]string{cmdName}, cmdArgs...), " "))
		return fmt.Errorf("%s: %w: %s", display, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *Manager) requirePrivilegeForSandbox(command string) error {
	if os.Geteuid() == 0 || m.cfg.BackgroundTasks.Sandbox.UseSudo {
		return nil
	}
	command = strings.TrimSpace(command)
	if command == "" {
		command = "sandbox setup"
	}
	return fmt.Errorf("%s requires root privileges; run the Lumen service as root, enable background_tasks.sandbox.use_sudo, or disable background_tasks.sandbox", command)
}

func (m *Manager) commandSpec(name string, args ...string) (string, []string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", append([]string(nil), args...)
	}
	if os.Geteuid() != 0 && m.cfg.BackgroundTasks.Sandbox.UseSudo {
		return "sudo", append([]string{name}, args...)
	}
	return name, append([]string(nil), args...)
}

func (m *Manager) activeProcess(name string) (*processState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.active[name]
	return proc, ok
}

func (m *Manager) setActive(name string, proc *processState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[name] = proc
}

func (m *Manager) clearActive(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, name)
}

func (m *Manager) baseInfo(name string) tools.BackgroundTaskSandboxInfo {
	return tools.BackgroundTaskSandboxInfo{
		Name:         name,
		Provider:     "nspawn",
		RootfsDir:    filepath.Join(m.cfg.BackgroundTasks.Sandbox.MachinesDir, name, "rootfs"),
		WorkspaceDir: m.cfg.App.WorkspaceRoot,
	}
}

func (m *Manager) release(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return strings.TrimSpace(m.cfg.BackgroundTasks.Sandbox.Release)
}

func (m *Manager) architecture(override string) string {
	value := strings.TrimSpace(strings.ToLower(override))
	if value == "" {
		value = strings.TrimSpace(strings.ToLower(m.cfg.BackgroundTasks.Sandbox.Architecture))
	}
	if value != "" {
		return value
	}
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

func (m *Manager) mirror() string {
	value := strings.TrimSpace(m.cfg.BackgroundTasks.Sandbox.Mirror)
	if value == "" {
		return "http://deb.debian.org/debian/"
	}
	return value
}

func needsBootstrap(rootfsDir string) bool {
	entries, err := os.ReadDir(rootfsDir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

func sanitizeSandboxName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
