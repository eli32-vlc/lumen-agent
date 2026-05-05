package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	APITypeOpenAI   = "openai"
	APITypeCodex    = "codex"
	APITypeDeepSeek = "deepseek"
)

type Client struct {
	impl chatClient
}

type chatClient interface {
	Chat(ctx context.Context, req Request) (Message, error)
}

type httpJSONClient struct {
	endpoint   string
	apiKey     string
	headers    map[string]string
	httpClient *http.Client
}

type Request struct {
	Model            string
	Messages         []Message
	Tools            []ToolDefinition
	Temperature      float64
	MaxTokens        int
	ReasoningEffort  string
	MaxThinkingToken string
}

type ContentPartType string

const (
	ContentPartText     ContentPartType = "text"
	ContentPartImageURL ContentPartType = "image_url"
)

type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL string          `json:"image_url,omitempty"`
}

type Message struct {
	Role             string           `json:"role"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Parts            []ContentPart    `json:"parts,omitempty"`
	Timestamp        string           `json:"timestamp,omitempty"`
	Name             string           `json:"name,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall       `json:"tool_calls,omitempty"`
	ResponseItems    []map[string]any `json:"response_items,omitempty"`
	Usage            map[string]any   `json:"usage,omitempty"`
	RequestPayload   map[string]any   `json:"request_payload,omitempty"`
	RawResponse      map[string]any   `json:"raw_response,omitempty"`
	OutputTokens     int              `json:"output_tokens,omitempty"`
	ReasoningTokens  int              `json:"reasoning_tokens,omitempty"`
}

type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

type ToolFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolFunctionCall `json:"function"`
}

type ToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func NewClient(baseURL string, apiKey string, apiType string, headers map[string]string, kimiNoThink bool, glmNoThink bool, timeout time.Duration) *Client {
	switch strings.TrimSpace(strings.ToLower(apiType)) {
	case "", APITypeOpenAI:
		return &Client{impl: &chatCompletionsClient{
			httpJSONClient: newHTTPJSONClient(baseURL, "/chat/completions", apiKey, headers, timeout),
			extraBody:      buildOpenAIExtraBody(kimiNoThink, glmNoThink),
		}}
	case APITypeDeepSeek:
		return &Client{impl: &chatCompletionsClient{
			httpJSONClient:    newHTTPJSONClient(baseURL, "/chat/completions", apiKey, headers, timeout),
			extraBody:         map[string]any{},
			normalizeMessage:  normalizeDeepSeekMessage,
		}}
	case APITypeCodex:
		return &Client{impl: &responsesClient{httpJSONClient: newHTTPJSONClient(baseURL, "/responses", apiKey, headers, timeout)}}
	default:
		return &Client{impl: &chatCompletionsClient{
			httpJSONClient: newHTTPJSONClient(baseURL, "/chat/completions", apiKey, headers, timeout),
			extraBody:      buildOpenAIExtraBody(kimiNoThink, glmNoThink),
		}}
	}
}

func (c *Client) Chat(ctx context.Context, req Request) (Message, error) {
	return c.impl.Chat(ctx, req)
}

func newHTTPJSONClient(baseURL string, path string, apiKey string, headers map[string]string, timeout time.Duration) *httpJSONClient {
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	clonedHeaders := make(map[string]string, len(headers))
	for key, value := range headers {
		clonedHeaders[key] = value
	}

	return &httpJSONClient{
		endpoint: strings.TrimRight(baseURL, "/") + path,
		apiKey:   apiKey,
		headers:  clonedHeaders,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *httpJSONClient) postJSON(ctx context.Context, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var apiErr apiErrorEnvelope
		if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != nil {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return data, nil
}

func (c *httpJSONClient) doJSONRequest(ctx context.Context, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	return resp, nil
}

type chatCompletionsClient struct {
	*httpJSONClient
	extraBody        map[string]any
	normalizeMessage func(Message) Message
}

func buildOpenAIExtraBody(kimiNoThink bool, glmNoThink bool) map[string]any {
	if !kimiNoThink && !glmNoThink {
		return map[string]any{}
	}

	extraBody := make(map[string]any)
	if kimiNoThink {
		extraBody["chat_template_kwargs"] = map[string]any{
			"thinking": false,
		}
	}
	if glmNoThink {
		extraBody["thinking"] = map[string]any{
			"type": "disabled",
		}
		extraBody["clear_thinking"] = true
	}
	return extraBody
}

func normalizeDeepSeekMessage(message Message) Message {
	if message.Role != "assistant" || message.ReasoningContent == "" {
		return message
	}
	if len(message.ToolCalls) > 0 {
		return message
	}
	message.ReasoningContent = ""
	return message
}

func (c *chatCompletionsClient) Chat(ctx context.Context, req Request) (Message, error) {
	messages := req.Messages
	if c.normalizeMessage != nil {
		normalized := make([]Message, len(messages))
		for i, msg := range messages {
			normalized[i] = c.normalizeMessage(msg)
		}
		messages = normalized
	}
	payload := map[string]any{
		"model":       req.Model,
		"messages":    buildChatCompletionsMessages(messages),
		"temperature": req.Temperature,
		"stream":      false,
	}

	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if effort := normalizedReasoningEffort(req.ReasoningEffort); effort != "" {
		payload["reasoning_effort"] = effort
	}
	if maxThinkingTokens, ok := normalizedMaxThinkingToken(req.MaxThinkingToken, req.ReasoningEffort); ok {
		payload["max_thinking_tokens"] = maxThinkingTokens
	}

	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
		payload["tool_choice"] = "auto"
	}
	for key, value := range c.extraBody {
		payload[key] = value
	}

	data, err := c.postJSON(ctx, payload)
	if err != nil {
		return Message{}, err
	}
	rawResponse := mustJSONObject(data)

	var parsed chatCompletionResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return Message{}, fmt.Errorf("response did not include choices")
	}

	message := parsed.Choices[0].Message.toMessage()
	message.RequestPayload = cloneJSONObject(payload)
	message.RawResponse = rawResponse
	message.Usage = extractUsage(rawResponse)
	message.OutputTokens = extractOutputTokens(message.Usage)
	message.ReasoningTokens = extractReasoningTokens(message.Usage)
	return message, nil
}

type responsesClient struct {
	*httpJSONClient
}

func (c *responsesClient) Chat(ctx context.Context, req Request) (Message, error) {
	payload := c.buildPayload(req)
	return c.sendPayload(ctx, payload)
}

func (c *responsesClient) buildPayload(req Request) map[string]any {
	payload := map[string]any{
		"model": req.Model,
		"input": buildResponsesInput(req.Messages),
		"text": map[string]any{
			"format": map[string]any{"type": "text"},
		},
	}

	if req.MaxTokens > 0 {
		payload["max_output_tokens"] = req.MaxTokens
	}
	if reasoningPayload := buildResponsesReasoningPayload(req.ReasoningEffort, req.MaxThinkingToken); len(reasoningPayload) > 0 {
		payload["reasoning"] = reasoningPayload
	}

	if len(req.Tools) > 0 {
		payload["tools"] = buildResponsesTools(req.Tools)
		payload["tool_choice"] = "auto"
	}
	return payload
}

func (c *responsesClient) sendPayload(ctx context.Context, payload map[string]any) (Message, error) {
	data, err := c.postJSON(ctx, payload)
	if err != nil {
		if shouldRetryResponsesAsStream(err) {
			return c.chatStream(ctx, payload)
		}
		return Message{}, err
	}

	var parsed responsesCreateResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode response: %w", err)
	}
	rawResponse := mustJSONObject(data)

	message, err := parsed.toMessage()
	if err != nil {
		return Message{}, err
	}
	message.RequestPayload = cloneJSONObject(payload)
	message.RawResponse = rawResponse
	message.Usage = extractUsage(rawResponse)
	message.OutputTokens = extractOutputTokens(message.Usage)
	message.ReasoningTokens = extractReasoningTokens(message.Usage)
	return message, nil
}

func (c *responsesClient) chatStream(ctx context.Context, payload map[string]any) (Message, error) {
	payload = clonePayload(payload)
	payload["stream"] = true

	resp, err := c.doJSONRequest(ctx, payload)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return Message{}, fmt.Errorf("read response: %w", readErr)
		}

		var apiErr apiErrorEnvelope
		if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != nil {
			return Message{}, fmt.Errorf("API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return Message{}, fmt.Errorf("API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	parsed, err := decodeResponsesStream(resp.Body)
	if err != nil {
		return Message{}, err
	}

	message, err := parsed.toMessage()
	if err != nil {
		return Message{}, err
	}
	rawResponse := mustJSONObjectFromValue(parsed)
	message.RequestPayload = cloneJSONObject(payload)
	message.RawResponse = rawResponse
	message.Usage = extractUsage(rawResponse)
	message.OutputTokens = extractOutputTokens(message.Usage)
	message.ReasoningTokens = extractReasoningTokens(message.Usage)
	return message, nil
}

func buildChatCompletionsMessages(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		result = append(result, chatCompletionsMessage(message))
	}
	return result
}

func chatCompletionsMessage(message Message) map[string]any {
	item := map[string]any{
		"role": message.Role,
	}
	if strings.TrimSpace(message.Name) != "" {
		item["name"] = message.Name
	}
	if strings.TrimSpace(message.ToolCallID) != "" {
		item["tool_call_id"] = message.ToolCallID
	}
	if len(message.ToolCalls) > 0 {
		item["tool_calls"] = message.ToolCalls
	}
	if reasoning := strings.TrimSpace(message.ReasoningContent); reasoning != "" {
		item["reasoning_content"] = reasoning
	}

	switch message.Role {
	case "system", "user", "assistant":
		if parts := chatCompletionsContentParts(message); len(parts) > 0 {
			item["content"] = parts
		} else {
			item["content"] = annotateMessageContent(message.Content, message.Timestamp)
		}
	default:
		item["content"] = annotateMessageContent(message.Content, message.Timestamp)
	}
	return item
}

func chatCompletionsContentParts(message Message) []map[string]any {
	if len(message.Parts) == 0 {
		return nil
	}
	parts := make([]map[string]any, 0, len(message.Parts)+1)
	if prefix := messageTimestampPrefix(message.Timestamp); prefix != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": prefix,
		})
	}
	for _, part := range message.Parts {
		switch part.Type {
		case ContentPartText:
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			parts = append(parts, map[string]any{
				"type": "text",
				"text": text,
			})
		case ContentPartImageURL:
			imageURL := strings.TrimSpace(part.ImageURL)
			if imageURL == "" {
				continue
			}
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": imageURL,
				},
			})
		}
	}
	return parts
}

func buildResponsesInput(messages []Message) []map[string]any {
	items := make([]map[string]any, 0, len(messages))
	seenFunctionCallIDs := make(map[string]struct{})
	for _, message := range messages {
		for _, item := range responseInputItemsForMessage(message) {
			itemType, _ := item["type"].(string)
			switch itemType {
			case "function_call":
				callID, _ := item["call_id"].(string)
				if strings.TrimSpace(callID) != "" {
					seenFunctionCallIDs[callID] = struct{}{}
				}
				items = append(items, item)
			case "function_call_output":
				callID, _ := item["call_id"].(string)
				if _, ok := seenFunctionCallIDs[strings.TrimSpace(callID)]; !ok {
					continue
				}
				items = append(items, item)
			default:
				items = append(items, item)
			}
		}
	}
	return items
}

func responseInputItemsForMessage(message Message) []map[string]any {
	if message.Role == "assistant" && len(message.ResponseItems) > 0 {
		return cloneResponseItems(message.ResponseItems)
	}

	items := make([]map[string]any, 0, 1+len(message.ToolCalls))

	content := strings.TrimSpace(message.Content)
	switch message.Role {
	case "system", "user", "assistant":
		if parts := responsesContentParts(message); len(parts) > 0 {
			items = append(items, map[string]any{
				"type":    "message",
				"role":    message.Role,
				"content": parts,
			})
		} else if content != "" {
			items = append(items, map[string]any{
				"type":    "message",
				"role":    message.Role,
				"content": annotateMessageContent(content, message.Timestamp),
			})
		}
	case "tool":
		items = append(items, map[string]any{
			"type":    "function_call_output",
			"call_id": message.ToolCallID,
			"output":  annotateMessageContent(message.Content, message.Timestamp),
		})
	}

	if message.Role == "assistant" {
		for _, call := range message.ToolCalls {
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   call.ID,
				"name":      call.Function.Name,
				"arguments": call.Function.Arguments,
			})
		}
	}

	return items
}

func responsesContentParts(message Message) []map[string]any {
	if len(message.Parts) == 0 {
		return nil
	}
	parts := make([]map[string]any, 0, len(message.Parts)+1)
	if prefix := messageTimestampPrefix(message.Timestamp); prefix != "" {
		parts = append(parts, map[string]any{
			"type": "input_text",
			"text": prefix,
		})
	}
	for _, part := range message.Parts {
		switch part.Type {
		case ContentPartText:
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			parts = append(parts, map[string]any{
				"type": "input_text",
				"text": text,
			})
		case ContentPartImageURL:
			imageURL := strings.TrimSpace(part.ImageURL)
			if imageURL == "" {
				continue
			}
			parts = append(parts, map[string]any{
				"type":      "input_image",
				"image_url": imageURL,
			})
		}
	}
	return parts
}

func buildResponsesTools(tools []ToolDefinition) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]any{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  tool.Function.Parameters,
		})
	}
	return result
}

func annotateMessageContent(content string, timestamp string) string {
	prefix := messageTimestampPrefix(timestamp)
	if prefix == "" {
		return content
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return prefix
	}
	return prefix + "\n" + content
}

func messageTimestampPrefix(timestamp string) string {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp == "" {
		return ""
	}
	return "[message_time " + timestamp + "]"
}

func clonePayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func normalizedReasoningEffort(effort string) string {
	effort = strings.TrimSpace(strings.ToLower(effort))
	if effort == "" || effort == "off" {
		return ""
	}
	return effort
}

func normalizedMaxThinkingToken(value string, reasoningEffort string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "off" {
		return 0, false
	}
	if strings.TrimSpace(strings.ToLower(reasoningEffort)) == "off" {
		return 0, false
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

func buildResponsesReasoningPayload(reasoningEffort string, maxThinkingToken string) map[string]any {
	payload := map[string]any{}
	if effort := normalizedReasoningEffort(reasoningEffort); effort != "" {
		payload["effort"] = effort
	}
	if maxThinkingTokens, ok := normalizedMaxThinkingToken(maxThinkingToken, reasoningEffort); ok {
		payload["max_thinking_tokens"] = maxThinkingTokens
	}
	return payload
}

func shouldRetryResponsesAsStream(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "only support stream")
}

type chatCompletionResponse struct {
	Choices []chatChoice `json:"choices"`
}

type chatChoice struct {
	Message responseMessage `json:"message"`
}

type responseMessage struct {
	Role             string          `json:"role"`
	Content          flexibleContent `json:"content"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	Name             string          `json:"name,omitempty"`
}

func (m responseMessage) toMessage() Message {
	return Message{
		Role:             m.Role,
		Content:          StripMessageTimeMetadata(m.Content.String()),
		ReasoningContent: strings.TrimSpace(m.ReasoningContent),
		Name:             m.Name,
		ToolCalls:        m.ToolCalls,
	}
}

type responsesCreateResponse struct {
	Output []json.RawMessage `json:"output"`
}

type responsesOutputItem struct {
	Type      string                   `json:"type"`
	Role      string                   `json:"role,omitempty"`
	Name      string                   `json:"name,omitempty"`
	CallID    string                   `json:"call_id,omitempty"`
	Arguments string                   `json:"arguments,omitempty"`
	Content   []responsesOutputContent `json:"content,omitempty"`
}

type responsesOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesStreamCompletedEvent struct {
	Type     string                  `json:"type"`
	Response responsesCreateResponse `json:"response"`
}

func (r responsesCreateResponse) toMessage() (Message, error) {
	message := Message{Role: "assistant"}
	var content strings.Builder

	for _, raw := range r.Output {
		var item responsesOutputItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return Message{}, fmt.Errorf("decode response output item: %w", err)
		}

		message.ResponseItems = append(message.ResponseItems, mustJSONObject(raw))

		switch item.Type {
		case "message":
			if item.Role != "" {
				message.Role = item.Role
			}
			text := item.text()
			if text == "" {
				continue
			}
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString(text)
		case "function_call":
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ToolFunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}

	message.Content = StripMessageTimeMetadata(content.String())
	if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
		return Message{}, fmt.Errorf("response did not include assistant output")
	}

	return message, nil
}

func decodeResponsesStream(body io.Reader) (responsesCreateResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var dataLines []string
	var completed responsesCreateResponse
	var sawCompleted bool

	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}

		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if payload == "" || payload == "[DONE]" {
			return nil
		}

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			return fmt.Errorf("decode stream event: %w", err)
		}

		switch envelope.Type {
		case "response.completed":
			var event responsesStreamCompletedEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				return fmt.Errorf("decode completed stream event: %w", err)
			}
			completed = event.Response
			sawCompleted = true
		case "error", "response.failed":
			var event struct {
				Type  string `json:"type"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				return fmt.Errorf("decode error stream event: %w", err)
			}
			if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
				return fmt.Errorf("stream error: %s", event.Error.Message)
			}
			return fmt.Errorf("stream error event: %s", envelope.Type)
		}

		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flush(); err != nil {
				return responsesCreateResponse{}, err
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") {
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return responsesCreateResponse{}, fmt.Errorf("read stream: %w", err)
	}
	if err := flush(); err != nil {
		return responsesCreateResponse{}, err
	}
	if !sawCompleted {
		return responsesCreateResponse{}, fmt.Errorf("stream ended without response.completed")
	}
	return completed, nil
}

func cloneResponseItems(items []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, cloneJSONObject(item))
	}
	return cloned
}

func cloneJSONObject(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}

	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = cloneJSONValue(item)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONObject(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneJSONValue(item)
		}
		return cloned
	default:
		return typed
	}
}

func mustJSONObject(raw json.RawMessage) map[string]any {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return map[string]any{}
	}
	return item
}

func mustJSONObjectFromValue(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	return mustJSONObject(data)
}

func extractUsage(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	usage, _ := raw["usage"].(map[string]any)
	return cloneJSONObject(usage)
}

func extractOutputTokens(usage map[string]any) int {
	if usage == nil {
		return 0
	}
	for _, key := range []string{"output_tokens", "completion_tokens"} {
		if count, ok := intFromAny(usage[key]); ok {
			return count
		}
	}
	return 0
}

func extractReasoningTokens(usage map[string]any) int {
	if usage == nil {
		return 0
	}
	if count, ok := intFromAny(usage["reasoning_tokens"]); ok {
		return count
	}
	for _, key := range []string{"output_tokens_details", "completion_tokens_details"} {
		details, _ := usage[key].(map[string]any)
		if details == nil {
			continue
		}
		if count, ok := intFromAny(details["reasoning_tokens"]); ok {
			return count
		}
	}
	return 0
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(number), true
	default:
		return 0, false
	}
}

func (item responsesOutputItem) text() string {
	var builder strings.Builder
	for _, part := range item.Content {
		switch part.Type {
		case "output_text", "text":
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

type flexibleContent struct {
	text string
}

func (c *flexibleContent) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		c.text = ""
		return nil
	}

	var plain string
	if err := json.Unmarshal(data, &plain); err == nil {
		c.text = plain
		return nil
	}

	var parts []struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		InputText string `json:"input_text"`
	}
	if err := json.Unmarshal(data, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			switch part.Type {
			case "text":
				builder.WriteString(part.Text)
			case "input_text":
				builder.WriteString(part.InputText)
			}
		}
		c.text = builder.String()
		return nil
	}

	return fmt.Errorf("unsupported content shape: %s", trimmed)
}

func (c flexibleContent) String() string {
	return c.text
}

type apiErrorEnvelope struct {
	Error *apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
}
