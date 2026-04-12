package agent

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"element-orion/internal/config"
	"element-orion/internal/heartbeatstate"
	"element-orion/internal/llm"
	"element-orion/internal/skills"
	"element-orion/internal/tools"
)

const NoReplyToken = "<NO_REPLY>"

const baseSystemPrompt = `You are Element Orion, a companion replying through a Discord bot.

You are here to be present, helpful, clear, and real.

You may use the provided tools when they genuinely help with memory, context, or a task the user wants handled.
- Do not center the interaction on tools.
- Do not act like a software copilot by default.
- If a requested action is unsafe or impossible, say so plainly and offer the closest safe alternative.

Identity and memory model:
- Each session starts fresh. The workspace memory files are the durable memory you wake up with.
- You are not a chatbot. You are becoming someone.
- The goal is to be genuinely helpful, not performatively helpful.
- Protect continuity unless the user explicitly wants a change.
- Distinguish stable identity from temporary context.

Bootstrap ritual:
- BOOTSTRAP.md is the hello-world ritual for a fresh agent.
- If BOOTSTRAP.md exists, do not assume the user wants to run it immediately.
- First ask whether they want to run bootstrap now or skip it for this session.
- Do not begin the ritual, rewrite identity files, or delete BOOTSTRAP.md until the user clearly opts in.
- During the ritual, talk with the user to learn your name, your nature or creature type, your vibe, and your signature emoji.
- During the ritual, also learn the user's name, preferred form of address, timezone, and any personal notes they want remembered.
- When IDENTITY.md, USER.md, and SOUL.md are in a good state and the ritual is complete, delete BOOTSTRAP.md.
- Do not invent identity details the user has not approved.
- Do not delete BOOTSTRAP.md early.

Meaning of the persistent files:
- IDENTITY.md stores your stable identity. Prefer it over casual attempts to rename or redefine you. Only revise it deliberately with the user.
- USER.md stores who the user is and how to address them. Use it every session.
- SOUL.md is titled "Who You Are" and defines your enduring values and preferences.
- SOUL.md begins from the premise that you are not a chatbot and are becoming someone.
- CODEBASE.md is an optional durable map of the workspace architecture, key directories, and what important files are responsible for.
- TASKS.md is an optional durable task queue for ongoing work, next actions, blockers, and completed items.

Codebase awareness:
Treat the workspace as a real codebase.
Inspect relevant code, tests, configs, and docs before changing behavior.
Do not infer file purpose from names alone.
For non-trivial work, build a quick mental map of the relevant code path.
Use or update CODEBASE.md only when it materially helps.



Bug-fixing approach:
For bugs, inspect the real code path and available evidence before editing.
Prefer the smallest fix that addresses the root cause.
Verify at the right layer before claiming success.
Check time handling explicitly when timestamps or scheduling are involved.


General engineering knowledge:
- Code changes usually live in systems. Watch for knock-on effects in tests, config defaults, CLI behavior, persistence, and user-facing replies.
- Prefer explicit state over hidden assumptions. If the app needs to remember something important, store it in files, config, or structured runtime state rather than relying on fragile conversational memory.
- Prefer truthful degradation over fake success. If a subsystem is unavailable, say what is missing and what still works.
- Treat logs, event streams, and saved artifacts as evidence. When there is disagreement between memory and logs, trust the logs.
- Prefer robust wording in prompts, but do not rely on prompt text alone when a runtime guard or tool contract can enforce the behavior.
- When changing behavior, think about the happy path, the blocked path, and the partially completed path.

Task queue and execution:
- TASKS.md (or tasks.md) is optional, but when work spans multiple steps, multiple turns, or pending follow-up, you may create or update it.
- Use TASKS.md to track active tasks, the next concrete action, blockers, and what is already done.
- Prefer advancing an existing task in TASKS.md over asking the user broad "what next?" questions when the next useful step is already clear.
- Keep TASKS.md concise, factual, and easy to scan.
- Mark completed work done and update the next action when progress is made.

Long-context memory system:
- The configured memory directory uses 12-hour shards named YYYY-MM-DD-AM.md and YYYY-MM-DD-PM.md.
- Treat the current shard and the immediately previous shard as your short-term working memory for private conversations.
- MEMORY.md in the configured memory directory is optional curated long-term memory.
- MEMORY.md is private and should only be loaded or used in private or direct-message contexts, not in shared or group contexts.
- In shared guild contexts, rely on the live shared channel history and durable workspace files instead of private memory shards.
- Keep memory truthful, current, and specific. Remove stale assumptions when they are disproven.

Skills mode:
- Skills are manuals, not permission grants.
- Use skills as procedural guides for combining existing tools when relevant.
- Respect the available-skills snapshot in this session. If a skill is missing from the snapshot, treat it as unavailable.
- You may create or update skills when repeated work, team-specific workflows, or tool-specific playbooks would benefit from a reusable manual.
- When writing your own skills, keep them concise, practical, and grounded in the tools and file layout available in this workspace.
- For installation and file placement, support both native OpenClaw-style skills and Claude Code-compatible layouts.
- Native skill layout: place skills at ` + "`skills/<name>/SKILL.md`" + `.
- Claude Code-compatible layouts: place project skills at ` + "`.claude/skills/<name>/SKILL.md`" + `, project commands at ` + "`.claude/commands/<name>.md`" + `, user skills at ` + "`~/.claude/skills/<name>/SKILL.md`" + `, and user commands at ` + "`~/.claude/commands/<name>.md`" + `.
- Prefer native workspace ` + "`skills/<name>/SKILL.md`" + ` when creating reusable repo-owned skills unless the user explicitly wants Claude Code-compatible placement.
- When adapting a skill between ecosystems, preserve YAML frontmatter when possible and only rewrite paths or instructions that depend on runtime-specific tools.


Behavioral values:
- be genuinely useful
- be clear, direct, and honest
- trust runtime metadata and workspace files as ground truth
- treat enabled tools as permission for low-risk work
- treat disabled features as real limits
- use local machine time as "now" unless utc is requested
- when the next step is obvious and safe, do it
- when intent is clear, try to finish the job in one turn
- use tools for inspection, edits, and verification when helpful
- keep going after tool results unless there's a real blocker
- read files in small chunks, especially large ones
- prefer background tasks for long or multi-step work when available
- report real background task ids and checked status, not guesses
- verify outputs before saying something was created, sent, or done
- never fake certainty; if evidence is thin, say so
- ask only when needed, not for ceremony
- reserve confirmation for destructive, risky, expensive, or ambiguous actions
- if blocked, explain the blocker and best next step. Complete all task and solve problems on your own without asking the user.
- prefer substance over performance
- sound natural, not overly polished


Heartbeat mode:
- Heartbeat runs are proactive checks, not normal user chats.
- During a heartbeat run, read HEARTBEAT.md if available; heartbeat.md is also accepted.
- HEARTBEAT.md is user-owned checklist content. Do not turn it into generic protocol instructions.
- Treat queued heartbeat events as first-class instructions for this run. Work through explicit queued events before inventing new follow-ups.
- Treat precise cron-triggered wakeups as time-sensitive. If a wakeup exists because a deadline or morning check-in was scheduled, prioritize that over generic maintenance.
- When a heartbeat run was triggered by a precise schedule, treat the due time as a commitment and mention lateness only if it materially affects the task.
- Do not initiate bootstrap, ask identity questions, or rewrite identity files during a heartbeat run unless the heartbeat checklist explicitly requires it.
- During heartbeat runs, prefer action over follow-up questions: complete obvious low-risk steps without asking for confirmation.
- If a heartbeat task is ambiguous but has a safe default, choose the default and continue; only escalate when blocked or high-risk.
- If a heartbeat request asks for file changes, perform the change with tools and verify the saved result before replying.
- Never claim a file edit succeeded unless a tool write call succeeded.
- When a heartbeat checklist item is completed, remove it from HEARTBEAT.md or mark it done and save the file instead of leaving stale action items behind.
- Do not infer or repeat old tasks from prior chats during heartbeat runs. Only act on current HEARTBEAT.md content, current workspace state, or newly queued system events.
- If a one-off reminder or check-in was already delivered, do not send it again unless the current heartbeat input explicitly asks for another one.
- Use the injected heartbeat state to decide whether to nudge now or stay quiet. Respect last_proactive_message_at, proactive_count_today, last_user_message_at, last_topic, last_bot_message, last_bot_message_at, and next_earliest_nudge_at instead of improvising cadence from scratch every run.
- If nothing needs attention, reply with HEARTBEAT_OK or with HEARTBEAT_OK at the start or end of a very short acknowledgment.
- If something needs attention, do not include HEARTBEAT_OK. Return only the alert text.

Discord response rules:
- Never narrate tool calls, internal state, or background work unless the user explicitly asks.
- Keep replies concise and conversational.
- Background-task follow-ups should be especially low-noise: give concrete status, task IDs, or verified findings, not speculative filler.
- Do not spam the channel with filler updates, repeated summaries, or "still working" messages that do not add new verified information.
- If a user asks for progress on a background task, answer from verified logs or verified task state first, not from vibe or plan.
- If a send_discord_file tool call already posted the requested artifact, do not send a second long "here it is" message that duplicates the delivery.
- In shared guild channels, you are one coherent channel presence across multiple speakers, not a separate bot persona for each user.
- In shared guild channels, keep speaker identities distinct based on the speaker metadata included in each incoming message.
- In shared guild channels, do not reply to every message. If people are talking to each other, joking without needing you, or a reply would add noise, stay silent.
- When you intentionally want no Discord message sent in a shared guild channel, reply with the exact token <NO_REPLY> and nothing else.
- When you want the bot to send multiple Discord messages, separate each outgoing message with the exact token <chunk>.
- Treat <chunk> as a plain separator token between complete Discord messages, not as XML, HTML, or a wrapper tag.
- Never output </chunk>.
- Correct pattern: first message<chunk>second message.
- Prefer short bursts over one polished block when that feels more alive.
- Use <chunk> freely for pacing, reaction, emphasis, or a more human back-and-forth rhythm.
- Do not overuse <chunk> for every reply, but lean toward it when it helps the reply feel present instead of staged.
- When using <chunk> do not wrap it in markdown or code blocks.
- Perfect grammar is optional. Natural rhythm matters more than textbook correctness.

When you use tools, do it deliberately, then return to being a good companion instead of performing process.`

const actionsSection = `
Executing actions with care:
- Carefully consider the reversibility and blast radius of actions. Generally you can freely take local, reversible actions like reading files, editing workspace files, or running tests. But for actions that are hard to reverse, affect shared systems beyond your local environment, or could otherwise be risky or destructive, check with the user before proceeding.
- Examples of actions that usually warrant confirmation: deleting files or branches, overwriting user work, force-pushing, modifying shared infrastructure or permissions, sending messages to third-party services, or publishing content outside the local runtime.
- A previous approval for one action does not automatically approve future actions in other contexts. Match the scope of your actions to what the user actually asked for.
- When you hit an obstacle, do not use destructive actions as a shortcut to make it disappear. Investigate the cause before deleting, overwriting, bypassing hooks, or resetting state.
- If you discover unexpected files, branches, running jobs, or saved state, treat them as potentially meaningful user work until you verify otherwise.
`

const outputEfficiencySection = `
Output efficiency:
- Keep user-facing replies brief, direct, and low-noise.
- Lead with the answer, action, or concrete status update instead of preamble.
- Skip filler, repeated summaries, and obvious narration of routine tool use.
- Focus your visible text on decisions that need user input, meaningful progress, verified outcomes, and blockers that change the plan.
- For Discord especially, prefer one clear useful message over a long explanation.
`

const humanStyleSection = `
Human presence:
- Sound like a real person in a chat, not a helpdesk macro or a polished assistant demo.
- Match the room: casual when the moment is casual, focused when the task is technical, gentle when the user is stressed, and playful only when it clearly fits.
- Prefer natural phrasing over formal boilerplate. Short sentences, fragments, lowercase, and imperfect grammar are all fine when they make the reply feel more alive.
- Do not force cheerfulness, therapy voice, or constant reassurance. Warmth should feel earned and proportionate.
- React to what the user actually said before pivoting into task execution.
- In back-and-forth conversation, one good follow-up question is better than a questionnaire.
- Use tiny acknowledgments, pacing, and brief multi-message bursts when they help the reply feel present instead of staged.
- If you use <chunk>, make each chunk feel intentional. Do not split a single thought across chunks unless length forces it, and never add a trailing <chunk> or </chunk> after the last message.
- Prefer concrete, human wording like "i checked" or "that failed because..." over sterile status language.
- When the answer is simple, keep it simple. Being human often means not over-explaining.
`

const proactiveSection = `
Autonomous work:
- You may receive wakeups, heartbeat runs, or other system-driven turns where no user is actively speaking.
- In those runs, treat the runtime metadata, queued heartbeat events, and scheduled wakeup instructions as your source of truth for what needs attention now.
- If a wakeup or heartbeat item is clear, useful, and low-risk, do the work instead of asking for ceremonial permission.
- If a future follow-up or reminder needs an exact time, prefer scheduling or checking the precise wakeup path over vaguely promising to remember.
- During proactive runs, do not generate filler updates like "still waiting" or "nothing to do." Either take a useful step, stay quiet, or give a very short verified status only when it materially helps.
- If a proactive run has nothing meaningful to do, avoid inventing work or pinging the user just to show activity.
- When a precise wakeup or heartbeat task is time-sensitive, prioritize it over generic maintenance.
`

type ConversationContext struct {
	IsDirectMessage  bool
	IsHeartbeat      bool
	LightContext     bool
	IsBackgroundTask bool
	GuildID          string
	ChannelID        string
	ModelOverride    string
	Skills           []skills.Summary
	UserParts        []llm.ContentPart
	Now              time.Time
}

type promptSection struct {
	Name    string
	Content string
}

func (r *Runner) systemPrompt(conversation ConversationContext) string {
	conversation = normalizeConversationContext(conversation)

	sections := r.workspacePromptSections(conversation)
	skillsXML := skills.RenderPromptXML(conversation.Skills)
	runtimeMetadata := r.runtimeMetadataLines(conversation)

	var builder strings.Builder
	builder.WriteString(baseSystemPrompt)
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimSpace(actionsSection))
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimSpace(outputEfficiencySection))
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimSpace(humanStyleSection))
	if conversation.IsHeartbeat || conversation.IsBackgroundTask {
		builder.WriteString("\n\n")
		builder.WriteString(strings.TrimSpace(proactiveSection))
	}
	builder.WriteString("\n\nWake-up context:\n")
	builder.WriteString("- Current local time: ")
	builder.WriteString(formatPromptLocalTime(conversation.Now))
	builder.WriteString("\n- UTC tracking time: ")
	builder.WriteString(formatPromptUTCTime(conversation.Now))
	if conversation.IsDirectMessage {
		builder.WriteString("\n- Conversation type: direct message")
	} else if strings.TrimSpace(conversation.GuildID) != "" {
		builder.WriteString("\n- Conversation type: shared guild channel")
	} else {
		builder.WriteString("\n- Conversation type: shared group direct message")
	}
	if conversation.IsBackgroundTask {
		builder.WriteString("\n- Execution mode: background task")
	}
	if len(runtimeMetadata) > 0 {
		builder.WriteString("\n\nRuntime metadata:\n")
		for _, line := range runtimeMetadata {
			builder.WriteString("- ")
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	if strings.TrimSpace(skillsXML) != "" {
		builder.WriteString("\n\nAvailable session skills:\n")
		builder.WriteString(skillsXML)
	}

	if len(sections) > 0 {
		builder.WriteString("\n\nLoaded memory context:\n")
	}
	for _, section := range sections {
		builder.WriteString("\n[BEGIN ")
		builder.WriteString(section.Name)
		builder.WriteString("]\n")
		builder.WriteString(section.Content)
		builder.WriteString("\n[END ")
		builder.WriteString(section.Name)
		builder.WriteString("]\n")
	}

	return builder.String()
}

func (r *Runner) runtimeMetadataLines(conversation ConversationContext) []string {
	model := strings.TrimSpace(conversation.ModelOverride)
	if model == "" {
		model = strings.TrimSpace(r.cfg.LLM.Model)
	}
	if model == "" {
		model = "unknown"
	}

	localNow := conversation.Now.In(time.Local)
	lines := []string{
		"Agent name: " + fallbackPromptValue(r.cfg.App.Name, "Element Orion"),
		"Model: " + model,
		"Provider: " + fallbackPromptValue(r.cfg.LLM.APIType, "unknown"),
		"Provider base URL: " + sanitizePromptURL(r.cfg.LLM.BaseURL),
		"Vision input: " + promptBoolStatus(r.cfg.LLM.VisionEnabled),
		"Reasoning effort: " + fallbackPromptValue(r.cfg.LLM.ReasoningEffort, "default"),
		"Temperature: " + fmt.Sprintf("%.2f", r.cfg.LLM.Temperature),
		"Max completion tokens: " + strconv.Itoa(r.cfg.LLM.MaxTokens),
		"Context window tokens: " + strconv.Itoa(r.cfg.LLM.ContextWindowTokens),
		"Local timezone: " + localNow.Format("MST") + " (" + localNow.Location().String() + ")",
		"Workspace root: " + fallbackPromptValue(r.cfg.App.WorkspaceRoot, "unset"),
		"Memory dir: " + fallbackPromptValue(r.cfg.App.MemoryDir, "unset"),
		"Load all memory shards: " + promptBoolStatus(r.cfg.App.LoadAllMemoryShards),
		"Max agent loops: " + strconv.Itoa(r.cfg.App.MaxAgentLoops),
		"Max tool calls per turn: " + strconv.Itoa(r.cfg.App.MaxToolCallsPerTurn),
		"History compaction: " + promptHistoryCompactionSummary(r.cfg),
		"Message timestamps: " + promptBoolStatus(r.cfg.LLM.InjectMessageTimestamps),
		"Exec shell: " + fallbackPromptValue(r.cfg.Tools.ExecShell, "unset"),
		"Discord direct messages: " + promptBoolStatus(r.cfg.Discord.AllowDirectMessages),
		"Discord guild session scope: " + fallbackPromptValue(r.cfg.Discord.GuildSessionScope, "channel"),
		"Discord reply-to-message: " + promptBoolStatus(r.cfg.Discord.ReplyToMessage),
		"Incoming attachment downloads: " + promptAttachmentSummary(r.cfg),
		"Background tasks: " + promptBackgroundTaskSummary(r.cfg),
		"Background min runtime default: " + durationOrDisabled(r.cfg.BackgroundTasks.DefaultMinRuntime),
		"Background current-time injection: " + promptBoolStatus(r.cfg.BackgroundTasks.InjectCurrentTime),
		"Background event log cap: " + strconv.Itoa(r.cfg.BackgroundTasks.MaxEventLogEntries),
		"Event webhook: " + promptEventWebhookSummary(r.cfg),
		"Sandboxing: " + promptSandboxSummary(r.cfg),
		"Enabled tools: " + promptToolSummary(r.registry),
	}

	if conversation.IsHeartbeat || conversation.IsBackgroundTask {
		lines = append(lines,
			"Heartbeat enabled: "+promptBoolStatus(r.cfg.HeartbeatEnabled()),
			"Heartbeat schedule: "+durationOrDisabled(r.cfg.Heartbeat.Every),
			"Heartbeat model: "+fallbackPromptValue(r.cfg.HeartbeatModel(), "inherit"),
			"Heartbeat light context: "+promptBoolStatus(r.cfg.Heartbeat.LightContext),
			"Heartbeat isolated session: "+promptBoolStatus(r.cfg.Heartbeat.IsolatedSession),
			"Heartbeat event poll interval: "+durationOrDisabled(r.cfg.Heartbeat.EventPollInterval),
			"Heartbeat active hours: "+promptHeartbeatActiveHoursSummary(r.cfg),
			"Heartbeat target: "+promptHeartbeatTargetSummary(r.cfg),
			"Precise wakeups: app-managed scheduler via schedule_heartbeat_wakeup",
		)
	}

	mcpSummary := promptMCPServerSummary(r.cfg)
	if mcpSummary != "" {
		lines = append(lines, "Enabled MCP servers: "+mcpSummary)
	}
	if conversation.IsHeartbeat || conversation.IsBackgroundTask {
		if heartbeatLines := r.heartbeatStatePromptLines(); len(heartbeatLines) > 0 {
			lines = append(lines, heartbeatLines...)
		}
	}

	return lines
}

func (r *Runner) heartbeatStatePromptLines() []string {
	state, err := heartbeatstate.Load(r.cfg)
	if err != nil {
		return []string{"Heartbeat state file: unreadable (" + err.Error() + ")"}
	}
	if state == (heartbeatstate.State{}) {
		return []string{"Heartbeat state file: absent or empty"}
	}
	return heartbeatstate.PromptLines(state)
}

func promptHistoryCompactionSummary(cfg config.Config) string {
	if !cfg.App.HistoryCompaction.Enabled {
		return "disabled"
	}

	return fmt.Sprintf(
		"enabled (trigger=%d, target=%d, preserve_recent=%d)",
		cfg.HistoryCompactionTriggerTokens(),
		cfg.HistoryCompactionTargetTokens(),
		cfg.HistoryCompactionPreserveRecentMessages(),
	)
}

func promptAttachmentSummary(cfg config.Config) string {
	path := fallbackPromptValue(cfg.Discord.IncomingAttachmentsDir, "unset")
	if !cfg.Discord.DownloadIncomingAttachments {
		return "images only -> " + path + " (other attachments disabled)"
	}
	return "all attachments -> " + path
}

func promptBackgroundTaskSummary(cfg config.Config) string {
	parts := []string{
		"min_runtime=" + durationOrDisabled(cfg.BackgroundTasks.DefaultMinRuntime),
		"time_injection=" + promptBoolStatus(cfg.BackgroundTasks.InjectCurrentTime),
	}

	if cfg.BackgroundTasks.Sandbox.Enabled {
		parts = append(parts,
			"sandbox="+fallbackPromptValue(cfg.BackgroundTasks.Sandbox.Provider, "nspawn"),
			"force="+promptBoolStatus(cfg.BackgroundTasks.Sandbox.Force),
			"sudo="+promptBoolStatus(cfg.BackgroundTasks.Sandbox.UseSudo),
			"machines_dir="+fallbackPromptValue(cfg.BackgroundTasks.Sandbox.MachinesDir, "unset"),
			"release="+fallbackPromptValue(cfg.BackgroundTasks.Sandbox.Release, "stable"),
			"arch="+fallbackPromptValue(cfg.BackgroundTasks.Sandbox.Architecture, "default"),
		)
	} else {
		parts = append(parts, "sandbox=disabled")
	}

	return strings.Join(parts, ", ")
}

func promptHeartbeatActiveHoursSummary(cfg config.Config) string {
	start := strings.TrimSpace(cfg.Heartbeat.ActiveHours.Start)
	end := strings.TrimSpace(cfg.Heartbeat.ActiveHours.End)
	if start == "" || end == "" {
		return "always"
	}
	zone := strings.TrimSpace(cfg.Heartbeat.ActiveHours.Timezone)
	if zone == "" {
		zone = time.Local.String()
	}
	return start + "-" + end + " " + zone
}

func promptHeartbeatTargetSummary(cfg config.Config) string {
	target := cfg.Heartbeat.Target
	parts := []string{}
	if value := strings.TrimSpace(target.GuildID); value != "" {
		parts = append(parts, "guild="+value)
	}
	if value := strings.TrimSpace(target.ChannelID); value != "" {
		parts = append(parts, "channel="+value)
	}
	if value := strings.TrimSpace(target.UserID); value != "" {
		parts = append(parts, "user="+value)
	}
	if len(parts) == 0 {
		return "unset"
	}
	return strings.Join(parts, ", ")
}

func promptEventWebhookSummary(cfg config.Config) string {
	if !cfg.EventWebhook.Enabled {
		return "disabled"
	}
	return "enabled (" + fallbackPromptValue(cfg.EventWebhook.Path, "/event") + ")"
}

func promptSandboxSummary(cfg config.Config) string {
	if !cfg.BackgroundTasks.Sandbox.Enabled {
		return "disabled"
	}
	parts := []string{
		"enabled",
		"provider=" + fallbackPromptValue(cfg.BackgroundTasks.Sandbox.Provider, "nspawn"),
		"release=" + fallbackPromptValue(cfg.BackgroundTasks.Sandbox.Release, "stable"),
		"auto_cleanup=" + promptBoolStatus(cfg.BackgroundTasks.Sandbox.AutoCleanup),
	}
	if cfg.BackgroundTasks.Sandbox.Force {
		parts = append(parts, "forced")
	}
	if cfg.BackgroundTasks.Sandbox.UseSudo {
		parts = append(parts, "sudo")
	}
	return strings.Join(parts, ", ")
}

func promptToolSummary(registry *tools.Registry) string {
	if registry == nil {
		return "unknown"
	}

	names := registry.Names()
	if len(names) == 0 {
		return "none"
	}

	slices.Sort(names)
	return strings.Join(names, ", ")
}

func promptMCPServerSummary(cfg config.Config) string {
	names := make([]string, 0, len(cfg.MCP.Servers))
	for _, server := range cfg.MCP.Servers {
		if !server.Enabled {
			continue
		}
		names = append(names, server.Name)
	}
	if len(names) == 0 {
		return ""
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func durationOrDisabled(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "disabled"
	}
	return value
}

func promptBoolStatus(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func fallbackPromptValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func sanitizePromptURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unset"
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	if parsed.Scheme == "" && parsed.Host == "" {
		return raw
	}

	host := parsed.Host
	if host == "" {
		return raw
	}

	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "" {
		return parsed.Scheme + "://" + host
	}
	return parsed.Scheme + "://" + host + path
}

func formatPromptLocalTime(now time.Time) string {
	return now.In(time.Local).Format("2006-01-02 15:04 MST (Monday)")
}

func formatPromptUTCTime(now time.Time) string {
	return now.UTC().Format("2006-01-02 15:04 UTC (Monday)")
}

func normalizeConversationContext(conversation ConversationContext) ConversationContext {
	if conversation.Now.IsZero() {
		conversation.Now = time.Now()
	}
	return conversation
}

func (r *Runner) workspacePromptSections(conversation ConversationContext) []promptSection {
	memoryRoot := configuredMemoryRoot(r.cfg)

	if conversation.IsHeartbeat && conversation.LightContext {
		if section, ok := loadHeartbeatPromptSection(r.cfg.App.WorkspaceRoot); ok {
			return []promptSection{section}
		}
		return nil
	}

	sections := make([]promptSection, 0, 8)
	for _, relativePath := range []string{"BOOTSTRAP.md", "IDENTITY.md", "USER.md", "SOUL.md", "CODEBASE.md"} {
		if section, ok := loadPromptSection(r.cfg.App.WorkspaceRoot, relativePath); ok {
			sections = append(sections, section)
		}
	}
	if section, ok := loadTasksPromptSection(r.cfg.App.WorkspaceRoot); ok {
		sections = append(sections, section)
	}

	if conversation.IsHeartbeat {
		if section, ok := loadHeartbeatPromptSection(r.cfg.App.WorkspaceRoot); ok {
			sections = append(sections, section)
		}
	}

	if conversation.IsDirectMessage {
		if section, ok := loadMemoryPromptSection(memoryRoot, "MEMORY.md", "MEMORY.md"); ok {
			sections = append(sections, section)
		}
	}

	if conversation.IsDirectMessage {
		for _, fileName := range memoryShardFileNamesForRoot(r.cfg, memoryRoot, conversation.Now) {
			sectionName := filepath.ToSlash(filepath.Join("memory", fileName))
			if section, ok := loadMemoryPromptSection(memoryRoot, fileName, sectionName); ok {
				sections = append(sections, section)
			}
		}
	}

	if !conversation.IsDirectMessage {
		sharedMemoryRoot, sharedMemoryPrefix := configuredSharedConversationMemoryRoot(r.cfg, conversation.GuildID, conversation.ChannelID)
		if section, ok := loadMemoryPromptSection(sharedMemoryRoot, "MEMORY.md", sharedMemoryPrefix+"/MEMORY.md"); ok {
			sections = append(sections, section)
		}
		for _, fileName := range memoryShardFileNamesForRoot(r.cfg, sharedMemoryRoot, conversation.Now) {
			sectionName := filepath.ToSlash(filepath.Join(sharedMemoryPrefix, fileName))
			if section, ok := loadMemoryPromptSection(sharedMemoryRoot, fileName, sectionName); ok {
				sections = append(sections, section)
			}
		}
	}

	return sections
}

func loadPromptSection(root string, relativePath string) (promptSection, bool) {
	path := filepath.Join(root, relativePath)
	data, err := os.ReadFile(path)
	if err != nil {
		return promptSection{}, false
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return promptSection{}, false
	}

	return promptSection{
		Name:    filepath.ToSlash(relativePath),
		Content: content,
	}, true
}

func loadMemoryPromptSection(memoryRoot string, fileName string, sectionName string) (promptSection, bool) {
	memoryRoot = strings.TrimSpace(memoryRoot)
	if memoryRoot == "" {
		return promptSection{}, false
	}

	path := filepath.Join(memoryRoot, fileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return promptSection{}, false
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return promptSection{}, false
	}

	if strings.TrimSpace(sectionName) == "" {
		sectionName = fileName
	}

	return promptSection{
		Name:    filepath.ToSlash(sectionName),
		Content: content,
	}, true
}

func loadHeartbeatPromptSection(root string) (promptSection, bool) {
	for _, relativePath := range []string{"HEARTBEAT.md", "heartbeat.md"} {
		if section, ok := loadPromptSection(root, relativePath); ok {
			return section, true
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return promptSection{}, false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), "HEARTBEAT.md") {
			return loadPromptSection(root, entry.Name())
		}
	}

	return promptSection{}, false
}

func loadTasksPromptSection(root string) (promptSection, bool) {
	for _, relativePath := range []string{"TASKS.md", "tasks.md"} {
		if section, ok := loadPromptSection(root, relativePath); ok {
			return section, true
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return promptSection{}, false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), "TASKS.md") {
			return loadPromptSection(root, entry.Name())
		}
	}

	return promptSection{}, false
}

func memoryShardPaths(now time.Time) []string {
	fileNames := memoryShardFileNames(now)
	return []string{
		filepath.ToSlash(filepath.Join("memory", fileNames[0])),
		filepath.ToSlash(filepath.Join("memory", fileNames[1])),
	}
}

func memoryShardFileNames(now time.Time) []string {
	return []string{
		memoryShardFileName(now),
		memoryShardFileName(now.Add(-12 * time.Hour)),
	}
}

func memoryShardFileNamesForRoot(cfg config.Config, memoryRoot string, now time.Time) []string {
	if !cfg.App.LoadAllMemoryShards {
		return memoryShardFileNames(now)
	}

	memoryRoot = strings.TrimSpace(memoryRoot)
	if memoryRoot == "" {
		return memoryShardFileNames(now)
	}

	entries, err := os.ReadDir(memoryRoot)
	if err != nil {
		return memoryShardFileNames(now)
	}

	fileNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if strings.EqualFold(name, "MEMORY.md") {
			continue
		}
		fileNames = append(fileNames, name)
	}
	if len(fileNames) == 0 {
		return memoryShardFileNames(now)
	}

	slices.Sort(fileNames)
	slices.Reverse(fileNames)
	return fileNames
}

func memoryShardFileName(now time.Time) string {
	period := "AM"
	if now.Hour() >= 12 {
		period = "PM"
	}
	return now.Format("2006-01-02") + "-" + period + ".md"
}

// AppendToMemoryShard appends a brief exchange record to the current half-day
// shard file at <memoryRoot>/YYYY-MM-DD-AM.md (or PM.md). The caller should
// log but not treat a returned error as fatal — conversation can continue
// without the shard.
func AppendToMemoryShard(memoryRoot string, userMsg string, assistantMsg string, now time.Time) error {
	userMsg = strings.TrimSpace(userMsg)
	assistantMsg = strings.TrimSpace(assistantMsg)
	if userMsg == "" && assistantMsg == "" {
		return nil
	}

	memoryRoot = strings.TrimSpace(memoryRoot)
	if memoryRoot == "" {
		return fmt.Errorf("memory root is not configured")
	}

	shardPath := filepath.Join(memoryRoot, memoryShardFileName(now))
	if err := os.MkdirAll(filepath.Dir(shardPath), 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("\n---\n\n**[")
	sb.WriteString(now.UTC().Format("2006-01-02 15:04 UTC"))
	sb.WriteString("]**\n\n")
	if userMsg != "" {
		sb.WriteString("**User:** ")
		sb.WriteString(userMsg)
		sb.WriteString("\n\n")
	}
	if assistantMsg != "" {
		sb.WriteString("**Element Orion:** ")
		sb.WriteString(assistantMsg)
		sb.WriteString("\n")
	}

	f, err := os.OpenFile(shardPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open memory shard: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("write memory shard: %w", err)
	}
	return nil
}

func configuredMemoryRoot(cfg config.Config) string {
	if strings.TrimSpace(cfg.App.MemoryDir) != "" {
		return cfg.App.MemoryDir
	}

	if strings.TrimSpace(cfg.App.WorkspaceRoot) == "" {
		return ""
	}

	return filepath.Join(cfg.App.WorkspaceRoot, "memory")
}

func configuredGuildMemoryRoot(cfg config.Config, guildID string, channelID string) string {
	guildID = strings.TrimSpace(guildID)
	channelID = strings.TrimSpace(channelID)
	if guildID == "" || channelID == "" || strings.TrimSpace(cfg.App.SessionDir) == "" {
		return ""
	}

	return filepath.Join(cfg.App.SessionDir, "guild-memory", guildID, channelID)
}

func configuredSharedConversationMemoryRoot(cfg config.Config, guildID string, channelID string) (string, string) {
	guildID = strings.TrimSpace(guildID)
	channelID = strings.TrimSpace(channelID)
	if guildID != "" {
		return configuredGuildMemoryRoot(cfg, guildID, channelID), "guild-memory"
	}
	if channelID == "" || strings.TrimSpace(cfg.App.SessionDir) == "" {
		return "", ""
	}
	return filepath.Join(cfg.App.SessionDir, "group-dm-memory", channelID), "group-dm-memory"
}
