package sandbox

import (
	"os"
	"strings"
	"testing"

	"element-orion/internal/config"
)

func TestCommandSpecUsesSudoWhenEnabledAndNotRoot(t *testing.T) {
	manager := &Manager{cfg: config.Config{
		BackgroundTasks: config.BackgroundTasksConfig{
			Sandbox: config.BackgroundTaskSandboxConfig{
				UseSudo: true,
			},
		},
	}}

	if isRootForSandboxTests() {
		t.Skip("sudo wrapping only applies to non-root processes")
	}

	name, args := manager.commandSpec("systemd-run", "--wait")
	if name != "sudo" {
		t.Fatalf("expected sudo command wrapper, got %q", name)
	}
	if len(args) != 2 || args[0] != "systemd-run" || args[1] != "--wait" {
		t.Fatalf("unexpected wrapped args: %#v", args)
	}
}

func TestRequirePrivilegeForSandboxMentionsUseSudo(t *testing.T) {
	manager := &Manager{}
	if isRootForSandboxTests() {
		t.Skip("root bypasses privilege error")
	}

	err := manager.requirePrivilegeForSandbox("debootstrap")
	if err == nil {
		t.Fatal("expected privilege error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "debootstrap", "use_sudo") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func isRootForSandboxTests() bool {
	return os.Geteuid() == 0
}
