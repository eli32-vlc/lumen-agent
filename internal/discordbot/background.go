package discordbot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"element-orion/internal/agent"
	"element-orion/internal/llm"
	"element-orion/internal/skills"
	"element-orion/internal/tools"
)

type backgroundTaskStatus string

const (
	backgroundTaskQueued    backgroundTaskStatus = "queued"
	backgroundTaskRunning   backgroundTaskStatus = "running"
	backgroundTaskCompleted backgroundTaskStatus = "completed"
	backgroundTaskFailed    backgroundTaskStatus = "failed"
	backgroundTaskCanceled  backgroundTaskStatus = "canceled"
)

type backgroundTask struct {
	ID               string
	Prompt           string
	GuildID          string
	ChannelID        string
	UserID           string
	Status           backgroundTaskStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
	StartedAt        time.Time
	CompletedAt      time.Time
	Error            string
	Result           string
	History          []llm.Message
	Skills           []skills.Summary
	Cancel           context.CancelFunc
	ModelOverride    string
	LightContext     bool
	MinRuntime       time.Duration
	Events           []tools.BackgroundTaskEvent
	Sandbox          *tools.BackgroundTaskSandboxInfo
	SandboxRequested bool
	SpawnMessages    int
	SpawnTokens      int
}

func (s *Service) StartBackgroundTask(ctx context.Context, options tools.BackgroundTaskStartOptions) (tools.BackgroundTaskInfo, error) {
	metadata, _ := tools.DiscordToolContextFromContext(ctx)
	if strings.TrimSpace(options.Prompt) == "" {
		return tools.BackgroundTaskInfo{}, fmt.Errorf("prompt must not be empty")
	}
	if strings.TrimSpace(metadata.ChannelID) == "" {
		return tools.BackgroundTaskInfo{}, fmt.Errorf("background tasks require an active Discord channel context")
	}
	task, err := s.startBackgroundTask(ctx, metadata.GuildID, metadata.ChannelID, metadata.UserID, options)
	if err != nil {
		return tools.BackgroundTaskInfo{}, err
	}
	return task.info(), nil
}

func (s *Service) startBackgroundTask(parent context.Context, guildID string, channelID string, userID string, options tools.BackgroundTaskStartOptions) (*backgroundTask, error) {
	if parent == nil {
		parent = s.currentContext()
	}
	if parent == nil {
		parent = context.Background()
	}

	now := time.Now().UTC()
	ctx, cancel := context.WithCancel(parent)

	key := s.sessionKey(guildID, channelID, userID)
	session := s.lookupSession(key)
	history := []llm.Message(nil)
	skillSnapshot := s.runner.SnapshotSkills()
	if session != nil {
		history, skillSnapshot = session.snapshotForRun()
	}
	if len(options.History) > 0 {
		history = append([]llm.Message(nil), options.History...)
	}
	minRuntime := options.MinRuntime
	if minRuntime <= 0 {
		minRuntime = s.cfg.BackgroundTaskDefaultMinRuntime()
	}

	task := &backgroundTask{
		ID:               newSessionID(now),
		Prompt:           strings.TrimSpace(options.Prompt),
		GuildID:          guildID,
		ChannelID:        channelID,
		UserID:           userID,
		Status:           backgroundTaskQueued,
		CreatedAt:        now,
		UpdatedAt:        now,
		History:          history,
		Skills:           skillSnapshot,
		Cancel:           cancel,
		ModelOverride:    strings.TrimSpace(options.ModelOverride),
		LightContext:     options.LightContext,
		MinRuntime:       minRuntime,
		SandboxRequested: options.Sandbox,
		SpawnMessages:    len(history),
		SpawnTokens:      agent.EstimateHistoryTokens(history),
	}

	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	go s.runBackgroundTask(ctx, task)
	return task, nil
}

func (s *Service) runBackgroundTask(ctx context.Context, task *backgroundTask) {
	s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
		task.Status = backgroundTaskRunning
		task.StartedAt = time.Now().UTC()
		task.UpdatedAt = task.StartedAt
	})

	runCtx := tools.WithDiscordToolContext(ctx, tools.DiscordToolContext{
		GuildID:   task.GuildID,
		ChannelID: task.ChannelID,
		UserID:    task.UserID,
	})
	runCtx = tools.WithBackgroundTaskContext(runCtx)

	sandboxActive := task.SandboxRequested || s.cfg.BackgroundTasks.Sandbox.Force
	if sandboxActive {
		if !s.cfg.BackgroundTasks.Sandbox.Enabled || s.sandboxes == nil {
			err := fmt.Errorf("background-task sandboxing was requested but no sandbox manager is configured")
			s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
				task.Status = backgroundTaskFailed
				task.CompletedAt = time.Now().UTC()
				task.UpdatedAt = task.CompletedAt
				task.Error = err.Error()
				task.Cancel = nil
			})
			_ = s.enqueueBackgroundTaskUpdate(task, "failed", "", err)
			return
		}

		sandboxName := "bg-" + strings.ToLower(task.ID)
		info, err := s.sandboxes.CreateSandbox(ctx, tools.SandboxCreateOptions{
			Name:      sandboxName,
			AutoStart: true,
		})
		if err != nil {
			s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
				task.Status = backgroundTaskFailed
				task.CompletedAt = time.Now().UTC()
				task.UpdatedAt = task.CompletedAt
				task.Error = err.Error()
				task.Cancel = nil
			})
			_ = s.enqueueBackgroundTaskUpdate(task, "failed", "", err)
			return
		}
		runCtx = tools.WithSandboxExecutionContext(runCtx, tools.SandboxExecutionContext{Name: info.Name})
		s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
			copyInfo := info
			task.Sandbox = &copyInfo
		})
		defer s.cleanupBackgroundTaskSandbox(task)
	}

	history := append([]llm.Message(nil), task.History...)
	prompt := task.Prompt
	previousHistoryLen := len(history)
	var updated []llm.Message
	var err error

	for {
		updated, err = s.runner.Run(runCtx, history, prompt, agent.ConversationContext{
			IsDirectMessage:  task.GuildID == "",
			IsBackgroundTask: true,
			LightContext:     task.LightContext,
			ModelOverride:    task.ModelOverride,
			Skills:           append([]skills.Summary(nil), task.Skills...),
			Now:              time.Now(),
		}, func(event agent.Event) {
			s.logBackgroundTaskEvent(task.ID, event)
		})
		if err != nil || errorsIsCanceled(ctx.Err()) {
			break
		}
		s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
			task.History = agent.CompactHistoryForStorage(s.cfg, updated)
			task.UpdatedAt = time.Now().UTC()
		})

		if task.MinRuntime <= 0 {
			break
		}
		elapsed := time.Since(task.StartedAt)
		if elapsed >= task.MinRuntime {
			break
		}

		history = updated
		previousHistoryLen = len(history)
		prompt = backgroundTaskContinuationPrompt(task.MinRuntime, elapsed)
		s.logBackgroundTaskEvent(task.ID, agent.Event{
			Kind:       agent.EventStatus,
			Message:    "Minimum runtime not reached; continuing",
			Detail:     prompt,
			FullDetail: prompt,
			Time:       time.Now().UTC(),
		})
	}

	if errorsIsCanceled(ctx.Err()) {
		s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
			task.Status = backgroundTaskCanceled
			task.CompletedAt = time.Now().UTC()
			task.UpdatedAt = task.CompletedAt
			task.Error = "canceled"
			task.Cancel = nil
		})
		return
	}

	if err != nil {
		s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
			task.Status = backgroundTaskFailed
			task.CompletedAt = time.Now().UTC()
			task.UpdatedAt = task.CompletedAt
			task.Error = strings.TrimSpace(err.Error())
			task.Cancel = nil
		})
		_ = s.enqueueBackgroundTaskUpdate(task, "failed", "", err)
		return
	}

	reply, silent := turnAssistantReply(updated, previousHistoryLen)
	if silent {
		reply = ""
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = "Finished."
	}

	s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
		task.Status = backgroundTaskCompleted
		task.CompletedAt = time.Now().UTC()
		task.UpdatedAt = task.CompletedAt
		task.Result = reply
		task.History = agent.CompactHistoryForStorage(s.cfg, updated)
		task.Cancel = nil
	})

	_ = s.enqueueBackgroundTaskUpdate(task, "finished", reply, nil)
}

func (s *Service) logBackgroundTaskEvent(taskID string, event agent.Event) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	s.recordRuntimeEvent(event)

	s.updateBackgroundTask(taskID, func(task *backgroundTask) {
		task.Events = append(task.Events, tools.BackgroundTaskEvent{
			Kind:       string(event.Kind),
			Message:    event.Message,
			ToolName:   event.ToolName,
			Detail:     event.Detail,
			FullDetail: event.FullDetail,
			Time:       event.Time,
		})
		limit := s.cfg.BackgroundTaskMaxEventLogEntries()
		if limit > 0 && len(task.Events) > limit {
			task.Events = append([]tools.BackgroundTaskEvent(nil), task.Events[len(task.Events)-limit:]...)
		}
		task.UpdatedAt = time.Now().UTC()
	})

	switch event.Kind {
	case agent.EventToolStarted:
		s.audit.Write("background_tool_start", taskID, map[string]any{"tool": event.ToolName, "detail": event.Detail, "full_detail": event.FullDetail})
	case agent.EventToolFinished:
		s.audit.Write("background_tool_done", taskID, map[string]any{"tool": event.ToolName, "detail": event.Detail, "full_detail": event.FullDetail, "duration_ms": event.DurationMS, "success": event.Success})
	case agent.EventModelDone:
		data := map[string]any{"duration_ms": event.DurationMS, "tokens": event.TokenCount}
		for key, value := range event.Data {
			data[key] = value
		}
		s.audit.Write("background_model_done", taskID, data)
	case agent.EventStatus:
		data := map[string]any{"message": event.Message, "detail": event.Detail, "full_detail": event.FullDetail}
		for key, value := range event.Data {
			data[key] = value
		}
		s.audit.Write("background_status", taskID, data)
	case agent.EventAssistant:
		s.audit.Write("background_assistant", taskID, map[string]any{"message": event.Message})
	}
}

func (s *Service) listBackgroundTasks(channelID string) []*backgroundTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*backgroundTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		if strings.TrimSpace(task.ChannelID) != strings.TrimSpace(channelID) {
			continue
		}
		copyTask := *task
		tasks = append(tasks, &copyTask)
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	if len(tasks) > 10 {
		tasks = tasks[:10]
	}
	return tasks
}

func (s *Service) ListBackgroundTasks(ctx context.Context, status string, limit int) ([]tools.BackgroundTaskInfo, error) {
	metadata, _ := tools.DiscordToolContextFromContext(ctx)
	items := s.listBackgroundTasks(metadata.ChannelID)
	result := make([]tools.BackgroundTaskInfo, 0, len(items))
	for _, task := range items {
		if status != "" && string(task.Status) != status {
			continue
		}
		result = append(result, task.info())
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *Service) lookupBackgroundTask(id string) *backgroundTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task := s.tasks[strings.TrimSpace(id)]
	if task == nil {
		return nil
	}
	copyTask := *task
	return &copyTask
}

func (s *Service) GetBackgroundTask(ctx context.Context, id string) (tools.BackgroundTaskInfo, error) {
	metadata, _ := tools.DiscordToolContextFromContext(ctx)
	task := s.lookupBackgroundTask(id)
	if task == nil || (metadata.ChannelID != "" && strings.TrimSpace(task.ChannelID) != strings.TrimSpace(metadata.ChannelID)) {
		return tools.BackgroundTaskInfo{}, fmt.Errorf("background task %q was not found", strings.TrimSpace(id))
	}
	return task.info(), nil
}

func (s *Service) GetBackgroundTaskLogs(ctx context.Context, id string, limit int) ([]tools.BackgroundTaskEvent, error) {
	metadata, _ := tools.DiscordToolContextFromContext(ctx)
	task := s.lookupBackgroundTask(id)
	if task == nil || (metadata.ChannelID != "" && strings.TrimSpace(task.ChannelID) != strings.TrimSpace(metadata.ChannelID)) {
		return nil, fmt.Errorf("background task %q was not found", strings.TrimSpace(id))
	}
	events := append([]tools.BackgroundTaskEvent(nil), task.Events...)
	if limit > 0 && len(events) > limit {
		events = append([]tools.BackgroundTaskEvent(nil), events[len(events)-limit:]...)
	}
	return events, nil
}

func (s *Service) updateBackgroundTask(id string, mutate func(*backgroundTask)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task := s.tasks[strings.TrimSpace(id)]
	if task == nil {
		return
	}
	mutate(task)
}

func (s *Service) cancelAllBackgroundTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range s.tasks {
		if task.Cancel != nil {
			task.Cancel()
		}
	}
}

func (s *Service) CancelBackgroundTask(ctx context.Context, id string) (tools.BackgroundTaskInfo, error) {
	metadata, _ := tools.DiscordToolContextFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()

	task := s.tasks[strings.TrimSpace(id)]
	if task == nil || (metadata.ChannelID != "" && strings.TrimSpace(task.ChannelID) != strings.TrimSpace(metadata.ChannelID)) {
		return tools.BackgroundTaskInfo{}, fmt.Errorf("background task %q was not found", strings.TrimSpace(id))
	}
	if task.Cancel != nil {
		task.Cancel()
	}
	task.Status = backgroundTaskCanceled
	task.Error = "canceled"
	task.UpdatedAt = time.Now().UTC()
	task.CompletedAt = task.UpdatedAt
	task.Cancel = nil
	return task.info(), nil
}

func describeBackgroundTask(task *backgroundTask) string {
	if task == nil {
		return "Task not found."
	}

	var builder strings.Builder
	builder.WriteString("Background task `")
	builder.WriteString(task.ID)
	builder.WriteString("`\n")
	builder.WriteString("Status: ")
	builder.WriteString(string(task.Status))
	builder.WriteString("\nPrompt: ")
	builder.WriteString(compactBackgroundTaskText(task.Prompt, 160))
	if task.Result != "" {
		builder.WriteString("\nResult: ")
		builder.WriteString(task.Result)
	}
	if task.Error != "" {
		builder.WriteString("\nError: ")
		builder.WriteString(compactBackgroundTaskText(task.Error, 220))
	}
	return builder.String()
}

func compactBackgroundTaskText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func errorsIsCanceled(err error) bool {
	return err == context.Canceled
}

func (t *backgroundTask) info() tools.BackgroundTaskInfo {
	if t == nil {
		return tools.BackgroundTaskInfo{}
	}
	return tools.BackgroundTaskInfo{
		ID:                t.ID,
		Status:            string(t.Status),
		Prompt:            t.Prompt,
		Result:            t.Result,
		Error:             t.Error,
		CreatedAt:         t.CreatedAt,
		UpdatedAt:         t.UpdatedAt,
		StartedAt:         t.StartedAt,
		CompletedAt:       t.CompletedAt,
		MinRuntimeSeconds: int64(t.MinRuntime / time.Second),
		Sandbox:           t.Sandbox,
	}
}

func (s *Service) cleanupBackgroundTaskSandbox(task *backgroundTask) {
	if s == nil || task == nil || task.Sandbox == nil || s.sandboxes == nil {
		return
	}
	name := strings.TrimSpace(task.Sandbox.Name)
	if name == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var info tools.BackgroundTaskSandboxInfo
	var err error
	if s.cfg.BackgroundTasks.Sandbox.AutoCleanup {
		info, err = s.sandboxes.DeleteSandbox(ctx, name, true)
	} else {
		info, err = s.sandboxes.StopSandbox(ctx, name, true)
	}
	if err != nil {
		s.audit.Write("background_sandbox_cleanup_error", task.ID, map[string]any{"name": name, "error": err.Error()})
		return
	}
	s.updateBackgroundTask(task.ID, func(task *backgroundTask) {
		copyInfo := info
		task.Sandbox = &copyInfo
		task.UpdatedAt = time.Now().UTC()
	})
}

func backgroundTaskContinuationPrompt(target time.Duration, elapsed time.Duration) string {
	target = target.Round(time.Second)
	elapsed = elapsed.Round(time.Second)
	remaining := target - elapsed
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("System continuation: the minimum background-task runtime is %s and only %s has elapsed so far. Continue working, deepen the investigation, verify more thoroughly, and only finish early if you are genuinely blocked. Remaining target time: about %s.", target, elapsed, remaining.Round(time.Second))
}

func (s *Service) enqueueBackgroundTaskUpdate(task *backgroundTask, outcome string, reply string, runErr error) error {
	if s == nil || task == nil {
		return nil
	}

	key := s.sessionKey(task.GuildID, task.ChannelID, task.UserID)
	session := s.lookupSession(key)
	if session == nil {
		var err error
		session, _, err = s.resetSession(key)
		if err != nil {
			return err
		}
	}

	prompt := inboundPrompt{
		Kind:         promptKindBackground,
		Content:      backgroundTaskUpdatePrompt(task, outcome, reply, runErr),
		GuildID:      task.GuildID,
		ChannelID:    task.ChannelID,
		LightContext: true,
	}

	select {
	case <-session.Context.Done():
		return context.Canceled
	case session.Queue <- prompt:
		return nil
	default:
		return fmt.Errorf("session queue is full")
	}
}

func backgroundTaskUpdatePrompt(task *backgroundTask, outcome string, reply string, runErr error) string {
	var builder strings.Builder
	builder.WriteString("Internal system event for the dom agent. ")
	builder.WriteString("A background worker finished its run and you should handle the user-facing follow-up yourself. ")
	builder.WriteString("The worker does not speak directly to the user; you do. ")
	builder.WriteString("Update your understanding using the handoff below and send a normal human reply to the user. ")
	builder.WriteString("Avoid boilerplate runtime phrasing like raw session IDs unless they are genuinely useful.\n\n")
	builder.WriteString("If the requested artifact was already sent with a Discord tool during the worker run, do not send a second duplicate delivery message. ")
	builder.WriteString("If you mention progress or completion, keep it tied to verified evidence from the handoff.\n\n")
	builder.WriteString("Background worker status: ")
	builder.WriteString(strings.TrimSpace(outcome))
	builder.WriteString("\n")
	builder.WriteString("Worker task id: ")
	builder.WriteString(strings.TrimSpace(task.ID))
	builder.WriteString("\n")
	builder.WriteString("Original worker prompt: ")
	builder.WriteString(compactBackgroundTaskText(task.Prompt, 800))
	builder.WriteString("\n")
	builder.WriteString("Worker inherited chat snapshot: ")
	builder.WriteString(fmt.Sprintf("%d messages (~%d tokens)\n", task.SpawnMessages, task.SpawnTokens))
	builder.WriteString("Worker final context size: ")
	builder.WriteString(fmt.Sprintf("%d messages (~%d tokens)\n", len(task.History), agent.EstimateHistoryTokens(task.History)))
	if strings.TrimSpace(reply) != "" {
		builder.WriteString("Worker final reply:\n")
		builder.WriteString(strings.TrimSpace(reply))
		builder.WriteString("\n")
	}
	if runErr != nil {
		builder.WriteString("Worker error:\n")
		builder.WriteString(strings.TrimSpace(runErr.Error()))
		builder.WriteString("\n")
	}
	builder.WriteString("Important: the worker's full internal context does not automatically merge into the main chat. ")
	builder.WriteString("Use this handoff to continue naturally.")
	return builder.String()
}
