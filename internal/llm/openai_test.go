package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAIClientUsesChatCompletionsEndpoint(t *testing.T) {
	client := &Client{impl: &chatCompletionsClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/chat/completions",
		apiKey:   "test-key",
		headers:  map[string]string{},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/chat/completions" {
				t.Fatalf("expected /chat/completions path, got %q", r.URL.Path)
			}

			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload["model"] != "gpt-4.1-mini" {
				t.Fatalf("unexpected model %v", payload["model"])
			}
			if payload["reasoning_effort"] != "medium" {
				t.Fatalf("unexpected reasoning_effort %v", payload["reasoning_effort"])
			}
			if payload["max_thinking_tokens"] != float64(128) {
				t.Fatalf("unexpected max_thinking_tokens %v", payload["max_thinking_tokens"])
			}

			body, err := json.Marshal(map[string]any{
				"usage": map[string]any{
					"completion_tokens": 18,
					"completion_tokens_details": map[string]any{
						"reasoning_tokens": 7,
					},
				},
				"choices": []map[string]any{{
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello from chat completions",
					},
				}},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		})},
	}}}

	message, err := client.Chat(context.Background(), Request{
		Model:            "gpt-4.1-mini",
		Messages:         []Message{{Role: "user", Content: "hi"}},
		Temperature:      0.2,
		MaxTokens:        32,
		ReasoningEffort:  "medium",
		MaxThinkingToken: "128",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if message.Content != "hello from chat completions" {
		t.Fatalf("unexpected content %q", message.Content)
	}
	if message.OutputTokens != 18 {
		t.Fatalf("expected 18 output tokens, got %d", message.OutputTokens)
	}
	if message.ReasoningTokens != 7 {
		t.Fatalf("expected 7 reasoning tokens, got %d", message.ReasoningTokens)
	}
	if message.RequestPayload["model"] != "gpt-4.1-mini" {
		t.Fatalf("expected request payload model to be preserved, got %#v", message.RequestPayload["model"])
	}
	if _, ok := message.RawResponse["usage"].(map[string]any); !ok {
		t.Fatalf("expected raw response usage to be preserved, got %#v", message.RawResponse)
	}
}

func TestNewClientAppliesKimiNoThinkExtraBodyToChatCompletions(t *testing.T) {
	client := NewClient(
		"https://api.example.test",
		"test-key",
		APITypeOpenAI,
		map[string]string{"X-Shared": "shared"},
		true,
		false,
		30*time.Second,
	)

	chatClient, ok := client.impl.(*chatCompletionsClient)
	if !ok {
		t.Fatalf("expected chat completions client, got %T", client.impl)
	}
	if got := chatClient.headers["X-Shared"]; got != "shared" {
		t.Fatalf("expected shared header to remain unchanged, got %q", got)
	}
	kwargs, ok := chatClient.extraBody["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["thinking"] != false {
		t.Fatalf("expected kimi no-think extra body, got %#v", chatClient.extraBody)
	}
}

func TestOpenAIClientOmitsReasoningFieldsWhenSetToOff(t *testing.T) {
	client := &Client{impl: &chatCompletionsClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/chat/completions",
		apiKey:   "test-key",
		headers:  map[string]string{},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if _, ok := payload["reasoning_effort"]; ok {
				t.Fatalf("expected reasoning_effort to be omitted, got %#v", payload["reasoning_effort"])
			}
			if _, ok := payload["max_thinking_tokens"]; ok {
				t.Fatalf("expected max_thinking_tokens to be omitted, got %#v", payload["max_thinking_tokens"])
			}

			body, err := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello",
					},
				}},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		})},
	}}}

	_, err := client.Chat(context.Background(), Request{
		Model:            "gpt-4.1-mini",
		Messages:         []Message{{Role: "user", Content: "hi"}},
		Temperature:      0.2,
		MaxTokens:        32,
		ReasoningEffort:  "off",
		MaxThinkingToken: "512",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}

func TestOpenAIClientSendsReasoningEffortNoneLiterally(t *testing.T) {
	client := &Client{impl: &chatCompletionsClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/chat/completions",
		apiKey:   "test-key",
		headers:  map[string]string{},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload["reasoning_effort"] != "none" {
				t.Fatalf("expected reasoning_effort=none, got %#v", payload["reasoning_effort"])
			}

			body, err := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello",
					},
				}},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		})},
	}}}

	_, err := client.Chat(context.Background(), Request{
		Model:           "gpt-4.1-mini",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		Temperature:     0.2,
		MaxTokens:       32,
		ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}

func TestOpenAIClientIncludesConfiguredNoThinkFieldsInPayload(t *testing.T) {
	client := &Client{impl: &chatCompletionsClient{
		httpJSONClient: &httpJSONClient{
			endpoint: "https://api.example.test/chat/completions",
			apiKey:   "test-key",
			headers:  map[string]string{},
			httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}

				kwargs, ok := payload["chat_template_kwargs"].(map[string]any)
				if !ok || kwargs["thinking"] != false {
					t.Fatalf("expected kimi no-think payload, got %#v", payload["chat_template_kwargs"])
				}
				thinking, ok := payload["thinking"].(map[string]any)
				if !ok || thinking["type"] != "disabled" {
					t.Fatalf("expected glm no-think payload, got %#v", payload["thinking"])
				}
				if payload["clear_thinking"] != true {
					t.Fatalf("expected clear_thinking=true, got %#v", payload["clear_thinking"])
				}

				body, err := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"message": map[string]any{
							"role":    "assistant",
							"content": "ok",
						},
					}},
				})
				if err != nil {
					t.Fatalf("marshal response: %v", err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			})},
		},
		extraBody: buildOpenAIExtraBody(true, true),
	}}

	_, err := client.Chat(context.Background(), Request{
		Model:       "kimi-k2.5",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: 0.2,
		MaxTokens:   32,
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}

func TestCodexClientUsesResponsesEndpointAndPreservesToolLoopState(t *testing.T) {
	client := &Client{impl: &responsesClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/responses",
		apiKey:   "test-key",
		headers:  map[string]string{"OpenAI-Beta": "responses=v1"},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/responses" {
				t.Fatalf("expected /responses path, got %q", r.URL.Path)
			}

			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload["model"] != "codex-mini-latest" {
				t.Fatalf("unexpected model %v", payload["model"])
			}
			if payload["max_output_tokens"] != float64(64) {
				t.Fatalf("unexpected max_output_tokens %v", payload["max_output_tokens"])
			}
			if _, ok := payload["temperature"]; ok {
				t.Fatalf("did not expect temperature in responses payload, got %#v", payload["temperature"])
			}
			reasoning, ok := payload["reasoning"].(map[string]any)
			if !ok || reasoning["effort"] != "high" {
				t.Fatalf("unexpected reasoning payload %#v", payload["reasoning"])
			}
			if reasoning["max_thinking_tokens"] != float64(256) {
				t.Fatalf("unexpected max_thinking_tokens in reasoning payload %#v", payload["reasoning"])
			}
			if r.Header.Get("OpenAI-Beta") != "responses=v1" {
				t.Fatalf("expected OpenAI-Beta header to be preserved, got %q", r.Header.Get("OpenAI-Beta"))
			}

			input, ok := payload["input"].([]any)
			if !ok {
				t.Fatalf("expected input array, got %T", payload["input"])
			}
			if len(input) != 4 {
				t.Fatalf("expected 4 input items, got %d", len(input))
			}

			functionCall, _ := input[1].(map[string]any)
			if functionCall["type"] != "function_call" || functionCall["call_id"] != "call_123" {
				t.Fatalf("unexpected function_call item: %#v", functionCall)
			}

			functionOutput, _ := input[2].(map[string]any)
			if functionOutput["type"] != "function_call_output" || functionOutput["call_id"] != "call_123" {
				t.Fatalf("unexpected function_call_output item: %#v", functionOutput)
			}

			firstMessage, _ := input[0].(map[string]any)
			firstContent, ok := firstMessage["content"].(string)
			if !ok {
				t.Fatalf("expected plain string message content, got %#v", firstMessage["content"])
			}
			if firstContent != "You are helpful." {
				t.Fatalf("unexpected first message content %#v", firstContent)
			}

			tools, ok := payload["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("expected one tool, got %#v", payload["tools"])
			}

			body, err := json.Marshal(map[string]any{
				"usage": map[string]any{
					"output_tokens": 24,
					"output_tokens_details": map[string]any{
						"reasoning_tokens": 9,
					},
				},
				"output": []map[string]any{
					{
						"type": "reasoning",
						"id":   "rs_123",
					},
					{
						"type": "message",
						"role": "assistant",
						"content": []map[string]any{{
							"type": "output_text",
							"text": "I checked it.",
						}},
					},
					{
						"type":      "function_call",
						"call_id":   "call_456",
						"name":      "write_file",
						"arguments": `{"path":"note.txt"}`,
					},
				},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		})},
	}}}

	message, err := client.Chat(context.Background(), Request{
		Model: "codex-mini-latest",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: ToolFunctionCall{
					Name:      "list_dir",
					Arguments: `{"path":"."}`,
				},
			}}},
			{Role: "tool", ToolCallID: "call_123", Content: `{"entries":["README.md"]}`},
			{Role: "user", Content: "What next?"},
		},
		Tools: []ToolDefinition{{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:        "write_file",
				Description: "Write a file",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
		Temperature:      0.2,
		MaxTokens:        64,
		ReasoningEffort:  "high",
		MaxThinkingToken: "256",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if message.Content != "I checked it." {
		t.Fatalf("unexpected content %q", message.Content)
	}
	if len(message.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(message.ToolCalls))
	}
	if len(message.ResponseItems) != 3 {
		t.Fatalf("expected three raw response items, got %d", len(message.ResponseItems))
	}
	if message.OutputTokens != 24 {
		t.Fatalf("expected 24 output tokens, got %d", message.OutputTokens)
	}
	if message.ReasoningTokens != 9 {
		t.Fatalf("expected 9 reasoning tokens, got %d", message.ReasoningTokens)
	}
	if message.RequestPayload["model"] != "codex-mini-latest" {
		t.Fatalf("expected request payload model to be preserved, got %#v", message.RequestPayload["model"])
	}
	if _, ok := message.RawResponse["usage"].(map[string]any); !ok {
		t.Fatalf("expected raw response usage to be preserved, got %#v", message.RawResponse)
	}
	if message.ToolCalls[0].ID != "call_456" {
		t.Fatalf("expected tool call ID call_456, got %q", message.ToolCalls[0].ID)
	}
	if message.ToolCalls[0].Function.Name != "write_file" {
		t.Fatalf("unexpected tool name %q", message.ToolCalls[0].Function.Name)
	}
}

func TestNewClientAppliesGLMNoThinkExtraBodyToChatCompletions(t *testing.T) {
	client := NewClient(
		"https://api.example.test",
		"test-key",
		APITypeOpenAI,
		map[string]string{},
		false,
		true,
		30*time.Second,
	)

	chatClient, ok := client.impl.(*chatCompletionsClient)
	if !ok {
		t.Fatalf("expected chat completions client, got %T", client.impl)
	}
	thinking, ok := chatClient.extraBody["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("expected glm thinking disable payload, got %#v", chatClient.extraBody)
	}
	if got := chatClient.extraBody["clear_thinking"]; got != true {
		t.Fatalf("expected clear_thinking=true, got %#v", got)
	}
}

func TestNewClientDoesNotApplyNoThinkExtraBodyToCodex(t *testing.T) {
	client := NewClient(
		"https://api.example.test",
		"test-key",
		APITypeCodex,
		map[string]string{"X-Shared": "shared"},
		true,
		true,
		30*time.Second,
	)

	responses, ok := client.impl.(*responsesClient)
	if !ok {
		t.Fatalf("expected responses client, got %T", client.impl)
	}
	if got := responses.headers["X-Shared"]; got != "shared" {
		t.Fatalf("expected shared header to remain for codex, got %q", got)
	}
}

func TestBuildResponsesInputUsesStoredResponseItemsForAssistantMessages(t *testing.T) {
	items := buildResponsesInput([]Message{
		{
			Role: "assistant",
			ResponseItems: []map[string]any{
				{"type": "reasoning", "id": "rs_123"},
				{"type": "function_call", "call_id": "call_123", "name": "list_dir", "arguments": `{"path":"."}`},
			},
		},
		{Role: "tool", ToolCallID: "call_123", Content: `{"entries":["README.md"]}`},
	})

	if len(items) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(items))
	}
	if items[0]["type"] != "reasoning" {
		t.Fatalf("expected first item to be preserved reasoning item, got %#v", items[0])
	}
	if items[1]["type"] != "function_call" {
		t.Fatalf("expected second item to be preserved function_call item, got %#v", items[1])
	}
	if items[2]["type"] != "function_call_output" {
		t.Fatalf("expected third item to be tool output, got %#v", items[2])
	}
}

func TestResponseMessageToMessageStripsMessageTimePrefix(t *testing.T) {
	message := responseMessage{
		Role:    "assistant",
		Content: flexibleContent{text: "[message_time 2026-03-28T05:24:27Z]\nhello"},
	}.toMessage()

	if message.Content != "hello" {
		t.Fatalf("expected message_time prefix to be removed, got %q", message.Content)
	}
}

func TestBuildResponsesInputDropsOrphanFunctionCallOutputs(t *testing.T) {
	items := buildResponsesInput([]Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "tool", ToolCallID: "call_missing", Content: `{"ok":true}`},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: ToolFunctionCall{
					Name:      "list_dir",
					Arguments: `{"path":"."}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_123", Content: `{"entries":["README.md"]}`},
	})

	if len(items) != 3 {
		t.Fatalf("expected orphan output to be dropped, got %d items: %#v", len(items), items)
	}
	if items[1]["type"] != "function_call" {
		t.Fatalf("expected second item to be matching function_call, got %#v", items[1])
	}
	if items[2]["type"] != "function_call_output" {
		t.Fatalf("expected third item to be matching function_call_output, got %#v", items[2])
	}
	if callID, _ := items[2]["call_id"].(string); callID != "call_123" {
		t.Fatalf("expected preserved tool output for call_123, got %#v", items[2])
	}
}

func TestCodexClientRetriesWithStreamingWhenProviderRequiresIt(t *testing.T) {
	client := &Client{impl: &responsesClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/responses",
		apiKey:   "test-key",
		headers:  map[string]string{},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}

			if payload["stream"] != true {
				body := []byte(`{"error":{"message":"only support stream"}}`)
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			}

			streamBody := strings.Join([]string{
				`data: {"type":"response.output_item.added"}`,
				``,
				`data: {"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"streamed ok"}]}]}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(streamBody)),
			}
			resp.Header.Set("Content-Type", "text/event-stream")
			return resp, nil
		})},
	}}}

	message, err := client.Chat(context.Background(), Request{
		Model:           "gpt-5.4",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		Temperature:     0.2,
		MaxTokens:       32,
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if message.Content != "streamed ok" {
		t.Fatalf("unexpected content %q", message.Content)
	}
}

func TestCodexClientOmitsReasoningPayloadWhenSetToOff(t *testing.T) {
	client := &Client{impl: &responsesClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/responses",
		apiKey:   "test-key",
		headers:  map[string]string{},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if _, ok := payload["reasoning"]; ok {
				t.Fatalf("expected reasoning payload to be omitted, got %#v", payload["reasoning"])
			}

			body, err := json.Marshal(map[string]any{
				"output": []map[string]any{
					{
						"type": "message",
						"role": "assistant",
						"content": []map[string]any{{
							"type": "output_text",
							"text": "ok",
						}},
					},
				},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		})},
	}}}

	_, err := client.Chat(context.Background(), Request{
		Model:            "gpt-5.4",
		Messages:         []Message{{Role: "user", Content: "hi"}},
		MaxTokens:        32,
		ReasoningEffort:  "off",
		MaxThinkingToken: "128",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}

func TestCodexClientSendsReasoningPayloadWhenSetToNone(t *testing.T) {
	client := &Client{impl: &responsesClient{httpJSONClient: &httpJSONClient{
		endpoint: "https://api.example.test/responses",
		apiKey:   "test-key",
		headers:  map[string]string{},
		httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			reasoning, ok := payload["reasoning"].(map[string]any)
			if !ok || reasoning["effort"] != "none" {
				t.Fatalf("expected reasoning.effort=none, got %#v", payload["reasoning"])
			}

			body, err := json.Marshal(map[string]any{
				"output": []map[string]any{
					{
						"type": "message",
						"role": "assistant",
						"content": []map[string]any{{
							"type": "output_text",
							"text": "ok",
						}},
					},
				},
			})
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		})},
	}}}

	_, err := client.Chat(context.Background(), Request{
		Model:           "gpt-5.4",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		MaxTokens:       32,
		ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}

func TestNewClientRoutesDeepSeekToChatCompletions(t *testing.T) {
	client := NewClient(
		"https://api.deepseek.com/v1",
		"test-key",
		APITypeDeepSeek,
		map[string]string{},
		false,
		false,
		30*time.Second,
	)

	chatClient, ok := client.impl.(*chatCompletionsClient)
	if !ok {
		t.Fatalf("expected chat completions client for deepseek, got %T", client.impl)
	}
	if chatClient.normalizeMessage == nil {
		t.Fatal("expected normalizeMessage to be set for deepseek client")
	}
}

func TestNormalizeDeepSeekMessageKeepsReasoningForToolCalls(t *testing.T) {
	message := Message{
		Role:             "assistant",
		Content:          "content",
		ReasoningContent: "Let me think about which tool to use...",
		ToolCalls: []ToolCall{{
			ID:   "call_123",
			Type: "function",
			Function: ToolFunctionCall{
				Name:      "read_file",
				Arguments: `{"path":"foo.go"}`,
			},
		}},
	}

	normalized := normalizeDeepSeekMessage(message)

	if normalized.ReasoningContent == "" {
		t.Fatal("expected reasoning_content to be preserved for messages with tool calls")
	}
}

func TestNormalizeDeepSeekMessageStripsReasoningWithoutToolCalls(t *testing.T) {
	message := Message{
		Role:             "assistant",
		Content:          "The answer is 42.",
		ReasoningContent: "Let me think about this...",
	}

	normalized := normalizeDeepSeekMessage(message)

	if normalized.ReasoningContent != "" {
		t.Fatalf("expected reasoning_content to be stripped for messages without tool calls, got %q", normalized.ReasoningContent)
	}
}

func TestNormalizeDeepSeekMessageIgnoresNonAssistantMessages(t *testing.T) {
	message := Message{
		Role:             "user",
		Content:          "Hello",
		ReasoningContent: "This should be ignored",
	}

	normalized := normalizeDeepSeekMessage(message)

	if normalized.ReasoningContent == "" {
		t.Fatal("expected reasoning_content to be untouched for non-assistant messages")
	}
}

func TestNormalizeDeepSeekMessageNoOpWhenNoReasoning(t *testing.T) {
	message := Message{
		Role:      "assistant",
		Content:   "Just a simple reply",
		ToolCalls: nil,
	}

	normalized := normalizeDeepSeekMessage(message)

	if normalized.ReasoningContent != "" {
		t.Fatal("expected no reasoning_content change when it was already empty")
	}
}

func TestResponseMessageCapturesReasoningContent(t *testing.T) {
	message := responseMessage{
		Role:             "assistant",
		Content:          flexibleContent{text: "final answer"},
		ReasoningContent: "step by step reasoning",
		ToolCalls: []ToolCall{{
			ID:   "call_456",
			Type: "function",
			Function: ToolFunctionCall{
				Name:      "list_dir",
				Arguments: `{"path":"."}`,
			},
		}},
	}.toMessage()

	if message.ReasoningContent != "step by step reasoning" {
		t.Fatalf("expected reasoning_content %q, got %q", "step by step reasoning", message.ReasoningContent)
	}
	if message.Content != "final answer" {
		t.Fatalf("expected content %q, got %q", "final answer", message.Content)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call_456" {
		t.Fatalf("expected tool call to be preserved, got %#v", message.ToolCalls)
	}
}

func TestDeepSeekClientSendsReasoningContentForToolCalls(t *testing.T) {
	client := &Client{impl: &chatCompletionsClient{
		httpJSONClient: &httpJSONClient{
			endpoint: "https://api.deepseek.com/v1/chat/completions",
			apiKey:   "test-key",
			headers:  map[string]string{},
			httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}

				messages, ok := payload["messages"].([]any)
				if !ok || len(messages) == 0 {
					t.Fatalf("expected messages in payload")
				}

				assistantMsg, ok := messages[0].(map[string]any)
				if !ok {
					t.Fatalf("expected first message to be a map")
				}

				if assistantMsg["role"] != "assistant" {
					t.Fatalf("expected assistant role, got %v", assistantMsg["role"])
				}

				reasoning, ok := assistantMsg["reasoning_content"].(string)
				if !ok || reasoning == "" {
					t.Fatal("expected reasoning_content to be included for assistant message with tool calls")
				}

				body, err := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"message": map[string]any{
							"role":    "assistant",
							"content": "Here is the result.",
						},
					}},
				})
				if err != nil {
					t.Fatalf("marshal response: %v", err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			})},
		},
		normalizeMessage: normalizeDeepSeekMessage,
	}}

	_, err := client.Chat(context.Background(), Request{
		Model: "deepseek-reasoner",
		Messages: []Message{{
			Role:             "assistant",
			Content:          "content",
			ReasoningContent: "I should use the read_file tool...",
			ToolCalls: []ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: ToolFunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"foo.go"}`,
				},
			}},
		}},
		Temperature: 0.2,
		MaxTokens:   32,
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}

func TestDeepSeekClientStripsReasoningContentWithoutToolCalls(t *testing.T) {
	client := &Client{impl: &chatCompletionsClient{
		httpJSONClient: &httpJSONClient{
			endpoint: "https://api.deepseek.com/v1/chat/completions",
			apiKey:   "test-key",
			headers:  map[string]string{},
			httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}

				messages, ok := payload["messages"].([]any)
				if !ok || len(messages) == 0 {
					t.Fatalf("expected messages in payload")
				}

				assistantMsg, ok := messages[0].(map[string]any)
				if !ok {
					t.Fatalf("expected first message to be a map")
				}

				if _, ok := assistantMsg["reasoning_content"]; ok {
					t.Fatal("expected reasoning_content to be omitted for assistant message without tool calls")
				}

				body, err := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"message": map[string]any{
							"role":    "assistant",
							"content": "Here is the result.",
						},
					}},
				})
				if err != nil {
					t.Fatalf("marshal response: %v", err)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			})},
		},
		normalizeMessage: normalizeDeepSeekMessage,
	}}

	_, err := client.Chat(context.Background(), Request{
		Model: "deepseek-reasoner",
		Messages: []Message{{
			Role:             "assistant",
			Content:          "The answer is 42.",
			ReasoningContent: "Let me think step by step...",
		}},
		Temperature: 0.2,
		MaxTokens:   32,
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}
