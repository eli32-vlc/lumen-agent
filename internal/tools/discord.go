package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type discordToolContextKey struct{}

type DiscordToolContext struct {
	GuildID   string
	ChannelID string
	UserID    string
	MessageID string
}

func WithDiscordToolContext(ctx context.Context, metadata DiscordToolContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, discordToolContextKey{}, metadata)
}

func DiscordToolContextFromContext(ctx context.Context) (DiscordToolContext, bool) {
	if ctx == nil {
		return DiscordToolContext{}, false
	}
	metadata, ok := ctx.Value(discordToolContextKey{}).(DiscordToolContext)
	return metadata, ok
}

func (r *Registry) registerDiscordTool() {
	r.register(
		"send_discord_message",
		"Send a plain Discord message to the current channel, an allowlisted channel, or an allowlisted DM recipient.",
		objectSchema(map[string]any{
			"content":             stringSchema("Message content to send."),
			"channel_id":          stringSchema("Optional target Discord channel ID."),
			"user_id":             stringSchema("Optional Discord user ID to DM. Requires allowlist access."),
			"reply_to_message_id": stringSchema("Optional message ID to reply to."),
		}, "content"),
		r.handleSendDiscordMessage,
	)

	r.register(
		"add_discord_reaction",
		"Add a reaction to a Discord message in the current channel or an allowlisted channel.",
		objectSchema(map[string]any{
			"emoji":      stringSchema("Unicode emoji or custom emoji tag to react with."),
			"message_id": stringSchema("Optional target message ID. Defaults to the triggering message when available."),
			"channel_id": stringSchema("Optional target channel ID."),
		}, "emoji"),
		r.handleAddDiscordReaction,
	)

	r.register(
		"send_discord_file",
		"Upload a file from the workspace to the current channel, an allowlisted channel, or an allowlisted DM recipient.",
		objectSchema(map[string]any{
			"path":       stringSchema("Path to the file inside the workspace root."),
			"channel_id": stringSchema("Optional target Discord channel ID."),
			"user_id":    stringSchema("Optional Discord user ID to DM. Requires allowlist access."),
			"message":    stringSchema("Optional message text to include with the file."),
		}, "path"),
		r.handleSendDiscordFile,
	)

	r.register(
		"discord_api_request",
		"Make a Discord REST API request for moderation and server management tasks such as bans, channel changes, role updates, and emoji management.",
		objectSchema(map[string]any{
			"method":      stringSchema("HTTP method such as GET, POST, PATCH, PUT, or DELETE."),
			"path":        stringSchema("Discord API path beginning with /. Example: /guilds/123/bans/456"),
			"body":        map[string]any{"type": "object", "description": "Optional JSON body.", "additionalProperties": true},
			"reason":      stringSchema("Optional audit log reason."),
			"image_path":  stringSchema("Optional workspace image path to convert to a data URI and place into the JSON body."),
			"image_field": stringSchema("JSON field name for the encoded image when image_path is used."),
		}, "method", "path"),
		r.handleDiscordAPIRequest,
	)
}

func (r *Registry) handleSendDiscordMessage(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Content          string `json:"content"`
		ChannelID        string `json:"channel_id"`
		UserID           string `json:"user_id"`
		ReplyToMessageID string `json:"reply_to_message_id"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return "", fmt.Errorf("content must not be empty")
	}

	target, err := r.resolveDiscordTarget(ctx, input.ChannelID, input.UserID)
	if err != nil {
		return "", err
	}

	body := map[string]any{
		"content": content,
		"allowed_mentions": map[string]any{
			"parse": []string{},
		},
	}
	if replyTo := strings.TrimSpace(input.ReplyToMessageID); replyTo != "" {
		body["message_reference"] = map[string]any{
			"message_id": replyTo,
			"channel_id": target.ChannelID,
		}
	}

	respBody, err := r.discordJSONRequest(ctx, http.MethodPost, "/channels/"+target.ChannelID+"/messages", body)
	if err != nil {
		return "", err
	}

	type discordMessage struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	message := discordMessage{}
	_ = json.Unmarshal(respBody, &message)

	return jsonResult(map[string]any{
		"channel_id": target.ChannelID,
		"user_id":    target.UserID,
		"message_id": message.ID,
		"content":    content,
	})
}

func (r *Registry) handleAddDiscordReaction(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Emoji     string `json:"emoji"`
		MessageID string `json:"message_id"`
		ChannelID string `json:"channel_id"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	emoji := strings.TrimSpace(input.Emoji)
	if emoji == "" {
		return "", fmt.Errorf("emoji must not be empty")
	}

	metadata, _ := DiscordToolContextFromContext(ctx)
	channelID := strings.TrimSpace(input.ChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(metadata.ChannelID)
	}
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required when no active Discord channel context is available")
	}
	if !r.cfg.DiscordChannelAllowed(channelID, metadata.ChannelID) {
		return "", fmt.Errorf("channel_id %q is not allowed for outbound Discord actions", channelID)
	}

	messageID := strings.TrimSpace(input.MessageID)
	if messageID == "" {
		messageID = strings.TrimSpace(metadata.MessageID)
	}
	if messageID == "" {
		return "", fmt.Errorf("message_id is required when no active Discord message context is available")
	}

	path := fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, url.PathEscape(emoji))
	if _, err := r.discordRequest(ctx, http.MethodPut, path, nil, ""); err != nil {
		return "", err
	}

	return jsonResult(map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji":      emoji,
	})
}

func (r *Registry) handleSendDiscordFile(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Path      string `json:"path"`
		ChannelID string `json:"channel_id"`
		UserID    string `json:"user_id"`
		Message   string `json:"message"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	path, err := r.resolvePath(input.Path)
	if err != nil {
		return "", err
	}
	if err := r.ensurePathAccessible(path); err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", r.relPath(path))
	}

	maxFileBytes := r.cfg.Tools.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = 1 << 20
	}
	if info.Size() > maxFileBytes {
		return "", fmt.Errorf("%s exceeds max_file_bytes", r.relPath(path))
	}

	target, err := r.resolveDiscordTarget(ctx, input.ChannelID, input.UserID)
	if err != nil {
		return "", err
	}

	authHeader, err := r.cfg.ResolveDiscordAuthorizationHeader()
	if err != nil {
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	payloadJSON, err := json.Marshal(map[string]any{
		"content": strings.TrimSpace(input.Message),
		"allowed_mentions": map[string]any{
			"parse": []string{},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encode Discord payload: %w", err)
	}

	payloadField, err := writer.CreateFormField("payload_json")
	if err != nil {
		return "", fmt.Errorf("create payload field: %w", err)
	}
	if _, err := payloadField.Write(payloadJSON); err != nil {
		return "", fmt.Errorf("write payload field: %w", err)
	}

	filePart, err := writer.CreateFormFile("files[0]", filepath.Base(path))
	if err != nil {
		return "", fmt.Errorf("create file part: %w", err)
	}
	if _, err := io.Copy(filePart, file); err != nil {
		return "", fmt.Errorf("copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("finalize multipart payload: %w", err)
	}

	endpoint := strings.TrimRight(r.discordAPIBase, "/") + "/channels/" + target.ChannelID + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("create Discord request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	client := r.discordClient
	if client == nil {
		client = &http.Client{}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send Discord request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read Discord response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		reason := strings.TrimSpace(string(respBody))
		if reason == "" {
			reason = http.StatusText(resp.StatusCode)
		}
		return "", fmt.Errorf("Discord API error (%d): %s", resp.StatusCode, reason)
	}

	type discordAttachment struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
		URL      string `json:"url"`
	}
	type discordMessage struct {
		ID          string              `json:"id"`
		ChannelID   string              `json:"channel_id"`
		Attachments []discordAttachment `json:"attachments"`
	}

	message := discordMessage{}
	_ = json.Unmarshal(respBody, &message)

	return jsonResult(map[string]any{
		"path":        r.relPath(path),
		"channel_id":  target.ChannelID,
		"user_id":     target.UserID,
		"message_id":  message.ID,
		"bytes":       info.Size(),
		"attachments": message.Attachments,
	})
}

type discordTarget struct {
	ChannelID string
	UserID    string
}

func (r *Registry) resolveDiscordTarget(ctx context.Context, channelID string, userID string) (discordTarget, error) {
	channelID = strings.TrimSpace(channelID)
	userID = strings.TrimSpace(userID)
	if channelID != "" && userID != "" {
		return discordTarget{}, fmt.Errorf("channel_id and user_id cannot both be set")
	}

	metadata, _ := DiscordToolContextFromContext(ctx)
	if userID != "" {
		if !r.cfg.DMAllowedForUser(userID) {
			return discordTarget{}, fmt.Errorf("user_id %q is not allowed for Discord DMs", userID)
		}
		dmChannelID, err := r.openDiscordDM(ctx, userID)
		if err != nil {
			return discordTarget{}, err
		}
		return discordTarget{ChannelID: dmChannelID, UserID: userID}, nil
	}

	if channelID == "" {
		channelID = strings.TrimSpace(metadata.ChannelID)
	}
	if channelID == "" {
		return discordTarget{}, fmt.Errorf("channel_id is required when no active Discord channel context is available")
	}
	if !r.cfg.DiscordChannelAllowed(channelID, metadata.ChannelID) {
		return discordTarget{}, fmt.Errorf("channel_id %q is not allowed for outbound Discord actions", channelID)
	}
	return discordTarget{ChannelID: channelID}, nil
}

func (r *Registry) openDiscordDM(ctx context.Context, userID string) (string, error) {
	respBody, err := r.discordJSONRequest(ctx, http.MethodPost, "/users/@me/channels", map[string]any{
		"recipient_id": userID,
	})
	if err != nil {
		return "", err
	}

	type dmChannel struct {
		ID string `json:"id"`
	}
	channel := dmChannel{}
	if err := json.Unmarshal(respBody, &channel); err != nil {
		return "", fmt.Errorf("decode Discord DM channel: %w", err)
	}
	if strings.TrimSpace(channel.ID) == "" {
		return "", fmt.Errorf("Discord DM channel response did not include an id")
	}
	return strings.TrimSpace(channel.ID), nil
}

func (r *Registry) discordJSONRequest(ctx context.Context, method string, path string, payload any) ([]byte, error) {
	body := io.Reader(nil)
	contentType := ""
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode Discord request: %w", err)
		}
		body = bytes.NewReader(data)
		contentType = "application/json"
	}
	return r.discordRequest(ctx, method, path, body, contentType)
}

func (r *Registry) discordRequest(ctx context.Context, method string, path string, body io.Reader, contentType string) ([]byte, error) {
	return r.discordRequestWithHeaders(ctx, method, path, body, contentType, nil)
}

func (r *Registry) discordRequestWithHeaders(ctx context.Context, method string, path string, body io.Reader, contentType string, headers map[string]string) ([]byte, error) {
	authHeader, err := r.cfg.ResolveDiscordAuthorizationHeader()
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(r.discordAPIBase, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create Discord request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	client := r.discordClient
	if client == nil {
		client = &http.Client{}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send Discord request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read Discord response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		reason := strings.TrimSpace(string(respBody))
		if reason == "" {
			reason = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("Discord API error (%d): %s", resp.StatusCode, reason)
	}
	return respBody, nil
}

func (r *Registry) handleDiscordAPIRequest(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Method     string         `json:"method"`
		Path       string         `json:"path"`
		Body       map[string]any `json:"body"`
		Reason     string         `json:"reason"`
		ImagePath  string         `json:"image_path"`
		ImageField string         `json:"image_field"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		return "", fmt.Errorf("method must not be empty")
	}
	path := strings.TrimSpace(input.Path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must start with '/'")
	}
	if err := r.authorizeDiscordRESTPath(ctx, path, input.Body); err != nil {
		return "", err
	}

	body := input.Body
	if body == nil {
		body = map[string]any{}
	}
	if strings.TrimSpace(input.ImagePath) != "" {
		field := strings.TrimSpace(input.ImageField)
		if field == "" {
			field = "image"
		}
		dataURI, err := r.readImageDataURI(input.ImagePath)
		if err != nil {
			return "", err
		}
		body[field] = dataURI
	}

	var requestBody any
	if method != http.MethodGet && method != http.MethodDelete || len(body) > 0 {
		requestBody = body
	}

	headers := map[string]string{}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		headers["X-Audit-Log-Reason"] = reason
	}

	respBody, err := r.discordJSONRequestWithHeaders(ctx, method, path, requestBody, headers)
	if err != nil {
		return "", err
	}

	var decoded any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &decoded); err == nil {
			return jsonResult(map[string]any{
				"method":   method,
				"path":     path,
				"response": decoded,
			})
		}
	}
	return jsonResult(map[string]any{
		"method":   method,
		"path":     path,
		"response": strings.TrimSpace(string(respBody)),
	})
}

func (r *Registry) discordJSONRequestWithHeaders(ctx context.Context, method string, path string, payload any, headers map[string]string) ([]byte, error) {
	body := io.Reader(nil)
	contentType := ""
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode Discord request: %w", err)
		}
		body = bytes.NewReader(data)
		contentType = "application/json"
	}
	return r.discordRequestWithHeaders(ctx, method, path, body, contentType, headers)
}

func (r *Registry) authorizeDiscordRESTPath(ctx context.Context, path string, body map[string]any) error {
	metadata, _ := DiscordToolContextFromContext(ctx)
	trimmed := strings.Trim(strings.SplitN(path, "?", 2)[0], "/")
	segments := strings.Split(trimmed, "/")
	if len(segments) == 0 || segments[0] == "" {
		return fmt.Errorf("path must not be empty")
	}

	switch segments[0] {
	case "channels":
		if len(segments) < 2 {
			return fmt.Errorf("channel path must include a channel id")
		}
		channelID := strings.TrimSpace(segments[1])
		if !r.cfg.DiscordChannelAllowed(channelID, metadata.ChannelID) {
			return fmt.Errorf("channel_id %q is not allowed for outbound Discord actions", channelID)
		}
		return nil
	case "guilds":
		if len(segments) < 2 {
			return fmt.Errorf("guild path must include a guild id")
		}
		guildID := strings.TrimSpace(segments[1])
		if guildID == "" {
			return fmt.Errorf("guild id must not be empty")
		}
		if metadata.GuildID != "" && guildID == metadata.GuildID {
			return nil
		}
		for _, allowed := range r.cfg.Discord.AllowedGuildIDs {
			if guildID == allowed {
				return nil
			}
		}
		return fmt.Errorf("guild_id %q is not allowed for Discord API actions", guildID)
	case "users":
		if len(segments) >= 3 && segments[1] == "@me" && segments[2] == "channels" {
			if recipient, ok := body["recipient_id"].(string); ok && !r.cfg.DMAllowedForUser(strings.TrimSpace(recipient)) {
				return fmt.Errorf("user_id %q is not allowed for Discord DMs", recipient)
			}
			return nil
		}
	}

	return fmt.Errorf("Discord API path %q is not supported by discord_api_request", path)
}

func (r *Registry) readImageDataURI(path string) (string, error) {
	resolved, err := r.resolvePath(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read image file: %w", err)
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("%s is not an image file", r.relPath(resolved))
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
