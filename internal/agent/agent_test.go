package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"element-orion/internal/config"
	"element-orion/internal/llm"
	"element-orion/internal/tools"
)

type fakeChatResult struct {
	message llm.Message
	err     error
}

type fakeChatClient struct {
	results []fakeChatResult
	calls   int
	reqs    []llm.Request
}

func (f *fakeChatClient) Chat(_ context.Context, req llm.Request) (llm.Message, error) {
	if f.calls >= len(f.results) {
		return llm.Message{}, errors.New("unexpected chat call")
	}

	f.reqs = append(f.reqs, req)
	result := f.results[f.calls]
	f.calls++
	return result.message, result.err
}

func TestTrimHistoryForContextKeepsLatestMessage(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "first message"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "latest message must survive"},
	}

	trimmed := trimHistoryForContext(history, "system", 10)
	if len(trimmed) == 0 {
		t.Fatal("expected at least one message after trimming")
	}
	if trimmed[len(trimmed)-1].Content != "latest message must survive" {
		t.Fatalf("expected latest message to remain, got %q", trimmed[len(trimmed)-1].Content)
	}
}

func TestNormalizeToolCallHistoryRemovesDanglingToolResponses(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", ToolCallID: "missing", Content: "orphan"},
		{Role: "assistant", Content: "ok"},
	}

	cleaned := normalizeToolCallHistory(history)
	if len(cleaned) != 2 {
		t.Fatalf("expected orphan tool message to be removed, got %d messages", len(cleaned))
	}
	for _, message := range cleaned {
		if message.Role == "tool" {
			t.Fatalf("did not expect tool message in cleaned history: %+v", message)
		}
	}
}

func TestNormalizeToolCallHistoryRemovesAssistantToolCallWithoutResponses(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "hello"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolFunctionCall{
					Name:      "list_dir",
					Arguments: `{"path":"."}`,
				},
			}},
		},
		{Role: "assistant", Content: "done"},
	}

	cleaned := normalizeToolCallHistory(history)
	if len(cleaned) != 2 {
		t.Fatalf("expected orphan assistant tool call message to be removed, got %#v", cleaned)
	}
	if cleaned[1].Role != "assistant" || cleaned[1].Content != "done" {
		t.Fatalf("unexpected cleaned history %#v", cleaned)
	}
}

func TestNormalizeToolCallHistoryKeepsMatchedAssistantToolCallAndToolResponse(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "hello"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolFunctionCall{
					Name:      "list_dir",
					Arguments: `{"path":"."}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: `{"entries":["README.md"]}`},
	}

	cleaned := normalizeToolCallHistory(history)
	if len(cleaned) != 3 {
		t.Fatalf("expected matched tool call pair to remain, got %#v", cleaned)
	}
}

func TestSanitizeAssistantContentRemovesThinkBlocks(t *testing.T) {
	content := "before <think>\nprivate reasoning\n</think> after"

	got := sanitizeAssistantContent(content)
	if got != "before after" {
		t.Fatalf("expected think block to be removed, got %q", got)
	}
}

func TestSanitizeAssistantContentRemovesMessageTimePrefix(t *testing.T) {
	content := "[message_time 2026-03-28T05:24:27Z]\nhello"

	got := sanitizeAssistantContent(content)
	if got != "hello" {
		t.Fatalf("expected message_time prefix to be removed, got %q", got)
	}
}

func TestCompactHistoryForNextTurnDropsAssistantResponseItems(t *testing.T) {
	history := []llm.Message{
		{
			Role:          "assistant",
			Content:       "hello",
			ResponseItems: []map[string]any{{"type": "reasoning", "id": "rs_123"}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"ok":true}`,
		},
	}

	compacted := CompactHistoryForNextTurn(history)
	if len(compacted[0].ResponseItems) != 0 {
		t.Fatalf("expected assistant response items to be dropped, got %#v", compacted[0].ResponseItems)
	}
	if compacted[1].Content != `{"ok":true}` {
		t.Fatalf("expected non-assistant messages to stay unchanged, got %q", compacted[1].Content)
	}
}

func TestChatWithRetryRetriesTimeoutAndSucceeds(t *testing.T) {
	client := &fakeChatClient{results: []fakeChatResult{
		{err: context.DeadlineExceeded},
		{message: llm.Message{Role: "assistant", Content: "ok"}},
	}}

	runner := &Runner{cfg: config.Config{LLM: config.LLMConfig{
		RequestMaxAttempts:  3,
		RetryInitialBackoff: "1ms",
		RetryMaxBackoff:     "2ms",
	}}, client: client}
	message, err := runner.chatWithRetry(context.Background(), llm.Request{}, nil)
	if err != nil {
		t.Fatalf("chatWithRetry returned unexpected error: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 chat attempts, got %d", client.calls)
	}
	if message.Content != "ok" {
		t.Fatalf("expected successful response content, got %q", message.Content)
	}
}

func TestChatWithRetryStopsOnNonRetriableError(t *testing.T) {
	client := &fakeChatClient{results: []fakeChatResult{
		{err: errors.New("decode response: invalid character")},
	}}

	runner := &Runner{cfg: config.Config{LLM: config.LLMConfig{
		RequestMaxAttempts:  3,
		RetryInitialBackoff: "1ms",
		RetryMaxBackoff:     "2ms",
	}}, client: client}
	_, err := runner.chatWithRetry(context.Background(), llm.Request{}, nil)
	if err == nil {
		t.Fatal("expected chatWithRetry to return an error")
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 chat attempt for non-retriable error, got %d", client.calls)
	}
}

func TestChatWithRetryStopsAtMaxAttempts(t *testing.T) {
	client := &fakeChatClient{results: []fakeChatResult{
		{err: context.DeadlineExceeded},
		{err: context.DeadlineExceeded},
		{err: context.DeadlineExceeded},
	}}

	runner := &Runner{cfg: config.Config{LLM: config.LLMConfig{
		RequestMaxAttempts:  3,
		RetryInitialBackoff: "1ms",
		RetryMaxBackoff:     "2ms",
	}}, client: client}
	_, err := runner.chatWithRetry(context.Background(), llm.Request{}, nil)
	if err == nil {
		t.Fatal("expected chatWithRetry to return an error after retries")
	}
	if !llm.IsTimeoutError(err) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if client.calls != 3 {
		t.Fatalf("expected 3 chat attempts, got %d", client.calls)
	}
}

func TestRunAutoFollowThroughAfterWorkspaceMutation(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot:       workspace,
			MaxAgentLoops:       4,
			MaxToolCallsPerTurn: 8,
		},
		LLM: config.LLMConfig{
			Model:               "test-model",
			MaxTokens:           256,
			ContextWindowTokens: 2048,
		},
		Tools: config.ToolsConfig{
			Enabled:               []string{"write_file", "read_file"},
			ExecShell:             "/bin/zsh",
			ExecTimeout:           "1s",
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}

	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	client := &fakeChatClient{results: []fakeChatResult{
		{message: llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-write",
				Type: "function",
				Function: llm.ToolFunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"note.txt","content":"hello","overwrite":true}`,
				},
			}},
		}},
		{message: llm.Message{Role: "assistant", Content: "I updated the file."}},
		{message: llm.Message{Role: "assistant", Content: "I verified the saved file and finished the task."}},
	}}

	runner := &Runner{cfg: cfg, client: client, registry: registry}
	history, err := runner.Run(context.Background(), nil, "create note.txt", ConversationContext{}, func(Event) {})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if client.calls != 3 {
		t.Fatalf("expected 3 chat calls including auto-follow-through, got %d", client.calls)
	}

	lastReq := client.reqs[len(client.reqs)-1]
	lastMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if lastMsg.Role != "user" || lastMsg.Content != autoFollowThroughPrompt {
		t.Fatalf("expected final request to append auto-follow-through prompt, got role=%q content=%q", lastMsg.Role, lastMsg.Content)
	}

	data, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil {
		t.Fatalf("expected note.txt to be written: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected note.txt content %q", string(data))
	}

	if got := history[len(history)-1].Content; got != "I verified the saved file and finished the task." {
		t.Fatalf("expected final assistant reply after auto-follow-through, got %q", got)
	}
}

func TestRunAutoRecoveryAfterToolFailure(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot:       workspace,
			MaxAgentLoops:       4,
			MaxToolCallsPerTurn: 8,
		},
		LLM: config.LLMConfig{
			Model:               "test-model",
			MaxTokens:           256,
			ContextWindowTokens: 2048,
		},
		Tools: config.ToolsConfig{
			Enabled:               []string{"read_file"},
			ExecShell:             "/bin/zsh",
			ExecTimeout:           "1s",
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}

	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	client := &fakeChatClient{results: []fakeChatResult{
		{message: llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-read",
				Type: "function",
				Function: llm.ToolFunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"missing.txt"}`,
				},
			}},
		}},
		{message: llm.Message{Role: "assistant", Content: "I hit a problem."}},
		{message: llm.Message{Role: "assistant", Content: "I could not read missing.txt because it does not exist, so the next best step is to create it or point me at the right file."}},
	}}

	runner := &Runner{cfg: cfg, client: client, registry: registry}
	history, err := runner.Run(context.Background(), nil, "inspect missing.txt", ConversationContext{}, func(Event) {})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if client.calls != 3 {
		t.Fatalf("expected 3 chat calls including auto-recovery, got %d", client.calls)
	}

	lastReq := client.reqs[len(client.reqs)-1]
	lastMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if lastMsg.Role != "user" || lastMsg.Content != autoRecoveryPrompt {
		t.Fatalf("expected final request to append auto-recovery prompt, got role=%q content=%q", lastMsg.Role, lastMsg.Content)
	}

	if got := history[len(history)-1].Content; !strings.Contains(got, "missing.txt") {
		t.Fatalf("expected concrete blocker in final assistant reply, got %q", got)
	}
}

func TestRunAutoWrapUpAfterVagueReply(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write note.txt: %v", err)
	}

	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot:       workspace,
			MaxAgentLoops:       4,
			MaxToolCallsPerTurn: 8,
		},
		LLM: config.LLMConfig{
			Model:               "test-model",
			MaxTokens:           256,
			ContextWindowTokens: 2048,
		},
		Tools: config.ToolsConfig{
			Enabled:               []string{"read_file"},
			ExecShell:             "/bin/zsh",
			ExecTimeout:           "1s",
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}

	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	client := &fakeChatClient{results: []fakeChatResult{
		{message: llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-read",
				Type: "function",
				Function: llm.ToolFunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"note.txt"}`,
				},
			}},
		}},
		{message: llm.Message{Role: "assistant", Content: "Done."}},
		{message: llm.Message{Role: "assistant", Content: "I read note.txt and confirmed it contains hello."}},
	}}

	runner := &Runner{cfg: cfg, client: client, registry: registry}
	history, err := runner.Run(context.Background(), nil, "check note.txt", ConversationContext{}, func(Event) {})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if client.calls != 3 {
		t.Fatalf("expected 3 chat calls including auto-wrap-up, got %d", client.calls)
	}

	lastReq := client.reqs[len(client.reqs)-1]
	lastMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if lastMsg.Role != "user" || lastMsg.Content != autoWrapUpPrompt {
		t.Fatalf("expected final request to append auto-wrap-up prompt, got role=%q content=%q", lastMsg.Role, lastMsg.Content)
	}

	if got := history[len(history)-1].Content; !strings.Contains(got, "note.txt") {
		t.Fatalf("expected concrete final assistant reply, got %q", got)
	}
}

func TestRunInjectsMessageTimestamps(t *testing.T) {
	cfg := config.Config{
		App: config.AppConfig{
			WorkspaceRoot:       t.TempDir(),
			MaxAgentLoops:       1,
			MaxToolCallsPerTurn: 1,
		},
		LLM: config.LLMConfig{
			Model:                   "test-model",
			MaxTokens:               128,
			ContextWindowTokens:     512,
			InjectMessageTimestamps: true,
		},
		Tools: config.ToolsConfig{
			Enabled:               []string{},
			ExecShell:             "/bin/zsh",
			ExecTimeout:           "1s",
			MaxFileBytes:          1 << 20,
			MaxSearchResults:      20,
			MaxCommandOutputBytes: 4096,
		},
	}

	registry, err := tools.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()

	client := &fakeChatClient{results: []fakeChatResult{
		{message: llm.Message{Role: "assistant", Content: "ok"}},
	}}

	runner := &Runner{cfg: cfg, client: client, registry: registry}
	_, err = runner.Run(context.Background(), nil, "hello", ConversationContext{}, func(Event) {})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(client.reqs) != 1 {
		t.Fatalf("expected one request, got %d", len(client.reqs))
	}
	req := client.reqs[0]
	if len(req.Messages) < 2 {
		t.Fatalf("expected system and user messages, got %#v", req.Messages)
	}
	if req.Messages[0].Timestamp == "" {
		t.Fatal("expected system message timestamp to be set")
	}
	if req.Messages[1].Timestamp == "" {
		t.Fatal("expected user message timestamp to be set")
	}
}

func TestCompactHistoryForStorageSummarizesOldTurns(t *testing.T) {
	cfg := config.Config{
		App: config.AppConfig{
			HistoryCompaction: config.AppHistoryCompactionConfig{
				Enabled:                true,
				TriggerTokens:          20,
				TargetTokens:           10,
				PreserveRecentMessages: 2,
			},
		},
		LLM: config.LLMConfig{
			MaxTokens:           64,
			ContextWindowTokens: 256,
		},
	}

	history := []llm.Message{
		{Role: "user", Content: strings.Repeat("first ", 20), Timestamp: time.Now().Add(-4 * time.Minute).Format(time.RFC3339)},
		{Role: "assistant", Content: strings.Repeat("second ", 20), Timestamp: time.Now().Add(-3 * time.Minute).Format(time.RFC3339)},
		{Role: "user", Content: "keep this user turn", Timestamp: time.Now().Add(-2 * time.Minute).Format(time.RFC3339)},
		{Role: "assistant", Content: "keep this assistant turn", Timestamp: time.Now().Add(-1 * time.Minute).Format(time.RFC3339)},
	}

	compacted := CompactHistoryForStorage(cfg, history)
	if len(compacted) != 3 {
		t.Fatalf("expected summary plus preserved tail, got %#v", compacted)
	}
	if compacted[0].Role != "system" {
		t.Fatalf("expected first message to be system summary, got %#v", compacted[0])
	}
	if !strings.Contains(compacted[0].Content, "Auto-generated conversation summary") {
		t.Fatalf("expected summary marker, got %q", compacted[0].Content)
	}
	if compacted[1].Content != "keep this user turn" {
		t.Fatalf("expected recent user turn to be preserved, got %q", compacted[1].Content)
	}
	if compacted[2].Content != "keep this assistant turn" {
		t.Fatalf("expected recent assistant turn to be preserved, got %q", compacted[2].Content)
	}
}

func TestCompactHistoryForStorageChunksLargeHistory(t *testing.T) {
	cfg := config.Config{
		App: config.AppConfig{
			HistoryCompaction: config.AppHistoryCompactionConfig{
				Enabled:                true,
				TriggerTokens:          80,
				TargetTokens:           64,
				PreserveRecentMessages: 2,
			},
		},
		LLM: config.LLMConfig{
			MaxTokens:           128,
			ContextWindowTokens: 96,
		},
	}

	history := []llm.Message{
		{Role: "user", Content: strings.Repeat("build id abc123 path /srv/app release 42 ", 6), Timestamp: time.Now().Add(-6 * time.Minute).Format(time.RFC3339)},
		{Role: "assistant", Content: strings.Repeat("checked logs for abc123 and release 42 ", 6), Timestamp: time.Now().Add(-5 * time.Minute).Format(time.RFC3339)},
		{Role: "user", Content: strings.Repeat("next inspect worker host api-01.example.internal ", 6), Timestamp: time.Now().Add(-4 * time.Minute).Format(time.RFC3339)},
		{Role: "assistant", Content: strings.Repeat("queued follow-up for api-01.example.internal and task 99 ", 6), Timestamp: time.Now().Add(-3 * time.Minute).Format(time.RFC3339)},
		{Role: "user", Content: "keep this user turn", Timestamp: time.Now().Add(-2 * time.Minute).Format(time.RFC3339)},
		{Role: "assistant", Content: "keep this assistant turn", Timestamp: time.Now().Add(-1 * time.Minute).Format(time.RFC3339)},
	}

	compacted := CompactHistoryForStorage(cfg, history)
	if len(compacted) != 3 {
		t.Fatalf("expected summary plus preserved tail, got %#v", compacted)
	}
	if compacted[0].Role != "system" {
		t.Fatalf("expected first message to be system summary, got %#v", compacted[0])
	}
	if !strings.Contains(compacted[0].Content, "Auto-generated conversation summary") {
		t.Fatalf("expected summary marker, got %q", compacted[0].Content)
	}
	if !strings.Contains(compacted[0].Content, "abc123") {
		t.Fatalf("expected identifiers to survive compaction, got %q", compacted[0].Content)
	}
}
