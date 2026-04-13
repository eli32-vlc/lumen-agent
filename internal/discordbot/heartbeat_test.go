package discordbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"element-orion/internal/agent"
	"element-orion/internal/auditlog"
	"element-orion/internal/config"
)

func TestClassifyHeartbeatReplyQuietAck(t *testing.T) {
	disposition := classifyHeartbeatReply("HEARTBEAT_OK all clear", 300)
	if !disposition.Quiet {
		t.Fatal("expected quiet heartbeat ack")
	}
	if disposition.Content != "all clear" {
		t.Fatalf("unexpected quiet ack content %q", disposition.Content)
	}
}

func TestClassifyHeartbeatReplyKeepsMiddleTokenNormal(t *testing.T) {
	disposition := classifyHeartbeatReply("alert HEARTBEAT_OK please check logs", 300)
	if disposition.Quiet {
		t.Fatal("did not expect quiet ack when token is in the middle")
	}
	if disposition.Content != "alert HEARTBEAT_OK please check logs" {
		t.Fatalf("unexpected content %q", disposition.Content)
	}
}

func TestClassifyHeartbeatReplyLongAckBecomesAlert(t *testing.T) {
	disposition := classifyHeartbeatReply("HEARTBEAT_OK this message is too long to be a quiet ack", 10)
	if disposition.Quiet {
		t.Fatal("expected long ack to be treated as an alert")
	}
	if disposition.Content != "this message is too long to be a quiet ack" {
		t.Fatalf("unexpected alert content %q", disposition.Content)
	}
}

func TestHeartbeatChecklistStateTreatsHeadersAsEmpty(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "HEARTBEAT.md")
	if err := os.WriteFile(path, []byte("# Checklist\n\n## Notes\n"), 0o644); err != nil {
		t.Fatalf("write HEARTBEAT.md: %v", err)
	}

	exists, hasContent, err := heartbeatChecklistState(path)
	if err != nil {
		t.Fatalf("heartbeatChecklistState returned error: %v", err)
	}
	if !exists {
		t.Fatal("expected HEARTBEAT.md to exist")
	}
	if hasContent {
		t.Fatal("expected headers-only HEARTBEAT.md to be treated as empty")
	}
}

func TestWithinHeartbeatActiveHoursSupportsWraparound(t *testing.T) {
	service := &Service{cfg: config.Config{Heartbeat: config.HeartbeatConfig{ActiveHours: config.HeartbeatActiveHoursConfig{Timezone: "UTC", Start: "22:00", End: "06:00"}}}}
	if !service.withinHeartbeatActiveHours(time.Date(2026, 3, 12, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("expected 23:00 UTC to be inside wrapped active hours")
	}
	if service.withinHeartbeatActiveHours(time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("expected 12:00 UTC to be outside wrapped active hours")
	}
}

func TestWithinDreamSleepHoursSupportsWraparound(t *testing.T) {
	service := &Service{cfg: config.Config{DreamMode: config.DreamModeConfig{Enabled: true, SleepHours: config.HeartbeatActiveHoursConfig{Timezone: "UTC", Start: "22:00", End: "06:00"}}}}
	if !service.withinDreamSleepHours(time.Date(2026, 3, 12, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("expected 23:00 UTC to be inside wrapped dream sleep hours")
	}
	if service.withinDreamSleepHours(time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("expected 12:00 UTC to be outside dream sleep hours")
	}
}

func TestBuildDreamPromptMentionsMemoryMaintenance(t *testing.T) {
	prompt := buildDreamPrompt()
	for _, snippet := range []string{
		"dream mode maintenance run",
		"review the configured memory directory",
		"Read the memory files",
		"Verify every saved memory file",
		"DREAM_OK",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected dream prompt to contain %q", snippet)
		}
	}
}

func TestLogDreamEventWritesRetryStatus(t *testing.T) {
	logDir := t.TempDir()
	logger, err := auditlog.New(logDir)
	if err != nil {
		t.Fatalf("new audit logger: %v", err)
	}
	defer logger.Close()

	service := &Service{
		cfg:   config.Config{DreamMode: config.DreamModeConfig{Model: "gpt-dream"}},
		audit: logger,
	}

	service.logDreamEvent(agent.Event{
		Kind:    agent.EventStatus,
		Message: "Model request hit transient error. Retrying (2/3)",
	})

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one audit log file")
	}

	data, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"dream_status"`) {
		t.Fatalf("expected dream_status entry, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), `"model":"gpt-dream"`) {
		t.Fatalf("expected dream model in log entry, got:\n%s", string(data))
	}
}

func TestResolveHeartbeatChecklistPathFindsLowercaseFile(t *testing.T) {
	root := t.TempDir()
	lowerPath := filepath.Join(root, "heartbeat.md")
	if err := os.WriteFile(lowerPath, []byte("- [ ] Check deploy status"), 0o644); err != nil {
		t.Fatalf("write heartbeat.md: %v", err)
	}

	path, name := resolveHeartbeatChecklistPath(root)
	gotInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat resolved heartbeat path: %v", err)
	}
	wantInfo, err := os.Stat(lowerPath)
	if err != nil {
		t.Fatalf("stat lowercase heartbeat path: %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("expected resolved heartbeat path %q to point at %q", path, lowerPath)
	}
	if !strings.EqualFold(name, "heartbeat.md") {
		t.Fatalf("expected heartbeat name to match heartbeat.md, got %q", name)
	}
}

func TestBuildHeartbeatPromptRequiresRealFileWrites(t *testing.T) {
	prompt := buildHeartbeatPrompt([]heartbeatSystemEvent{{Text: "Write next 3 AM check-in to heartbeat.md"}}, true, "heartbeat.md")

	for _, snippet := range []string{
		"Review heartbeat.md",
		"Action-first behavior: complete obvious low-risk steps without asking follow-up questions",
		"If a task is ambiguous but has a safe default, choose it and continue",
		"Do not infer or repeat old tasks from prior chats",
		"If a one-off reminder or check-in was already delivered, do not send it again",
		"When multiple queued system events are present, work through all of them in this run when feasible",
		"use tools to perform the change",
		"Never claim a file update succeeded unless a write tool call succeeded",
		"delete it or mark it done in the file",
		"Use the injected heartbeat state to avoid clingy behavior",
		"Queued system events:",
		"Write next 3 AM check-in to heartbeat.md",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected heartbeat prompt to contain %q", snippet)
		}
	}
}

func TestConsumeHeartbeatEventsKeepsFilesUntilAcknowledged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event-now.json")
	event := heartbeatSystemEvent{Text: "Check in at 3 AM", Mode: heartbeatModeNow, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write event file: %v", err)
	}

	loaded, err := consumeHeartbeatEvents(dir, heartbeatModeNow)
	if err != nil {
		t.Fatalf("consumeHeartbeatEvents returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one queued heartbeat event, got %d", len(loaded))
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected event file to remain before ack, stat error: %v", err)
	}

	if err := acknowledgeHeartbeatEvents(loaded); err != nil {
		t.Fatalf("acknowledgeHeartbeatEvents returned error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected event file to be removed after ack, stat error: %v", err)
	}
}

func TestConsumeHeartbeatEventsSkipsOtherModeWithoutDeleting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event-scheduled.json")
	event := heartbeatSystemEvent{Text: "Do a scheduled check", Mode: heartbeatModeScheduled, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write event file: %v", err)
	}

	loaded, err := consumeHeartbeatEvents(dir, heartbeatModeNow)
	if err != nil {
		t.Fatalf("consumeHeartbeatEvents returned error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected zero queued heartbeat events, got %d", len(loaded))
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected mismatched-mode event file to stay on disk, stat error: %v", err)
	}
}
