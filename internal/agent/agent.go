package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"lumen-agent/internal/config"
	"lumen-agent/internal/llm"
	"lumen-agent/internal/skills"
	"lumen-agent/internal/tools"
)

const autoFollowThroughPrompt = "System follow-up: you made workspace changes during this turn. Unless the work is already verified or you are genuinely blocked, continue autonomously. Inspect the saved result, run the smallest relevant verification step you can, update TASKS.md if that would help continuity, and then give the final user-facing reply. Do not ask for confirmation for low-risk verification or obvious next steps."
const autoRecoveryPrompt = "System recovery: one or more tool calls failed during this turn. Continue autonomously by trying the safest reasonable fallback, narrower scope, or inspection step you can. Only stop if you are genuinely blocked or the next action would be high-risk. If you remain blocked, give a concrete blocker plus the best next step."
const autoWrapUpPrompt = "System wrap-up: your latest reply is too generic for the work completed in this turn. Continue autonomously and give a concrete final update: what changed, what you verified, any remaining blocker, and the next useful step if anything is still pending."

type chatClient interface {
	Chat(ctx context.Context, req llm.Request) (llm.Message, error)
}

type EventKind string

const (
	EventStatus       EventKind = "status"
	EventAssistant    EventKind = "assistant"
	EventToolStarted  EventKind = "tool_started"
	EventToolFinished EventKind = "tool_finished"
)

type Event struct {
	Kind       EventKind
	Message    string
	ToolName   string
	Detail     string
	FullDetail string
	Time       time.Time
}

type Runner struct {
	cfg      config.Config
	client   chatClient
	registry *tools.Registry
	skills   *skills.Loader
}

func NewRunner(cfg config.Config, client chatClient, registry *tools.Registry) *Runner {
	return &Runner{
		cfg:      cfg,
		client:   client,
		registry: registry,
		skills:   skills.NewLoader(cfg),
	}
}

func (r *Runner) SetBackgroundTaskManager(manager tools.BackgroundTaskManager) {
	if r == nil || r.registry == nil {
		return
	}
	r.registry.SetBackgroundTaskManager(manager)
}

func (r *Runner) SetSandboxManager(manager tools.SandboxManager) {
	if r == nil || r.registry == nil {
		return
	}
	r.registry.SetSandboxManager(manager)
}

func (r *Runner) SnapshotSkills() []skills.Summary {
	if r.skills == nil {
		return nil
	}
	return r.skills.Snapshot()
}

func (r *Runner) Run(ctx context.Context, history []llm.Message, userPrompt string, conversation ConversationContext, emit func(Event)) ([]llm.Message, error) {
	initialUserTime := time.Now().UTC()
	workingHistory := append(cloneMessages(history), llm.Message{
		Role:      "user",
		Content:   userPrompt,
		Parts:     conversation.UserParts,
		Timestamp: r.messageTimestamp(initialUserTime),
	})
	turnStartIndex := len(workingHistory) - 1
	autoRecoveryUsed := false
	autoFollowThroughUsed := false
	autoWrapUpUsed := false

	model := r.cfg.LLM.Model
	if strings.TrimSpace(conversation.ModelOverride) != "" {
		model = strings.TrimSpace(conversation.ModelOverride)
	}

	for step := 0; step < r.cfg.App.MaxAgentLoops; step++ {
		emit(Event{Kind: EventStatus, Message: "Contacting model", Time: time.Now()})

		response, err := r.chatWithRetry(ctx, llm.Request{
			Model:           model,
			Messages:        r.withSystemPrompt(workingHistory, conversation),
			Tools:           r.registry.Definitions(),
			Temperature:     r.cfg.LLM.Temperature,
			MaxTokens:       r.cfg.LLM.MaxTokens,
			ReasoningEffort: r.cfg.LLM.ReasoningEffort,
		}, emit)
		if err != nil {
			emit(Event{Kind: EventStatus, Message: "Request failed", Time: time.Now()})
			return workingHistory, err
		}

		responseTime := time.Now().UTC()
		assistantMessage := llm.Message{
			Role:          "assistant",
			Content:       sanitizeAssistantContent(response.Content),
			Name:          response.Name,
			ToolCalls:     response.ToolCalls,
			ResponseItems: response.ResponseItems,
			Timestamp:     r.messageTimestamp(responseTime),
		}
		workingHistory = append(workingHistory, assistantMessage)

		if strings.TrimSpace(response.Content) != "" {
			emit(Event{Kind: EventAssistant, Message: response.Content, Time: time.Now()})
		}

		if len(response.ToolCalls) == 0 {
			turnMessages := workingHistory[turnStartIndex+1:]
			if !autoRecoveryUsed && shouldAutoRecover(turnMessages) {
				autoRecoveryUsed = true
				workingHistory = append(workingHistory, llm.Message{
					Role:      "user",
					Content:   autoRecoveryPrompt,
					Timestamp: r.messageTimestamp(time.Now().UTC()),
				})
				emit(Event{Kind: EventStatus, Message: "Auto-recovery", Time: time.Now()})
				continue
			}
			if !autoFollowThroughUsed && shouldAutoFollowThrough(workingHistory[turnStartIndex+1:]) {
				autoFollowThroughUsed = true
				workingHistory = append(workingHistory, llm.Message{
					Role:      "user",
					Content:   autoFollowThroughPrompt,
					Timestamp: r.messageTimestamp(time.Now().UTC()),
				})
				emit(Event{Kind: EventStatus, Message: "Auto-follow-through", Time: time.Now()})
				continue
			}
			if !autoWrapUpUsed && shouldAutoWrapUp(turnMessages) {
				autoWrapUpUsed = true
				workingHistory = append(workingHistory, llm.Message{
					Role:      "user",
					Content:   autoWrapUpPrompt,
					Timestamp: r.messageTimestamp(time.Now().UTC()),
				})
				emit(Event{Kind: EventStatus, Message: "Auto-wrap-up", Time: time.Now()})
				continue
			}
			emit(Event{Kind: EventStatus, Message: "Ready", Time: time.Now()})
			return workingHistory, nil
		}

		toolCalls := response.ToolCalls
		if limit := r.cfg.App.MaxToolCallsPerTurn; limit > 0 && len(toolCalls) > limit {
			skipped := toolCalls[limit:]
			toolCalls = toolCalls[:limit]
			for _, call := range skipped {
				workingHistory = append(workingHistory, llm.Message{
					Role:       "tool",
					Name:       call.Function.Name,
					ToolCallID: call.ID,
					Content:    toolCallLimitResult(call.Function.Name, limit),
				})
			}
		}

		for _, call := range toolCalls {
			callTime := time.Now().UTC()
			emit(Event{
				Kind:       EventToolStarted,
				ToolName:   call.Function.Name,
				Detail:     compact(call.Function.Arguments, 220),
				FullDetail: call.Function.Arguments,
				Time:       callTime,
			})

			callCtx := tools.WithBackgroundTaskRuntimeContext(ctx, tools.BackgroundTaskRuntimeContext{
				History:     cloneMessages(workingHistory),
				RequestedAt: callTime,
			})
			result, err := r.registry.Execute(callCtx, call)
			if err != nil {
				result = toolErrorResult(call.Function.Name, err)
				emit(Event{
					Kind:       EventToolFinished,
					ToolName:   call.Function.Name,
					Detail:     "error: " + err.Error(),
					FullDetail: result,
					Time:       time.Now(),
				})
			} else {
				emit(Event{
					Kind:       EventToolFinished,
					ToolName:   call.Function.Name,
					Detail:     compact(result, 220),
					FullDetail: result,
					Time:       time.Now(),
				})
			}

			workingHistory = append(workingHistory, llm.Message{
				Role:       "tool",
				Name:       call.Function.Name,
				ToolCallID: call.ID,
				Content:    result,
				Timestamp:  r.messageTimestamp(time.Now().UTC()),
			})
		}
	}

	return workingHistory, fmt.Errorf("agent stopped after %d tool loops", r.cfg.App.MaxAgentLoops)
}

func shouldAutoFollowThrough(messages []llm.Message) bool {
	if len(messages) == 0 {
		return false
	}

	if containsInjectedPrompt(messages, autoFollowThroughPrompt) {
		return false
	}

	return hasMutatingToolResult(messages)
}

func shouldAutoRecover(messages []llm.Message) bool {
	if len(messages) == 0 {
		return false
	}

	if containsInjectedPrompt(messages, autoRecoveryPrompt) {
		return false
	}

	return hasToolErrorResult(messages)
}

func shouldAutoWrapUp(messages []llm.Message) bool {
	if len(messages) == 0 {
		return false
	}

	if containsInjectedPrompt(messages, autoWrapUpPrompt) {
		return false
	}

	if !hasToolActivity(messages) {
		return false
	}

	lastAssistant, ok := lastAssistantMessage(messages)
	if !ok {
		return false
	}

	return isVagueCompletionReply(lastAssistant.Content)
}

func containsInjectedPrompt(messages []llm.Message, prompt string) bool {
	for _, message := range messages {
		if message.Role == "user" && strings.TrimSpace(message.Content) == prompt {
			return true
		}
	}
	return false
}

func hasToolActivity(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role == "tool" {
			return true
		}
	}
	return false
}

func hasMutatingToolResult(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		if !isMutatingTool(message.Name) {
			continue
		}
		if strings.Contains(strings.ToLower(message.Content), `"error"`) {
			continue
		}
		return true
	}
	return false
}

func hasToolErrorResult(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		if isToolErrorContent(message.Content) {
			return true
		}
	}
	return false
}

func isMutatingTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "write_file", "replace_in_file", "move_path", "delete_path", "mkdir":
		return true
	default:
		return false
	}
}

func isToolErrorContent(content string) bool {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		_, ok := parsed["error"]
		return ok
	}
	return strings.Contains(strings.ToLower(content), `"error"`)
}

func lastAssistantMessage(messages []llm.Message) (llm.Message, bool) {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].Role == "assistant" {
			return messages[idx], true
		}
	}
	return llm.Message{}, false
}

func isVagueCompletionReply(content string) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	normalized = strings.TrimSuffix(normalized, "!")
	normalized = strings.TrimSuffix(normalized, ".")

	switch normalized {
	case "", "done", "fixed", "updated", "completed", "all set", "i updated the file", "i fixed it", "finished":
		return true
	default:
		return false
	}
}

func (r *Runner) chatWithRetry(ctx context.Context, req llm.Request, emit func(Event)) (llm.Message, error) {
	attempts := r.cfg.LLM.RequestMaxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	backoff := r.cfg.LLMRetryInitialBackoff()
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}

	maxBackoff := r.cfg.LLMRetryMaxBackoff()
	if maxBackoff < backoff {
		maxBackoff = backoff
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		response, err := r.client.Chat(ctx, req)
		if err == nil {
			return response, nil
		}

		lastErr = err
		if ctx.Err() != nil || attempt == attempts || !llm.IsRetriableError(err) {
			return llm.Message{}, err
		}

		if emit != nil {
			reason := "transient error"
			if llm.IsTimeoutError(err) {
				reason = "timeout"
			}
			emit(Event{
				Kind:    EventStatus,
				Message: fmt.Sprintf("Model request hit %s. Retrying (%d/%d)", reason, attempt+1, attempts),
				Time:    time.Now(),
			})
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return llm.Message{}, ctx.Err()
			}
			return llm.Message{}, lastErr
		case <-timer.C:
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return llm.Message{}, lastErr
}

func (r *Runner) withSystemPrompt(history []llm.Message, conversation ConversationContext) []llm.Message {
	systemPrompt := r.systemPrompt(conversation)
	trimmedHistory := trimHistoryForContext(history, systemPrompt, r.cfg.LLMInputTokenBudget())

	result := make([]llm.Message, 0, len(history)+1)
	result = append(result, llm.Message{
		Role:      "system",
		Content:   systemPrompt,
		Timestamp: r.messageTimestamp(time.Now().UTC()),
	})
	result = append(result, trimmedHistory...)
	return result
}

func cloneMessages(messages []llm.Message) []llm.Message {
	cloned := make([]llm.Message, len(messages))
	copy(cloned, messages)
	return cloned
}

func CompactHistoryForNextTurn(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return nil
	}

	compacted := make([]llm.Message, len(history))
	for i, message := range history {
		compacted[i] = message
		if compacted[i].Role == "assistant" {
			compacted[i].Content = sanitizeAssistantContent(compacted[i].Content)
			compacted[i].ResponseItems = nil
		}
	}
	return compacted
}

func CompactHistoryForStorage(cfg config.Config, history []llm.Message) []llm.Message {
	compacted := CompactHistoryForNextTurn(history)
	if !cfg.App.HistoryCompaction.Enabled || len(compacted) == 0 {
		return compacted
	}

	trigger := cfg.HistoryCompactionTriggerTokens()
	target := cfg.HistoryCompactionTargetTokens()
	preserve := cfg.HistoryCompactionPreserveRecentMessages()
	if trigger <= 0 || target <= 0 || preserve <= 0 {
		return compacted
	}
	if approximateHistoryTokens(compacted) <= trigger {
		return compacted
	}
	if len(compacted) <= preserve {
		return compacted
	}

	tailStart := len(compacted) - preserve
	if tailStart < 1 {
		tailStart = 1
	}
	for tailStart > 0 && tailStart < len(compacted) && compacted[tailStart].Role == "tool" {
		tailStart--
	}
	for tailStart > 0 && len(compacted[tailStart-1].ToolCalls) > 0 {
		tailStart--
	}
	if tailStart <= 0 || tailStart >= len(compacted) {
		return historyTail(compacted, preserve)
	}

	head := compacted[:tailStart]
	tail := compacted[tailStart:]
	if len(head) == 0 {
		return compacted
	}

	summary := summarizeHistory(head, target)
	if strings.TrimSpace(summary) == "" {
		return compacted
	}

	result := make([]llm.Message, 0, 1+len(tail))
	result = append(result, llm.Message{
		Role:      "system",
		Content:   summary,
		Timestamp: lastHistoryTimestamp(head),
	})
	result = append(result, tail...)
	return dropOrphanToolMessages(result)
}

func toolErrorResult(name string, err error) string {
	data, marshalErr := json.MarshalIndent(map[string]any{
		"tool":  name,
		"error": err.Error(),
	}, "", "  ")
	if marshalErr != nil {
		return fmt.Sprintf(`{"tool":%q,"error":%q}`, name, err.Error())
	}
	return string(data)
}

func toolCallLimitResult(name string, limit int) string {
	data, marshalErr := json.MarshalIndent(map[string]any{
		"tool":  name,
		"error": fmt.Sprintf("tool call skipped because app.max_tool_calls_per_turn=%d", limit),
	}, "", "  ")
	if marshalErr != nil {
		return fmt.Sprintf(`{"tool":%q,"error":%q}`, name, fmt.Sprintf("tool call skipped because app.max_tool_calls_per_turn=%d", limit))
	}
	return string(data)
}

func trimHistoryForContext(history []llm.Message, systemPrompt string, budgetTokens int) []llm.Message {
	if budgetTokens <= 0 || len(history) == 0 {
		return history
	}

	systemTokens := approximateTextTokens(systemPrompt) + 12
	if systemTokens >= budgetTokens {
		return historyTail(history, 1)
	}

	remainingBudget := budgetTokens - systemTokens
	selected := make([]llm.Message, 0, len(history))
	used := 0

	for idx := len(history) - 1; idx >= 0; idx-- {
		message := history[idx]
		tokens := approximateMessageTokens(message)

		if len(selected) == 0 {
			selected = append(selected, message)
			used += tokens
			continue
		}

		if used+tokens > remainingBudget {
			break
		}

		selected = append(selected, message)
		used += tokens
	}

	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}

	selected = dropOrphanToolMessages(selected)
	if len(selected) == 0 {
		return historyTail(history, 1)
	}

	return selected
}

func approximateMessageTokens(message llm.Message) int {
	count := 8
	count += approximateTextTokens(message.Role)
	count += approximateTextTokens(message.Content)
	count += approximateTextTokens(message.Name)
	count += approximateTextTokens(message.ToolCallID)

	for _, toolCall := range message.ToolCalls {
		count += 8
		count += approximateTextTokens(toolCall.ID)
		count += approximateTextTokens(toolCall.Function.Name)
		count += approximateTextTokens(toolCall.Function.Arguments)
	}

	return count
}

func approximateTextTokens(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}

	runes := utf8.RuneCountInString(trimmed)
	if runes <= 0 {
		return 0
	}

	return (runes + 3) / 4
}

func sanitizeAssistantContent(content string) string {
	trimmed := strings.TrimSpace(llm.StripMessageTimeMetadata(content))
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "<think") {
		return trimmed
	}

	var builder strings.Builder
	remaining := trimmed
	for {
		lowerRemaining := strings.ToLower(remaining)
		start := strings.Index(lowerRemaining, "<think")
		if start < 0 {
			builder.WriteString(remaining)
			break
		}

		builder.WriteString(remaining[:start])
		tagEnd := strings.Index(lowerRemaining[start:], ">")
		if tagEnd < 0 {
			break
		}

		afterOpen := start + tagEnd + 1
		closeIdx := strings.Index(strings.ToLower(remaining[afterOpen:]), "</think>")
		if closeIdx < 0 {
			break
		}

		remaining = remaining[afterOpen+closeIdx+len("</think>"):]
	}

	sanitized := strings.TrimSpace(builder.String())
	if sanitized == "" {
		return ""
	}

	return strings.Join(strings.Fields(sanitized), " ")
}

func dropOrphanToolMessages(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return nil
	}

	validToolCallIDs := make(map[string]struct{})
	for _, message := range history {
		for _, call := range message.ToolCalls {
			if strings.TrimSpace(call.ID) == "" {
				continue
			}
			validToolCallIDs[call.ID] = struct{}{}
		}
	}

	cleaned := make([]llm.Message, 0, len(history))
	for _, message := range history {
		if message.Role != "tool" {
			cleaned = append(cleaned, message)
			continue
		}

		if _, ok := validToolCallIDs[message.ToolCallID]; !ok {
			continue
		}

		cleaned = append(cleaned, message)
	}

	return cleaned
}

func historyTail(history []llm.Message, count int) []llm.Message {
	if count <= 0 || len(history) == 0 {
		return nil
	}
	if count >= len(history) {
		return history
	}
	return history[len(history)-count:]
}

func approximateHistoryTokens(history []llm.Message) int {
	total := 0
	for _, message := range history {
		total += approximateMessageTokens(message)
	}
	return total
}

func summarizeHistory(history []llm.Message, targetTokens int) string {
	if len(history) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("Auto-generated conversation summary of earlier context:\n")

	maxLines := len(history)
	if maxLines > 48 {
		maxLines = 48
	}

	lines := 0
	for _, message := range history {
		if lines >= maxLines || approximateTextTokens(builder.String()) >= targetTokens {
			break
		}
		line := historySummaryLine(message)
		if line == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(line)
		builder.WriteByte('\n')
		lines++
	}

	summary := strings.TrimSpace(builder.String())
	if approximateTextTokens(summary) <= targetTokens {
		return summary
	}

	trimmed := []rune(summary)
	limit := targetTokens * 4
	if limit > 3 && len(trimmed) > limit {
		return strings.TrimSpace(string(trimmed[:limit-3])) + "..."
	}
	return summary
}

func historySummaryLine(message llm.Message) string {
	role := strings.TrimSpace(message.Role)
	switch role {
	case "tool":
		name := strings.TrimSpace(message.Name)
		if name == "" {
			name = "tool"
		}
		return fmt.Sprintf("tool %s -> %s", name, compact(message.Content, 160))
	default:
		content := compact(message.Content, 160)
		if content == "" && len(message.ToolCalls) > 0 {
			toolNames := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				if strings.TrimSpace(call.Function.Name) == "" {
					continue
				}
				toolNames = append(toolNames, strings.TrimSpace(call.Function.Name))
			}
			if len(toolNames) > 0 {
				content = "called tools: " + strings.Join(toolNames, ", ")
			}
		}
		if content == "" {
			return ""
		}
		return role + ": " + content
	}
}

func lastHistoryTimestamp(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if strings.TrimSpace(history[i].Timestamp) != "" {
			return history[i].Timestamp
		}
	}
	return ""
}

func (r *Runner) messageTimestamp(now time.Time) string {
	if !r.cfg.LLM.InjectMessageTimestamps {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC().Format(time.RFC3339)
}

func compact(value string, limit int) string {
	collapsed := strings.Join(strings.Fields(value), " ")
	if len(collapsed) <= limit {
		return collapsed
	}
	return collapsed[:limit-3] + "..."
}
