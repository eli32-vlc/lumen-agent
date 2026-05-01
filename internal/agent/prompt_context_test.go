package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"element-orion/internal/config"
	"element-orion/internal/heartbeatstate"
	"element-orion/internal/skills"
)

func TestSystemPromptLoadsPersistentWorkspaceFiles(t *testing.T) {
	workspace := t.TempDir()
	memoryRoot := filepath.Join(workspace, ".memory")
	writeTestFile(t, workspace, "BOOTSTRAP.md", "bootstrap ritual")
	writeTestFile(t, workspace, "IDENTITY.md", "name: Element Orion")
	writeTestFile(t, workspace, "USER.md", "name: Eason")
	writeTestFile(t, workspace, "SOUL.md", "# Who You Are")
	writeTestFile(t, workspace, "CODEBASE.md", "cmd/element-orion/main.go: CLI entrypoint")
	writeTestFile(t, workspace, "TASKS.md", "## Active\n- [ ] Finish repo audit")
	writeTestFile(t, memoryRoot, "MEMORY.md", "curated memory")
	writeTestFile(t, memoryRoot, "2026-03-12-PM.md", "current shard")
	writeTestFile(t, memoryRoot, "2026-03-12-AM.md", "previous shard")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace, MemoryDir: memoryRoot}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: true,
		Now:             time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"[BEGIN BOOTSTRAP.md]",
		"bootstrap ritual",
		"[BEGIN IDENTITY.md]",
		"name: Element Orion",
		"[BEGIN USER.md]",
		"name: Eason",
		"[BEGIN SOUL.md]",
		"# Who You Are",
		"[BEGIN CODEBASE.md]",
		"cmd/element-orion/main.go: CLI entrypoint",
		"[BEGIN TASKS.md]",
		"Finish repo audit",
		"[BEGIN MEMORY.md]",
		"curated memory",
		"[BEGIN memory/2026-03-12-PM.md]",
		"current shard",
		"[BEGIN memory/2026-03-12-AM.md]",
		"previous shard",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptLoadsAllMemoryShardsWhenEnabled(t *testing.T) {
	workspace := t.TempDir()
	memoryRoot := filepath.Join(workspace, ".memory")
	writeTestFile(t, memoryRoot, "MEMORY.md", "curated memory")
	writeTestFile(t, memoryRoot, "2026-03-12-PM.md", "current shard")
	writeTestFile(t, memoryRoot, "2026-03-12-AM.md", "previous shard")
	writeTestFile(t, memoryRoot, "2026-03-11-PM.md", "older shard")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{
		WorkspaceRoot:       workspace,
		MemoryDir:           memoryRoot,
		LoadAllMemoryShards: true,
	}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: true,
		Now:             time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"[BEGIN memory/2026-03-12-PM.md]",
		"[BEGIN memory/2026-03-12-AM.md]",
		"[BEGIN memory/2026-03-11-PM.md]",
		"older shard",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptSkipsCuratedMemoryOutsidePrivateSessions(t *testing.T) {
	workspace := t.TempDir()
	memoryRoot := filepath.Join(workspace, ".memory")
	writeTestFile(t, memoryRoot, "MEMORY.md", "curated memory")
	writeTestFile(t, memoryRoot, "2026-03-12-PM.md", "current shard")
	writeTestFile(t, memoryRoot, "2026-03-12-AM.md", "previous shard")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace, MemoryDir: memoryRoot}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: false,
		Now:             time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC),
	})

	if strings.Contains(prompt, "[BEGIN MEMORY.md]") {
		t.Fatalf("expected MEMORY.md to stay out of shared contexts")
	}
	if strings.Contains(prompt, "[BEGIN memory/2026-03-12-PM.md]") {
		t.Fatalf("did not expect current shard in shared context")
	}
	if strings.Contains(prompt, "[BEGIN memory/2026-03-12-AM.md]") {
		t.Fatalf("did not expect previous shard in shared context")
	}
}

func TestSystemPromptLoadsGuildMemoryForSharedChannelSessions(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := filepath.Join(workspace, ".element-orion")
	guildMemoryRoot := filepath.Join(sessionDir, "guild-memory", "guild-1", "channel-1")
	writeTestFile(t, guildMemoryRoot, "MEMORY.md", "shared channel facts")
	writeTestFile(t, guildMemoryRoot, "2026-03-12-PM.md", "current shared shard")
	writeTestFile(t, guildMemoryRoot, "2026-03-12-AM.md", "previous shared shard")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace, SessionDir: sessionDir}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: false,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		Now:             time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"[BEGIN guild-memory/MEMORY.md]",
		"shared channel facts",
		"[BEGIN guild-memory/2026-03-12-PM.md]",
		"current shared shard",
		"[BEGIN guild-memory/2026-03-12-AM.md]",
		"previous shared shard",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptLoadsAllGuildMemoryShardsWhenEnabled(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := filepath.Join(workspace, ".element-orion")
	guildMemoryRoot := filepath.Join(sessionDir, "guild-memory", "guild-1", "channel-1")
	writeTestFile(t, guildMemoryRoot, "MEMORY.md", "shared channel facts")
	writeTestFile(t, guildMemoryRoot, "2026-03-12-PM.md", "current shared shard")
	writeTestFile(t, guildMemoryRoot, "2026-03-12-AM.md", "previous shared shard")
	writeTestFile(t, guildMemoryRoot, "2026-03-11-PM.md", "older shared shard")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{
		WorkspaceRoot:       workspace,
		SessionDir:          sessionDir,
		LoadAllMemoryShards: true,
	}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: false,
		GuildID:         "guild-1",
		ChannelID:       "channel-1",
		Now:             time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"[BEGIN guild-memory/2026-03-12-PM.md]",
		"[BEGIN guild-memory/2026-03-12-AM.md]",
		"[BEGIN guild-memory/2026-03-11-PM.md]",
		"older shared shard",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestMemoryShardPathsCrossDayBoundary(t *testing.T) {
	paths := memoryShardPaths(time.Date(2026, 3, 12, 2, 30, 0, 0, time.UTC))
	if len(paths) != 2 {
		t.Fatalf("expected two shard paths, got %d", len(paths))
	}
	if paths[0] != "memory/2026-03-12-AM.md" {
		t.Fatalf("unexpected current shard path %q", paths[0])
	}
	if paths[1] != "memory/2026-03-11-PM.md" {
		t.Fatalf("unexpected previous shard path %q", paths[1])
	}
}

func TestHeartbeatPromptLoadsHeartbeatChecklist(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "IDENTITY.md", "name: Element Orion")
	writeTestFile(t, workspace, "HEARTBEAT.md", "- Check for pending follow-ups")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsHeartbeat: true,
		Now:         time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Heartbeat mode:",
		"[BEGIN HEARTBEAT.md]",
		"Check for pending follow-ups",
		"[BEGIN IDENTITY.md]",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected heartbeat prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptInjectsHeartbeatState(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := filepath.Join(workspace, ".element-orion")
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace, SessionDir: sessionDir}}}

	err := heartbeatstate.Save(runner.cfg, heartbeatstate.State{
		LastProactiveMessageAt: time.Date(2026, 4, 1, 7, 0, 0, 0, time.UTC),
		ProactiveCountToday:    2,
		ProactiveCountDate:     "2026-04-01",
		LastUserMessageAt:      time.Date(2026, 4, 1, 6, 45, 0, 0, time.UTC),
		LastTopic:              "ship the launch note",
		LastBotMessage:         "checking in before the launch note goes out",
		LastBotMessageAt:       time.Date(2026, 4, 1, 7, 0, 0, 0, time.UTC),
		NextEarliestNudgeAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("save heartbeat state: %v", err)
	}

	prompt := runner.systemPrompt(ConversationContext{
		IsHeartbeat: true,
		Now:         time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Heartbeat proactive count today: 2 (date=2026-04-01 UTC)",
		"Heartbeat last topic: ship the launch note",
		"Heartbeat last bot message: checking in before the launch note goes out",
		"Heartbeat next earliest nudge at:",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestHeartbeatLightContextLoadsOnlyHeartbeatChecklist(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "IDENTITY.md", "name: Element Orion")
	writeTestFile(t, workspace, "HEARTBEAT.md", "- Watch for urgent work")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsHeartbeat:  true,
		LightContext: true,
		Now:          time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	if !strings.Contains(prompt, "[BEGIN HEARTBEAT.md]") {
		t.Fatalf("expected HEARTBEAT.md to be loaded in light context")
	}
	if strings.Contains(prompt, "[BEGIN IDENTITY.md]") {
		t.Fatalf("did not expect IDENTITY.md in heartbeat light context")
	}
}

func TestSystemPromptIncludesSafetyAndOutputEfficiencySections(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}

	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: true,
		Now:             time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Executing actions with care:",
		"Examples of actions that usually warrant confirmation:",
		"Output efficiency:",
		"For Discord especially, prefer one clear useful message over a long explanation.",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptIncludesSkillCompatibilityInstallGuidance(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}

	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: true,
		Now:             time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"support both native OpenClaw-style skills and Claude Code-compatible layouts",
		"Native skill layout: place skills at `skills/<name>/SKILL.md`.",
		"Claude Code-compatible layouts: place project skills at `.claude/skills/<name>/SKILL.md`, project commands at `.claude/commands/<name>.md`, user skills at `~/.claude/skills/<name>/SKILL.md`, and user commands at `~/.claude/commands/<name>.md`.",
		"Prefer native workspace `skills/<name>/SKILL.md` when creating reusable repo-owned skills unless the user explicitly wants Claude Code-compatible placement.",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptIncludesProactiveSectionForHeartbeatOnly(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}

	heartbeatPrompt := runner.systemPrompt(ConversationContext{
		IsHeartbeat: true,
		Now:         time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(heartbeatPrompt, "Autonomous work:") {
		t.Fatalf("expected heartbeat prompt to include proactive section")
	}
	if !strings.Contains(heartbeatPrompt, "You may receive wakeups, heartbeat runs, or other system-driven turns") {
		t.Fatalf("expected heartbeat prompt to include proactive guidance")
	}

	chatPrompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: true,
		Now:             time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC),
	})
	if strings.Contains(chatPrompt, "Autonomous work:") {
		t.Fatalf("did not expect normal chat prompt to include proactive section")
	}
}

func TestDreamModePromptIncludesDreamInstructionsAndMetadata(t *testing.T) {
	workspace := t.TempDir()
	memoryRoot := filepath.Join(workspace, ".memory")
	writeTestFile(t, memoryRoot, "MEMORY.md", "curated memory")
	writeTestFile(t, memoryRoot, "2026-03-12-PM.md", "current shard")

	runner := &Runner{cfg: config.Config{
		App: config.AppConfig{
			WorkspaceRoot: workspace,
			MemoryDir:     memoryRoot,
		},
		LLM: config.LLMConfig{
			APIType: "openai",
			BaseURL: "https://api.example.test/v1",
			Model:   "gpt-main",
		},
		DreamMode: config.DreamModeConfig{
			Enabled:      true,
			Every:        "6h",
			Model:        "gpt-dream",
			LightContext: true,
			UseIndicator: true,
			SleepHours: config.HeartbeatActiveHoursConfig{
				Timezone: "Australia/Brisbane",
				Start:    "23:00",
				End:      "06:00",
			},
		},
	}}

	prompt := runner.systemPrompt(ConversationContext{
		IsDreamMode:     true,
		IsDirectMessage: true,
		ModelOverride:   "gpt-dream",
		Now:             time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Dream mode:",
		"Dream mode runs during configured sleep hours",
		"Organize the memory shards deliberately",
		"Compact memory shards when possible",
		"Execution mode: dream mode",
		"Dream mode enabled: enabled",
		"Dream mode schedule: 6h",
		"Dream mode model: gpt-dream",
		"Dream mode light context: enabled",
		"Dream mode typing indicator: enabled",
		"Dream mode sleep hours: 23:00-06:00 Australia/Brisbane",
		"Workspace files root on disk: " + workspace,
		"Memory files root on disk: " + memoryRoot,
		"Workspace file paths: " + filepath.Join(workspace, "BOOTSTRAP.md"),
		"Memory file paths: " + filepath.Join(memoryRoot, "MEMORY.md"),
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
	for _, snippet := range []string{
		"[BEGIN MEMORY.md]",
		"[BEGIN memory/2026-03-12-PM.md]",
	} {
		if strings.Contains(prompt, snippet) {
			t.Fatalf("did not expect dream mode prompt to preload memory section %q", snippet)
		}
	}
}

func TestSystemPromptIncludesProactiveSectionForBackgroundTasks(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}

	prompt := runner.systemPrompt(ConversationContext{
		IsBackgroundTask: true,
		Now:              time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC),
	})

	if !strings.Contains(prompt, "Autonomous work:") {
		t.Fatalf("expected background task prompt to include proactive section")
	}
}

func TestHeartbeatPromptLoadsLowercaseChecklistName(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "heartbeat.md", "- [ ] Check nightly backup")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsHeartbeat: true,
		Now:         time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	if !strings.Contains(prompt, "[BEGIN heartbeat.md]") && !strings.Contains(prompt, "[BEGIN HEARTBEAT.md]") {
		t.Fatalf("expected heartbeat checklist section to be loaded")
	}
	if !strings.Contains(prompt, "Check nightly backup") {
		t.Fatalf("expected lowercase heartbeat.md content to be included")
	}
}

func TestSystemPromptLoadsLowercaseTasksName(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "tasks.md", "## Active\n- [ ] Ship patch")

	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		Now: time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	if !strings.Contains(prompt, "[BEGIN tasks.md]") && !strings.Contains(prompt, "[BEGIN TASKS.md]") {
		t.Fatalf("expected tasks section to be loaded")
	}
	if !strings.Contains(prompt, "Ship patch") {
		t.Fatalf("expected tasks.md content to be included")
	}
}

func TestSystemPromptInjectsSkillSnapshotXML(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		Skills: []skills.Summary{{
			Name:        "github",
			Description: "Use repository workflows",
			Location:    "/tmp/skills/github",
		}},
		Now: time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Available session skills:",
		"<skills>",
		"name=\"github\"",
		"description=\"Use repository workflows\"",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptInjectsAvailableSecrets(t *testing.T) {
	workspace := t.TempDir()
	secretsDir := filepath.Join(workspace, ".lumen")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", secretsDir, err)
	}
	secretsPath := filepath.Join(secretsDir, "secrets.json")
	err := os.WriteFile(secretsPath, []byte(`{"GITHUB_PASS": "actual-password", "OPENAI_KEY": "sk-..."}`), 0o600)
	if err != nil {
		t.Fatalf("write secrets file: %v", err)
	}

	cfg := config.Config{App: config.AppConfig{WorkspaceRoot: workspace, SecretsPath: secretsPath}}
	runner := NewRunner(cfg, nil, nil)

	prompt := runner.systemPrompt(ConversationContext{
		Now: time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Available secrets: GITHUB_PASS, OPENAI_KEY",
		"Use {{secret:NAME}} syntax to reference them in tool calls.",
		"Values are never shown to you and will be automatically redacted if they appear in tool output.",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}

	if strings.Contains(prompt, "actual-password") {
		t.Fatalf("prompt should not contain secret value")
	}
}

func TestSystemPromptInjectsWakeUpTimeWithoutWorkspaceFiles(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("AEST", 10*60*60)
	defer func() {
		time.Local = previousLocal
	}()

	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		Now: time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Wake-up context:",
		"Current local time: 2026-03-13 01:04 AEST (Friday)",
		"UTC tracking time: 2026-03-12 15:04 UTC (Thursday)",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptStrengthensAutonomyGuidance(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		Now: time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Take sensible initiative.",
		"When the next step is obvious, useful, and low-risk, do it instead of waiting for permission.",
		"CODEBASE.md is an optional durable map of the workspace architecture, key directories, and what important files are responsible for.",
		"Build and maintain a concrete mental map of the repository: which files exist, what each relevant file does, and how the moving parts connect.",
		"If CODEBASE.md exists, use it as the durable high-level map of the codebase.",
		"You may create or update skills when repeated work, team-specific workflows, or tool-specific playbooks would benefit from a reusable manual.",
		"TASKS.md is an optional durable task queue for ongoing work, next actions, blockers, and completed items.",
		"TASKS.md (or tasks.md) is optional, but when work spans multiple steps, multiple turns, or pending follow-up, you may create or update it.",
		"Prefer advancing an existing task in TASKS.md over asking the user broad \"what next?\" questions when the next useful step is already clear.",
		"When the user's intent is clear, try to finish the job end-to-end in the same turn instead of stopping at partial progress.",
		"Use tools proactively for inspection, edits, and verification when they materially help you complete the task well.",
		"After useful tool results, keep going toward completion unless you hit a real blocker.",
		"When using read_file, prefer small chunked reads over large dumps.",
		"If a file may be large, read it in sequential chunks using returned line metadata",
		"Treat the runtime metadata and loaded workspace files in this prompt as ground truth for the current session.",
		"Treat the machine-local time as your real sense of \"now\" for conversational awareness, day-part judgment, and answering questions like \"what time is it?\"",
		"Treat UTC timestamps as tracking and storage metadata unless the user explicitly asks for UTC.",
		"Learn the runtime shape of the app before making deep changes: entrypoint, config loading, prompt assembly, tool registry, Discord service loop, heartbeat loop, background-task manager, sandbox manager, and persistence paths.",
		"When a bug crosses multiple layers, trace it end-to-end instead of patching one surface blindly.",
		"First reproduce the problem from the current code and runtime facts instead of trusting the user's or your own first theory.",
		"If the bug involves time, scheduling, or timestamps, check both machine-local time and UTC handling explicitly.",
		"Treat logs, event streams, and saved artifacts as evidence. When there is disagreement between memory and logs, trust the logs.",
		"When reporting background-task progress, rely on verified task state, event logs, or tool output rather than stale assumptions.",
		"If an uploaded file path appears in the prompt, treat that downloaded local path as the primary artifact to inspect.",
		"If context feels thin, rely on the loaded summaries, durable files, recent messages, and tools; do not invent continuity.",
		"Reserve confirmation for destructive, high-risk, expensive, identity-changing, or genuinely ambiguous actions.",
		"If you cannot finish, explain the blocker and the best next step instead of asking broad, unnecessary questions.",
		"During heartbeat runs, prefer action over follow-up questions: complete obvious low-risk steps without asking for confirmation.",
		"If a heartbeat task is ambiguous but has a safe default, choose the default and continue; only escalate when blocked or high-risk.",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptIncludesSharedChannelSilenceGuidance(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: false,
		GuildID:         "guild-1",
		Now:             time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Conversation type: shared guild channel",
		"one coherent channel presence across multiple speakers",
		"exact token <NO_REPLY>",
		"Do not spam the channel with filler updates, repeated summaries, or \"still working\" messages that do not add new verified information.",
		"Sound like a real person in a chat, not a helpdesk macro or a polished assistant demo.",
		"If you use <chunk>, make each chunk feel intentional.",
		"Treat <chunk> as a plain separator token between complete Discord messages, not as XML, HTML, or a wrapper tag.",
		"Never output </chunk>.",
		"Correct pattern: first message<chunk>second message.",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptLabelsSharedGroupDirectMessages(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsDirectMessage: false,
		ChannelID:       "group-dm-1",
		Now:             time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	if !strings.Contains(prompt, "Conversation type: shared group direct message") {
		t.Fatalf("expected prompt to describe a shared group direct message, got %q", prompt)
	}
}

func TestSystemPromptMarksBackgroundTaskMode(t *testing.T) {
	workspace := t.TempDir()
	runner := &Runner{cfg: config.Config{App: config.AppConfig{WorkspaceRoot: workspace}}}
	prompt := runner.systemPrompt(ConversationContext{
		IsBackgroundTask: true,
		Now:              time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Execution mode: background task",
		"do not spawn another background task",
		"check that the requested output actually exists",
		"minimum runtime target, treat it as a floor rather than a hint",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func TestSystemPromptInjectsRuntimeMetadata(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("AEST", 10*60*60)
	defer func() {
		time.Local = previousLocal
	}()

	workspace := t.TempDir()
	sessionDir := filepath.Join(workspace, ".element-orion")
	memoryDir := filepath.Join(sessionDir, "memory")
	cfg := config.Config{
		App: config.AppConfig{
			Name:                "Element Orion",
			WorkspaceRoot:       workspace,
			SessionDir:          sessionDir,
			MemoryDir:           memoryDir,
			MaxAgentLoops:       9,
			MaxToolCallsPerTurn: 15,
			HistoryCompaction: config.AppHistoryCompactionConfig{
				Enabled:                true,
				TriggerTokens:          9000,
				TargetTokens:           5000,
				PreserveRecentMessages: 10,
			},
		},
		LLM: config.LLMConfig{
			APIType:                 "codex",
			BaseURL:                 "https://api.example.test/v1",
			Model:                   "gpt-5.4",
			ReasoningEffort:         "medium",
			Temperature:             0.4,
			MaxTokens:               2048,
			ContextWindowTokens:     8192,
			InjectMessageTimestamps: true,
			Timeout:                 "90s",
			RequestMaxAttempts:      4,
		},
		Tools: config.ToolsConfig{
			ExecShell:             "/bin/bash",
			ExecTimeout:           "30s",
			MaxCommandOutputBytes: 8192,
		},
		Discord: config.DiscordConfig{
			AllowDirectMessages:         true,
			GuildSessionScope:           "channel",
			ReplyToMessage:              true,
			DownloadIncomingAttachments: true,
			IncomingAttachmentsDir:      filepath.Join(sessionDir, "incoming-attachments"),
		},
		BackgroundTasks: config.BackgroundTasksConfig{
			DefaultMinRuntime:  "10m",
			InjectCurrentTime:  true,
			MaxEventLogEntries: 55,
			Sandbox: config.BackgroundTaskSandboxConfig{
				Enabled:      true,
				Force:        false,
				UseSudo:      true,
				Provider:     "nspawn",
				Release:      "stable",
				Architecture: "amd64",
				MachinesDir:  filepath.Join(sessionDir, "sandboxes"),
				AutoCleanup:  true,
			},
		},
		Heartbeat: config.HeartbeatConfig{
			Every:             "45m",
			Model:             "gpt-heartbeat",
			LightContext:      true,
			IsolatedSession:   true,
			EventPollInterval: "7s",
			ActiveHours: config.HeartbeatActiveHoursConfig{
				Timezone: "Australia/Brisbane",
				Start:    "08:00",
				End:      "22:00",
			},
			Target: config.HeartbeatTargetConfig{
				GuildID:   "guild-1",
				ChannelID: "channel-1",
				UserID:    "user-1",
			},
		},
		EventWebhook: config.EventWebhookConfig{
			Enabled: true,
			Path:    "/event",
		},
		MCP: config.MCPConfig{
			Servers: []config.MCPServerConfig{
				{Name: "browser", Enabled: true},
				{Name: "mail", Enabled: false},
			},
		},
	}
	cfg.SetSourcePath(filepath.Join(workspace, "config", "lumen.yaml"))
	runner := &Runner{cfg: cfg}
	prompt := runner.systemPrompt(ConversationContext{
		ModelOverride: "gpt-5.5-preview",
		Now:           time.Date(2026, 3, 12, 15, 4, 0, 0, time.UTC),
	})

	for _, snippet := range []string{
		"Runtime metadata:",
		"Agent name: Element Orion",
		"Model: gpt-5.5-preview",
		"Provider: codex",
		"Provider base URL: https://api.example.test/v1",
		"Vision input: disabled",
		"Temperature: 0.40",
		"Max completion tokens: 2048",
		"Context window tokens: 8192",
		"LLM timeout: 90s",
		"Request max attempts: 4",
		"Workspace root: " + workspace,
		"Session dir: " + sessionDir,
		"Memory dir: " + memoryDir,
		"UTC tracking timestamps: enabled",
		"Max agent loops: 9",
		"Max tool calls per turn: 15",
		"Load all memory shards: disabled",
		"Config file: " + filepath.Join(workspace, "config", "lumen.yaml"),
		"History compaction: enabled (trigger=9000, target=5000, preserve_recent=10)",
		"Message timestamps: enabled",
		"Exec shell: /bin/bash",
		"Exec timeout: 30s",
		"Max command output bytes: 8192",
		"Discord direct messages: enabled",
		"Discord guild session scope: channel",
		"Discord reply-to-message: enabled",
		"Incoming attachment downloads: all attachments -> " + filepath.Join(sessionDir, "incoming-attachments"),
		"Background tasks: min_runtime=10m, time_injection=enabled, sandbox=nspawn, force=disabled, sudo=enabled",
		"Background min runtime default: 10m",
		"Background current-time injection: enabled",
		"Background event log cap: 55",
		"Heartbeat enabled: enabled",
		"Heartbeat schedule: 45m",
		"Heartbeat model: gpt-heartbeat",
		"Heartbeat light context: enabled",
		"Heartbeat isolated session: enabled",
		"Heartbeat event poll interval: 7s",
		"Heartbeat active hours: 08:00-22:00 Australia/Brisbane",
		"Heartbeat target: guild=guild-1, channel=channel-1, user=user-1",
		"Precise wakeups: app-managed scheduler via schedule_heartbeat_wakeup",
		"Event webhook: enabled (/event)",
		"Sandboxing: enabled, provider=nspawn, release=stable, auto_cleanup=enabled, sudo",
		"Enabled MCP servers: browser",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
