package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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

			body, err := json.Marshal(map[string]any{
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
		Model:           "gpt-4.1-mini",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		Temperature:     0.2,
		MaxTokens:       32,
		ReasoningEffort: "medium",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if message.Content != "hello from chat completions" {
		t.Fatalf("unexpected content %q", message.Content)
	}
}

func TestOpenAIClientOmitsReasoningEffortWhenSetToNone(t *testing.T) {
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
		Temperature:     0.2,
		MaxTokens:       64,
		ReasoningEffort: "high",
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
	if message.ToolCalls[0].ID != "call_456" {
		t.Fatalf("expected tool call ID call_456, got %q", message.ToolCalls[0].ID)
	}
	if message.ToolCalls[0].Function.Name != "write_file" {
		t.Fatalf("unexpected tool name %q", message.ToolCalls[0].Function.Name)
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

func TestCodexClientOmitsReasoningPayloadWhenSetToNone(t *testing.T) {
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
		Model:           "gpt-5.4",
		Messages:        []Message{{Role: "user", Content: "hi"}},
		MaxTokens:       32,
		ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
}
