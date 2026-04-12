package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverrideWorkspaceRootUsesWorkingDirectory(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempRoot := t.TempDir()
	workspaceDir := filepath.Join(tempRoot, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	if err := os.Chdir(tempRoot); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	cfg := Config{}
	if err := cfg.OverrideWorkspaceRoot("workspace"); err != nil {
		t.Fatalf("OverrideWorkspaceRoot returned error: %v", err)
	}

	resolvedExpected, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		t.Fatalf("resolve expected path: %v", err)
	}
	resolvedActual, err := filepath.EvalSymlinks(cfg.App.WorkspaceRoot)
	if err != nil {
		t.Fatalf("resolve actual path: %v", err)
	}

	if resolvedActual != resolvedExpected {
		t.Fatalf("expected workspace root %q, got %q", resolvedExpected, resolvedActual)
	}
}

func TestOverrideWorkspaceRootRejectsMissingDirectory(t *testing.T) {
	cfg := Config{}
	err := cfg.OverrideWorkspaceRoot(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error for missing workspace directory")
	}
}

func TestOverrideWorkspaceRootRepointsDefaultSessionAndMemoryDirs(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempRoot := t.TempDir()
	oldWorkspaceDir := filepath.Join(tempRoot, "old-workspace")
	newWorkspaceDir := filepath.Join(tempRoot, "new-workspace")
	if err := os.MkdirAll(oldWorkspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir old workspace: %v", err)
	}
	if err := os.MkdirAll(newWorkspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir new workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(newWorkspaceDir, ".element-orion", "memory"), 0o755); err != nil {
		t.Fatalf("mkdir new workspace memory: %v", err)
	}

	if err := os.Chdir(tempRoot); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	cfg := Config{}
	cfg.App.WorkspaceRoot = oldWorkspaceDir
	cfg.App.SessionDir = filepath.Join(oldWorkspaceDir, ".element-orion")
	cfg.App.MemoryDir = filepath.Join(cfg.App.SessionDir, "memory")

	if err := cfg.OverrideWorkspaceRoot("new-workspace"); err != nil {
		t.Fatalf("OverrideWorkspaceRoot returned error: %v", err)
	}

	assertResolvedPathEqual(t, cfg.App.WorkspaceRoot, newWorkspaceDir)
	assertResolvedPathEqual(t, cfg.App.SessionDir, filepath.Join(newWorkspaceDir, ".element-orion"))
	assertResolvedPathEqual(t, cfg.App.MemoryDir, filepath.Join(newWorkspaceDir, ".element-orion", "memory"))
}

func TestOverrideWorkspaceRootKeepsExplicitMemoryDir(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempRoot := t.TempDir()
	oldWorkspaceDir := filepath.Join(tempRoot, "old-workspace")
	newWorkspaceDir := filepath.Join(tempRoot, "new-workspace")
	explicitMemoryDir := filepath.Join(tempRoot, "shared-memory")
	if err := os.MkdirAll(oldWorkspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir old workspace: %v", err)
	}
	if err := os.MkdirAll(newWorkspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir new workspace: %v", err)
	}
	if err := os.MkdirAll(explicitMemoryDir, 0o755); err != nil {
		t.Fatalf("mkdir explicit memory dir: %v", err)
	}

	if err := os.Chdir(tempRoot); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	cfg := Config{}
	cfg.App.WorkspaceRoot = oldWorkspaceDir
	cfg.App.SessionDir = filepath.Join(oldWorkspaceDir, ".element-orion")
	cfg.App.MemoryDir = explicitMemoryDir

	if err := cfg.OverrideWorkspaceRoot("new-workspace"); err != nil {
		t.Fatalf("OverrideWorkspaceRoot returned error: %v", err)
	}

	assertResolvedPathEqual(t, cfg.App.MemoryDir, explicitMemoryDir)
}

func assertResolvedPathEqual(t *testing.T, actual string, expected string) {
	t.Helper()

	resolvedExpected, err := filepath.EvalSymlinks(expected)
	if err != nil {
		t.Fatalf("resolve expected path %q: %v", expected, err)
	}
	resolvedActual, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatalf("resolve actual path %q: %v", actual, err)
	}
	if resolvedActual != resolvedExpected {
		t.Fatalf("expected path %q, got %q", resolvedExpected, resolvedActual)
	}
}

func TestValidateEventWebhookRequiresHeartbeat(t *testing.T) {
	workspaceDir := t.TempDir()
	sessionDir := t.TempDir()

	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = workspaceDir
	cfg.App.SessionDir = sessionDir
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.EventWebhook.Enabled = true
	cfg.EventWebhook.DefaultMode = "now"
	cfg.Heartbeat.Every = ""
	cfg.Heartbeat.Target.ChannelID = ""
	cfg.Heartbeat.Target.UserID = ""

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error when event webhook is enabled without heartbeat")
	}
	if !strings.Contains(err.Error(), "event_webhook.enabled requires heartbeat") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateEventWebhookRejectsInvalidDefaultMode(t *testing.T) {
	workspaceDir := t.TempDir()
	sessionDir := t.TempDir()

	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = workspaceDir
	cfg.App.SessionDir = sessionDir
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.EventWebhook.Enabled = true
	cfg.EventWebhook.DefaultMode = "later"
	cfg.Heartbeat.Target.ChannelID = "channel-1"
	cfg.Heartbeat.Target.UserID = "user-1"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for invalid event webhook default mode")
	}
	if !strings.Contains(err.Error(), "event_webhook.default_mode") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestResolveEventWebhookSecretFromEnv(t *testing.T) {
	t.Setenv("ELEMENT_ORION_EVENT_WEBHOOK_SECRET_TEST", "secret-from-env")

	cfg := Config{
		EventWebhook: EventWebhookConfig{
			SecretEnv: "ELEMENT_ORION_EVENT_WEBHOOK_SECRET_TEST",
		},
	}

	secret, err := cfg.ResolveEventWebhookSecret()
	if err != nil {
		t.Fatalf("ResolveEventWebhookSecret returned error: %v", err)
	}
	if secret != "secret-from-env" {
		t.Fatalf("expected secret %q, got %q", "secret-from-env", secret)
	}
}

func TestResolvePathsSetsDefaultMemoryDirAndToolCallLimit(t *testing.T) {
	tempRoot := t.TempDir()

	cfg := defaultConfig()
	cfg.sourcePath = filepath.Join(tempRoot, "lumen.yaml")
	cfg.App.WorkspaceRoot = "."
	cfg.App.SessionDir = ".element-orion"
	cfg.App.MemoryDir = ""
	cfg.App.MaxToolCallsPerTurn = 0

	if err := cfg.resolvePaths(); err != nil {
		t.Fatalf("resolvePaths returned error: %v", err)
	}

	if cfg.App.MaxToolCallsPerTurn != 24 {
		t.Fatalf("expected default max tool calls per turn to be 24, got %d", cfg.App.MaxToolCallsPerTurn)
	}
	if cfg.App.MaxAgentLoops != 12 {
		t.Fatalf("expected default max agent loops to be 12, got %d", cfg.App.MaxAgentLoops)
	}
	if !cfg.Heartbeat.IsolatedSession {
		t.Fatal("expected heartbeat isolated session to default to true")
	}
	if cfg.LLM.RequestMaxAttempts != 3 {
		t.Fatalf("expected default request max attempts to be 3, got %d", cfg.LLM.RequestMaxAttempts)
	}
	if cfg.Dashboard.ListenAddr != "127.0.0.1:8788" {
		t.Fatalf("expected default dashboard listen addr, got %q", cfg.Dashboard.ListenAddr)
	}
	if cfg.Dashboard.Path != "/dashboard" {
		t.Fatalf("expected default dashboard path, got %q", cfg.Dashboard.Path)
	}
	if cfg.GIFs.Provider != "giphy" {
		t.Fatalf("expected default GIF provider %q, got %q", "giphy", cfg.GIFs.Provider)
	}

	wantMemoryDir := filepath.Join(cfg.App.SessionDir, "memory")
	if resolvedMemory, err := filepath.EvalSymlinks(cfg.App.MemoryDir); err == nil {
		cfg.App.MemoryDir = resolvedMemory
	}
	if resolvedWant, err := filepath.EvalSymlinks(wantMemoryDir); err == nil {
		wantMemoryDir = resolvedWant
	}
	if cfg.App.MemoryDir != wantMemoryDir {
		t.Fatalf("expected memory dir %q, got %q", wantMemoryDir, cfg.App.MemoryDir)
	}
}

func TestResolvePathsCreatesSandboxMachinesDirWhenEnabled(t *testing.T) {
	tempRoot := t.TempDir()

	cfg := defaultConfig()
	cfg.sourcePath = filepath.Join(tempRoot, "lumen.yaml")
	cfg.App.WorkspaceRoot = "."
	cfg.App.SessionDir = ".element-orion"
	cfg.BackgroundTasks.Sandbox.Enabled = true
	cfg.BackgroundTasks.Sandbox.MachinesDir = ".element-orion/sandboxes"

	if err := cfg.resolvePaths(); err != nil {
		t.Fatalf("resolvePaths returned error: %v", err)
	}

	info, err := os.Stat(cfg.BackgroundTasks.Sandbox.MachinesDir)
	if err != nil {
		t.Fatalf("stat sandbox machines dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected sandbox machines dir %q to be a directory", cfg.BackgroundTasks.Sandbox.MachinesDir)
	}
}

func TestValidateRejectsUnknownLLMAPIType(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.LLM.APIType = "mystery"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for unknown llm.api_type")
	}
	if !strings.Contains(err.Error(), "llm.api_type") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRejectsUnknownLLMReasoningEffort(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.LLM.ReasoningEffort = "turbo"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for unknown llm.reasoning_effort")
	}
	if !strings.Contains(err.Error(), "llm.reasoning_effort") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRejectsInvalidMaxThinkingToken(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.LLM.MaxThinkingToken = "abc"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for invalid llm.max_thinking_token")
	}
	if !strings.Contains(err.Error(), "llm.max_thinking_token") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateDreamModeRequiresSleepHoursWhenEnabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.DreamMode.Enabled = true
	cfg.DreamMode.Every = "6h"
	cfg.DreamMode.SleepHours = HeartbeatActiveHoursConfig{}

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for missing dream mode sleep hours")
	}
	if !strings.Contains(err.Error(), "dream_mode.sleep_hours") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRejectsContextWindowNotAboveMaxTokens(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.LLM.ContextWindowTokens = cfg.LLM.MaxTokens

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error when context window is not above max tokens")
	}
	if !strings.Contains(err.Error(), "llm.context_window_tokens") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRejectsUnknownGuildSessionScope(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.Discord.GuildSessionScope = "guild"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for unknown discord.guild_session_scope")
	}
	if !strings.Contains(err.Error(), "discord.guild_session_scope") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateDashboardRejectsRelativePath(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.Dashboard.Enabled = true
	cfg.Dashboard.Path = "dashboard"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for relative dashboard path")
	}
	if !strings.Contains(err.Error(), "dashboard.path") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestResolvePathsNormalizesDiscordListsAndSessionScope(t *testing.T) {
	tempRoot := t.TempDir()

	cfg := defaultConfig()
	cfg.sourcePath = filepath.Join(tempRoot, "lumen.yaml")
	cfg.Discord.AllowedDMUserIDs = []string{" user-1 ", "", "user-1", "user-2"}
	cfg.Discord.AllowedOutboundChannelIDs = []string{" chan-1 ", "chan-1", "chan-2"}
	cfg.Discord.GuildSessionScope = " USER "
	cfg.GIFs.Provider = " GIPHY "
	cfg.GIFs.ContentFilter = " HIGH "
	cfg.LLM.ReasoningEffort = " NONE "
	cfg.LLM.MaxThinkingToken = " OFF "
	cfg.LLM.Headers = map[string]string{" X-Shared ": " value ", "": "drop-me"}

	if err := cfg.resolvePaths(); err != nil {
		t.Fatalf("resolvePaths returned error: %v", err)
	}

	if got, want := cfg.Discord.AllowedDMUserIDs, []string{"user-1", "user-2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected normalized DM allowlist %v, got %v", want, got)
	}
	if got, want := cfg.Discord.AllowedOutboundChannelIDs, []string{"chan-1", "chan-2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected normalized outbound channel allowlist %v, got %v", want, got)
	}
	if cfg.Discord.GuildSessionScope != "user" {
		t.Fatalf("expected normalized guild session scope %q, got %q", "user", cfg.Discord.GuildSessionScope)
	}
	if cfg.GIFs.Provider != "giphy" {
		t.Fatalf("expected normalized GIF provider %q, got %q", "giphy", cfg.GIFs.Provider)
	}
	if cfg.GIFs.ContentFilter != "pg-13" {
		t.Fatalf("expected normalized GIF content filter %q, got %q", "pg-13", cfg.GIFs.ContentFilter)
	}
	if cfg.LLM.ReasoningEffort != "none" {
		t.Fatalf("expected normalized reasoning effort %q, got %q", "none", cfg.LLM.ReasoningEffort)
	}
	if cfg.LLM.MaxThinkingToken != "off" {
		t.Fatalf("expected normalized max thinking token %q, got %q", "off", cfg.LLM.MaxThinkingToken)
	}
	if got := cfg.LLM.Headers["X-Shared"]; got != "value" {
		t.Fatalf("expected normalized shared header value %q, got %q", "value", got)
	}
	if _, ok := cfg.LLM.Headers[""]; ok {
		t.Fatal("expected empty shared header key to be removed")
	}
}

func TestValidateRequiresUserTokenInUserMode(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.TokenMode = "user"
	cfg.Discord.BotToken = ""
	cfg.Discord.UserToken = ""
	cfg.Discord.AllowGroupDirectMessages = true

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for missing discord.user_token")
	}
	if !strings.Contains(err.Error(), "discord.user_token") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateAllowsGroupDirectMessagesWithoutGuildsOrDMs(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.TokenMode = "user"
	cfg.Discord.BotToken = ""
	cfg.Discord.UserToken = "user-token"
	cfg.Discord.AllowDirectMessages = false
	cfg.Discord.AllowGroupDirectMessages = true
	cfg.Discord.AllowedGuildIDs = nil

	if err := cfg.validate(); err != nil {
		t.Fatalf("expected group direct messages to satisfy the routing requirement, got %v", err)
	}
}

func TestResolveDiscordAuthorizationHeaderUsesUserTokenRaw(t *testing.T) {
	cfg := defaultConfig()
	cfg.Discord.TokenMode = "user"
	cfg.Discord.UserToken = "user-token"

	value, err := cfg.ResolveDiscordAuthorizationHeader()
	if err != nil {
		t.Fatalf("ResolveDiscordAuthorizationHeader returned error: %v", err)
	}
	if value != "user-token" {
		t.Fatalf("expected raw user token header, got %q", value)
	}
}

func TestValidateRejectsInvalidRetryAttempts(t *testing.T) {
	cfg := defaultConfig()
	cfg.App.WorkspaceRoot = t.TempDir()
	cfg.App.SessionDir = t.TempDir()
	cfg.App.MemoryDir = t.TempDir()
	cfg.Discord.BotToken = "token"
	cfg.Discord.AllowDirectMessages = true
	cfg.LLM.RequestMaxAttempts = 0

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for llm.request_max_attempts")
	}
	if !strings.Contains(err.Error(), "llm.request_max_attempts") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestResolveGIFAPIKeyFromEnv(t *testing.T) {
	t.Setenv("GIPHY_API_KEY_TEST", "giphy-secret")

	cfg := Config{
		GIFs: GIFConfig{
			APIKeyEnv: "GIPHY_API_KEY_TEST",
		},
	}

	value, err := cfg.ResolveGIFAPIKey()
	if err != nil {
		t.Fatalf("ResolveGIFAPIKey returned error: %v", err)
	}
	if value != "giphy-secret" {
		t.Fatalf("expected API key %q, got %q", "giphy-secret", value)
	}
}

func TestDMAllowedForUserUsesAllowlistWhenPresent(t *testing.T) {
	cfg := defaultConfig()
	cfg.Discord.AllowDirectMessages = true
	cfg.Discord.AllowedDMUserIDs = []string{"user-1"}

	if !cfg.DMAllowedForUser("user-1") {
		t.Fatal("expected listed DM user to be allowed")
	}
	if cfg.DMAllowedForUser("user-2") {
		t.Fatal("expected unlisted DM user to be blocked")
	}
}

func TestDiscordChannelAllowedAcceptsActiveOrAllowlistedChannel(t *testing.T) {
	cfg := defaultConfig()
	cfg.Discord.AllowedOutboundChannelIDs = []string{"channel-2"}

	if !cfg.DiscordChannelAllowed("channel-1", "channel-1") {
		t.Fatal("expected active channel to be allowed")
	}
	if !cfg.DiscordChannelAllowed("channel-2", "channel-1") {
		t.Fatal("expected allowlisted outbound channel to be allowed")
	}
	if cfg.DiscordChannelAllowed("channel-3", "channel-1") {
		t.Fatal("expected unlisted outbound channel to be blocked")
	}
}
