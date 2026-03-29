package discordbot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"lumen-agent/internal/agent"
	"lumen-agent/internal/auditlog"
	"lumen-agent/internal/config"
	"lumen-agent/internal/llm"
	"lumen-agent/internal/skills"
	"lumen-agent/internal/tools"
)

const (
	newCommandName         = "new"
	statusCommandName      = "status"
	compactCommandName     = "compact"
	stopCommandName        = "stop"
	typingInterval         = 8 * time.Second
	promptQueueSize        = 16
	cancelReplyText        = "The active session was reset before I could finish. Send your message again when you're ready."
	errorReplyText         = "I hit an error while working on that."
	timeoutReplyText       = "The request timed out before I could finish. I kept your session state. Please try again, or increase llm.timeout and/or lower llm.context_window_tokens."
	queuedReplyText        = "I'm still working through earlier messages in this session. Wait for my reply, then send the next one."
	emergencyStopDoneReply = "Emergency stop complete. I canceled the active session in this channel."
	emergencyStopIdleReply = "No active session was running in this channel."
)

type promptKind string

const (
	promptKindUser      promptKind = "user"
	promptKindHeartbeat promptKind = "heartbeat"
)

type Service struct {
	cfg          config.Config
	runner       *agent.Runner
	discord      *discordgo.Session
	audit        *auditlog.Logger
	sandboxes    tools.SandboxManager
	allowedGuild map[string]struct{}

	mu          sync.RWMutex
	runContext  context.Context
	application string
	sessions    map[string]*sessionState
	tasks       map[string]*backgroundTask
}

type sessionKey struct {
	GuildID   string
	ChannelID string
	UserID    string
}

type inboundPrompt struct {
	Kind          promptKind
	Content       string
	RawContent    string
	UserParts     []llm.ContentPart
	AuthorID      string
	GuildID       string
	ChannelID     string
	MessageID     string
	ModelOverride string
	LightContext  bool
	UseIndicator  bool
}

type downloadedAttachment struct {
	ID          string
	Filename    string
	ContentType string
	URL         string
	LocalPath   string
	IsImage     bool
}

type sessionState struct {
	mu        sync.RWMutex
	ID        string
	Key       sessionKey
	CreatedAt time.Time
	UpdatedAt time.Time
	FilePath  string
	History   []llm.Message
	Skills    []skills.Summary
	Queue     chan inboundPrompt
	Context   context.Context
	Cancel    context.CancelFunc
	RunLock   *sync.Mutex
}

type persistedSession struct {
	ID        string        `json:"id"`
	GuildID   string        `json:"guild_id,omitempty"`
	ChannelID string        `json:"channel_id"`
	UserID    string        `json:"user_id"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	History   []llm.Message `json:"history"`
}

func New(cfg config.Config, runner *agent.Runner, audit *auditlog.Logger, sandboxes tools.SandboxManager) (*Service, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner must not be nil")
	}
	if audit == nil {
		return nil, fmt.Errorf("audit logger must not be nil")
	}

	session, err := discordgo.New("Bot " + cfg.Discord.BotToken)
	if err != nil {
		return nil, fmt.Errorf("create Discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	service := &Service{
		cfg:          cfg,
		runner:       runner,
		discord:      session,
		audit:        audit,
		sandboxes:    sandboxes,
		allowedGuild: make(map[string]struct{}, len(cfg.Discord.AllowedGuildIDs)),
		sessions:     make(map[string]*sessionState),
		tasks:        make(map[string]*backgroundTask),
	}

	for _, guildID := range cfg.Discord.AllowedGuildIDs {
		service.allowedGuild[guildID] = struct{}{}
	}
	runner.SetBackgroundTaskManager(service)

	session.AddHandler(service.handleInteractionCreate)
	session.AddHandler(service.handleMessageCreate)

	return service, nil
}

func (s *Service) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	s.runContext = ctx
	s.mu.Unlock()

	if err := s.discord.Open(); err != nil {
		return fmt.Errorf("open Discord gateway: %w", err)
	}
	defer s.shutdown()

	user, err := s.discord.User("@me")
	if err != nil {
		return fmt.Errorf("fetch bot identity: %w", err)
	}

	s.mu.Lock()
	s.application = user.ID
	s.mu.Unlock()

	if err := s.syncCommands(); err != nil {
		return fmt.Errorf("sync slash commands: %w", err)
	}

	s.audit.Write("bot_connected", "", map[string]any{
		"username": user.Username,
		"user_id":  user.ID,
		"guilds":   s.cfg.Discord.AllowedGuildIDs,
		"allow_dm": s.cfg.Discord.AllowDirectMessages,
	})

	if s.cfg.HeartbeatEnabled() {
		if err := os.MkdirAll(s.cfg.HeartbeatEventsDir(), 0o755); err != nil {
			return fmt.Errorf("create heartbeat events dir: %w", err)
		}
		if err := os.MkdirAll(s.cfg.CronJobsDir(), 0o755); err != nil {
			return fmt.Errorf("create cron jobs dir: %w", err)
		}
		go s.runHeartbeatLoop(ctx)
		go s.runCronLoop(ctx)
	}

	<-ctx.Done()
	return nil
}

func (s *Service) shutdown() {
	s.cancelAllSessions()
	s.cancelAllBackgroundTasks()
	if err := s.discord.Close(); err != nil {
		s.audit.Write("error", "", map[string]any{"op": "close_discord", "error": err.Error()})
	}
}

func (s *Service) syncCommands() error {
	s.mu.RLock()
	applicationID := s.application
	s.mu.RUnlock()

	command := []*discordgo.ApplicationCommand{
		{
			Name:        newCommandName,
			Description: "Start a fresh chat session in this channel",
		},
		{
			Name:        statusCommandName,
			Description: "Show session, background task, and context-window status for this channel",
		},
		{
			Name:        compactCommandName,
			Description: "Compact the current session history for this channel",
		},
		{
			Name:        stopCommandName,
			Description: "Emergency stop: cancel the active session in this channel",
		},
	}

	if s.cfg.Discord.AllowDirectMessages {
		if _, err := s.discord.ApplicationCommandBulkOverwrite(applicationID, "", command); err != nil {
			return fmt.Errorf("register global commands: %w", err)
		}
	} else {
		if _, err := s.discord.ApplicationCommandBulkOverwrite(applicationID, "", []*discordgo.ApplicationCommand{}); err != nil {
			return fmt.Errorf("clear global commands: %w", err)
		}
	}

	for _, guildID := range s.cfg.Discord.AllowedGuildIDs {
		if _, err := s.discord.ApplicationCommandBulkOverwrite(applicationID, guildID, command); err != nil {
			return fmt.Errorf("register guild command for %s: %w", guildID, err)
		}
	}

	return nil
}

func (s *Service) handleInteractionCreate(_ *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction == nil || interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	switch interaction.ApplicationCommandData().Name {
	case newCommandName:
		s.handleNewCommand(interaction)
	case statusCommandName:
		s.handleStatusCommand(interaction)
	case compactCommandName:
		s.handleCompactCommand(interaction)
	case stopCommandName:
		s.handleStopCommand(interaction)
	}
}

func (s *Service) handleNewCommand(interaction *discordgo.InteractionCreate) {
	if interaction == nil {
		return
	}

	userID := interactionUserID(interaction)
	if userID == "" {
		return
	}

	if ok, reason := s.authorizeContext(interaction.GuildID, userID); !ok {
		s.respondToInteraction(interaction, reason, false)
		return
	}

	key := s.sessionKey(interaction.GuildID, interaction.ChannelID, userID)

	_, replaced, err := s.resetSession(key)
	if err != nil {
		s.audit.Write("error", "", map[string]any{
			"op":         "reset_session",
			"channel_id": interaction.ChannelID,
			"guild_id":   interaction.GuildID,
			"user_id":    userID,
			"error":      err.Error(),
		})
		s.respondToInteraction(interaction, formatRunErrorForDiscord(err), false)
		return
	}

	message := "Started a new session in this channel."
	if interaction.GuildID != "" && s.cfg.SharedGuildSessions() {
		message = "Started a new shared session for this channel."
	}
	if replaced {
		message = "Started a new session in this channel. Previous context cleared."
		if interaction.GuildID != "" && s.cfg.SharedGuildSessions() {
			message = "Started a new shared session for this channel. Previous shared context cleared."
		}
	}
	if s.hasBootstrapFile() {
		message += " BOOTSTRAP.md is present. Do you want to run bootstrap now or skip it for this session? Reply with 'bootstrap' or 'skip bootstrap'."
	} else {
		message += " Send a message when you want to continue."
	}

	s.respondToInteraction(interaction, message, false)
}

func (s *Service) handleStopCommand(interaction *discordgo.InteractionCreate) {
	if interaction == nil {
		return
	}

	userID := interactionUserID(interaction)
	if userID == "" {
		return
	}

	if ok, reason := s.authorizeContext(interaction.GuildID, userID); !ok {
		s.respondToInteraction(interaction, reason, false)
		return
	}

	key := s.sessionKey(interaction.GuildID, interaction.ChannelID, userID)

	message := emergencyStopIdleReply
	if s.stopSession(key) {
		message = emergencyStopDoneReply
	}

	s.respondToInteraction(interaction, message, false)
}

func (s *Service) handleStatusCommand(interaction *discordgo.InteractionCreate) {
	if interaction == nil {
		return
	}

	userID := interactionUserID(interaction)
	if userID == "" {
		return
	}
	if ok, reason := s.authorizeContext(interaction.GuildID, userID); !ok {
		s.respondToInteraction(interaction, reason, false)
		return
	}

	key := s.sessionKey(interaction.GuildID, interaction.ChannelID, userID)
	s.respondToInteraction(interaction, s.statusReport(key), false)
}

func (s *Service) handleCompactCommand(interaction *discordgo.InteractionCreate) {
	if interaction == nil {
		return
	}

	userID := interactionUserID(interaction)
	if userID == "" {
		return
	}
	if ok, reason := s.authorizeContext(interaction.GuildID, userID); !ok {
		s.respondToInteraction(interaction, reason, false)
		return
	}

	key := s.sessionKey(interaction.GuildID, interaction.ChannelID, userID)
	message, err := s.compactSessionForKey(key)
	if err != nil {
		s.respondToInteraction(interaction, formatRunErrorForDiscord(err), false)
		return
	}
	s.respondToInteraction(interaction, message, false)
}

func (s *Service) handleMessageCreate(_ *discordgo.Session, message *discordgo.MessageCreate) {
	if message == nil || message.Author == nil || message.Author.Bot {
		return
	}

	content := strings.TrimSpace(message.Content)
	if content == "" && !messageHasAttachments(message.Message) {
		return
	}

	if ok, _ := s.authorizeContext(message.GuildID, message.Author.ID); !ok {
		return
	}

	key := s.sessionKey(message.GuildID, message.ChannelID, message.Author.ID)

	if isEmergencyStopCommand(content) {
		reply := emergencyStopIdleReply
		if s.stopSession(key) {
			reply = emergencyStopDoneReply
		}

		prompt := inboundPrompt{
			Kind:       promptKindUser,
			Content:    content,
			RawContent: content,
			AuthorID:   strings.TrimSpace(message.Author.ID),
			GuildID:    message.GuildID,
			ChannelID:  message.ChannelID,
			MessageID:  message.ID,
		}

		if err := s.sendReply(prompt, reply); err != nil {
			s.audit.Write("error", "", map[string]any{"op": "send_stop_reply", "error": err.Error()})
		}
		return
	}

	session := s.lookupSession(key)
	if session == nil {
		var err error
		session, _, err = s.resetSession(key)
		if err != nil {
			s.audit.Write("error", "", map[string]any{
				"op":         "auto_start_session",
				"channel_id": message.ChannelID,
				"guild_id":   message.GuildID,
				"user_id":    message.Author.ID,
				"error":      err.Error(),
			})
			prompt := inboundPrompt{
				Kind:         promptKindUser,
				Content:      message.Content,
				RawContent:   strings.TrimSpace(message.Content),
				AuthorID:     strings.TrimSpace(message.Author.ID),
				GuildID:      message.GuildID,
				ChannelID:    message.ChannelID,
				MessageID:    message.ID,
				UseIndicator: true,
			}
			if sendErr := s.sendReply(prompt, formatRunErrorForDiscord(err)); sendErr != nil {
				s.audit.Write("error", "", map[string]any{"op": "send_auto_start_error", "error": sendErr.Error()})
			}
			return
		}
	}

	prompt := s.userPromptFromMessage(message)

	select {
	case <-session.Context.Done():
		return
	case session.Queue <- prompt:
	default:
		s.audit.Write("warn", session.ID, map[string]any{"op": "queue_full", "channel_id": message.ChannelID})
		if err := s.sendReply(prompt, queuedReplyText); err != nil {
			s.audit.Write("error", session.ID, map[string]any{"op": "send_queue_warning", "error": err.Error()})
		}
	}
}

func (s *Service) resetSession(key sessionKey) (*sessionState, bool, error) {
	parent := s.currentContext()
	if parent == nil {
		parent = context.Background()
	}

	now := time.Now().UTC()
	ctx, cancel := context.WithCancel(parent)
	sessionID := newSessionID(now)
	state := &sessionState{
		ID:        sessionID,
		Key:       key,
		CreatedAt: now,
		UpdatedAt: now,
		FilePath:  filepath.Join(s.cfg.App.SessionDir, sessionID+".json"),
		Skills:    s.runner.SnapshotSkills(),
		Queue:     make(chan inboundPrompt, promptQueueSize),
		Context:   ctx,
		Cancel:    cancel,
		RunLock:   &sync.Mutex{},
	}

	if err := state.persist(); err != nil {
		cancel()
		return nil, false, fmt.Errorf("persist new session: %w", err)
	}

	keyString := key.String()

	s.mu.Lock()
	previous, replaced := s.sessions[keyString]
	s.sessions[keyString] = state
	s.mu.Unlock()

	if replaced && previous != nil {
		previous.Cancel()
	}

	go s.runSession(state)
	return state, replaced, nil
}

func (s *Service) runSession(state *sessionState) {
	for {
		select {
		case <-state.Context.Done():
			return
		case prompt := <-state.Queue:
			s.processPrompt(state, prompt)
		}
	}
}

func (s *Service) processPrompt(state *sessionState, prompt inboundPrompt) {
	state.lockRun()
	defer state.unlockRun()

	stopTyping := func() {}
	if prompt.UseIndicator {
		stopTyping = s.startTyping(prompt.ChannelID)
	}
	defer stopTyping()

	s.audit.Write("turn_start", state.ID, map[string]any{
		"kind":       string(prompt.Kind),
		"channel_id": prompt.ChannelID,
		"guild_id":   prompt.GuildID,
	})

	runCtx := tools.WithDiscordToolContext(state.Context, tools.DiscordToolContext{
		GuildID:   prompt.GuildID,
		ChannelID: prompt.ChannelID,
		UserID:    promptUserID(prompt, state),
		MessageID: prompt.MessageID,
	})

	history, skillsSnapshot := state.snapshotForRun()
	history, previousHistoryLen, persistSessionHistory := s.prepareRunHistory(prompt, history)
	updated, err := s.runner.Run(runCtx, history, prompt.Content, agent.ConversationContext{
		IsDirectMessage: state.Key.GuildID == "",
		IsHeartbeat:     prompt.Kind == promptKindHeartbeat,
		LightContext:    prompt.LightContext,
		GuildID:         prompt.GuildID,
		ChannelID:       prompt.ChannelID,
		ModelOverride:   prompt.ModelOverride,
		Skills:          skillsSnapshot,
		UserParts:       prompt.UserParts,
		Now:             time.Now(),
	}, func(event agent.Event) {
		s.logAgentEvent(state, event)
	})

	if errors.Is(state.Context.Err(), context.Canceled) {
		if s.sessionStillActive(state) {
			if sendErr := s.sendReply(prompt, cancelReplyText); sendErr != nil {
				s.audit.Write("error", state.ID, map[string]any{"op": "send_cancel_reply", "error": sendErr.Error()})
			}
		}
		return
	}

	if err != nil {
		s.audit.Write("error", state.ID, map[string]any{"op": "run_failed", "error": err.Error()})
		if persistSessionHistory && len(updated) > 0 {
			state.setHistory(agent.CompactHistoryForStorage(s.cfg, updated))
		}
		if persistSessionHistory {
			state.setUpdatedAt(time.Now().UTC())
			if persistErr := state.persist(); persistErr != nil {
				s.audit.Write("error", state.ID, map[string]any{"op": "persist_failed_session", "error": persistErr.Error()})
			}
		}
		if prompt.Kind == promptKindHeartbeat {
			return
		}
		replyText := formatRunErrorForDiscord(err)
		if sendErr := s.sendReply(prompt, replyText); sendErr != nil {
			s.audit.Write("error", state.ID, map[string]any{"op": "send_error_reply", "error": sendErr.Error()})
		}
		return
	}

	reply, silent := turnAssistantReply(updated, previousHistoryLen)
	if silent {
		updated = clearNoReplyToken(updated, previousHistoryLen)
	}

	if persistSessionHistory {
		state.setHistory(agent.CompactHistoryForStorage(s.cfg, updated))
		state.setUpdatedAt(time.Now().UTC())
		if persistErr := state.persist(); persistErr != nil {
			s.audit.Write("error", state.ID, map[string]any{"op": "persist_session", "error": persistErr.Error()})
		}
	}

	if prompt.Kind == promptKindHeartbeat {
		s.handleHeartbeatReply(prompt, reply)
		return
	}

	if silent || strings.TrimSpace(reply) == "" {
		return
	}

	memoryPrompt := strings.TrimSpace(prompt.RawContent)
	if memoryPrompt == "" {
		memoryPrompt = strings.TrimSpace(prompt.Content)
	}
	if state.Key.GuildID == "" {
		if err := agent.AppendToMemoryShard(s.cfg.App.MemoryDir, memoryPrompt, reply, time.Now()); err != nil {
			s.audit.Write("error", state.ID, map[string]any{"op": "append_memory_shard", "error": err.Error()})
		}
	} else {
		guildMemoryRoot := filepath.Join(s.cfg.App.SessionDir, "guild-memory", state.Key.GuildID, state.Key.ChannelID)
		if err := agent.AppendToMemoryShard(guildMemoryRoot, memoryPrompt, reply, time.Now()); err != nil {
			s.audit.Write("error", state.ID, map[string]any{"op": "append_guild_memory_shard", "error": err.Error()})
		}
	}

	if sendErr := s.sendReply(prompt, reply); sendErr != nil {
		s.audit.Write("error", state.ID, map[string]any{"op": "send_reply", "error": sendErr.Error()})
	}
}

func (s *Service) prepareRunHistory(prompt inboundPrompt, history []llm.Message) ([]llm.Message, int, bool) {
	previousHistoryLen := len(history)
	persistSessionHistory := true
	if prompt.Kind == promptKindHeartbeat && s.cfg.Heartbeat.IsolatedSession {
		return nil, 0, false
	}
	return history, previousHistoryLen, persistSessionHistory
}

func (s *Service) userPromptFromMessage(message *discordgo.MessageCreate) inboundPrompt {
	rawContent := strings.TrimSpace(message.Content)
	attachments := s.prepareInboundAttachments(message.Message)
	content := replaceAttachmentURLs(rawContent, attachments)
	if message.GuildID != "" && s.cfg.SharedGuildSessions() {
		content = formatSharedChannelPrompt(message, s.application, attachments)
	} else if strings.TrimSpace(content) == "" && len(attachments) > 0 {
		content = describeDirectAttachments(attachments)
	} else if len(attachments) > 0 {
		content = appendAttachmentInventory(content, attachments)
	}

	return inboundPrompt{
		Kind:         promptKindUser,
		Content:      content,
		RawContent:   rawContent,
		UserParts:    buildUserMessageParts(content, attachments),
		AuthorID:     strings.TrimSpace(message.Author.ID),
		GuildID:      message.GuildID,
		ChannelID:    message.ChannelID,
		MessageID:    message.ID,
		UseIndicator: true,
	}
}

func (s *Service) sessionKey(guildID string, channelID string, userID string) sessionKey {
	if guildID != "" && s.cfg.SharedGuildSessions() {
		userID = ""
	}

	return sessionKey{
		GuildID:   guildID,
		ChannelID: channelID,
		UserID:    userID,
	}
}

func promptUserID(prompt inboundPrompt, state *sessionState) string {
	if strings.TrimSpace(prompt.AuthorID) != "" {
		return strings.TrimSpace(prompt.AuthorID)
	}
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.Key.UserID)
}

func formatSharedChannelPrompt(message *discordgo.MessageCreate, botUserID string, attachments []downloadedAttachment) string {
	content := replaceAttachmentURLs(strings.TrimSpace(message.Content), attachments)
	authorName := sharedChannelAuthorName(message)
	mentionedBot := messageMentionsUser(message, botUserID)
	replyingToBot := messageRepliesToUser(message, botUserID)
	replyToMessageID := ""
	if message.MessageReference != nil {
		replyToMessageID = strings.TrimSpace(message.MessageReference.MessageID)
	}

	var builder strings.Builder
	builder.WriteString("Shared channel message\n")
	builder.WriteString("speaker: ")
	builder.WriteString(authorName)
	if message.Author != nil && strings.TrimSpace(message.Author.ID) != "" {
		builder.WriteString("\nuser_id: ")
		builder.WriteString(strings.TrimSpace(message.Author.ID))
	}
	builder.WriteString("\nmessage_id: ")
	builder.WriteString(strings.TrimSpace(message.ID))
	builder.WriteString("\nmentioned_you: ")
	builder.WriteString(yesNo(mentionedBot))
	builder.WriteString("\nreplying_to_you: ")
	builder.WriteString(yesNo(replyingToBot))
	if replyToMessageID != "" {
		builder.WriteString("\nreply_to_message_id: ")
		builder.WriteString(replyToMessageID)
	}
	if len(attachments) > 0 {
		builder.WriteString("\nattachments: ")
		builder.WriteString(fmt.Sprintf("%d", len(attachments)))
		for _, attachment := range attachments {
			builder.WriteString("\n- ")
			builder.WriteString(sharedAttachmentLabel(attachment))
		}
	}
	builder.WriteString("\ncontent:\n")
	builder.WriteString(content)
	return builder.String()
}

func buildUserMessageParts(content string, attachments []downloadedAttachment) []llm.ContentPart {
	if len(attachments) == 0 {
		return nil
	}

	parts := make([]llm.ContentPart, 0, 1+len(attachments))
	if strings.TrimSpace(content) != "" {
		parts = append(parts, llm.ContentPart{
			Type: llm.ContentPartText,
			Text: content,
		})
	}
	for _, attachment := range attachments {
		if !attachment.IsImage || strings.TrimSpace(attachment.URL) == "" {
			continue
		}
		parts = append(parts, llm.ContentPart{
			Type:     llm.ContentPartImageURL,
			ImageURL: strings.TrimSpace(attachment.URL),
		})
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func (s *Service) prepareInboundAttachments(message *discordgo.Message) []downloadedAttachment {
	if message == nil || len(message.Attachments) == 0 {
		return nil
	}

	result := make([]downloadedAttachment, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		if attachment == nil {
			continue
		}
		item := downloadedAttachment{
			ID:          strings.TrimSpace(attachment.ID),
			Filename:    strings.TrimSpace(attachment.Filename),
			ContentType: strings.TrimSpace(attachment.ContentType),
			URL:         strings.TrimSpace(attachment.URL),
			IsImage:     isImageAttachment(attachment),
		}
		if s.cfg.Discord.DownloadIncomingAttachments {
			localPath, err := s.downloadIncomingAttachment(message, attachment)
			if err != nil {
				if s.audit != nil {
					s.audit.Write("error", "", map[string]any{
						"op":         "download_incoming_attachment",
						"message_id": message.ID,
						"channel_id": message.ChannelID,
						"attachment": item.Filename,
						"error":      err.Error(),
					})
				}
			} else {
				item.LocalPath = localPath
			}
		}
		result = append(result, item)
	}
	return result
}

func messageHasAttachments(message *discordgo.Message) bool {
	return message != nil && len(message.Attachments) > 0
}

func isImageAttachment(attachment *discordgo.MessageAttachment) bool {
	if attachment == nil {
		return false
	}
	contentType := strings.TrimSpace(strings.ToLower(attachment.ContentType))
	if strings.HasPrefix(contentType, "image/") {
		return true
	}

	filename := strings.TrimSpace(strings.ToLower(attachment.Filename))
	for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic", ".heif"} {
		if strings.HasSuffix(filename, suffix) {
			return true
		}
	}
	return false
}

func sharedAttachmentLabel(attachment downloadedAttachment) string {
	filename := strings.TrimSpace(attachment.Filename)
	if filename == "" {
		filename = "attachment"
	}
	label := filename
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType != "" {
		label += " (" + contentType + ")"
	}
	if strings.TrimSpace(attachment.LocalPath) != "" {
		label += " -> " + attachment.LocalPath
	} else if strings.TrimSpace(attachment.URL) != "" {
		label += " -> " + strings.TrimSpace(attachment.URL)
	}
	return label
}

func appendAttachmentInventory(content string, attachments []downloadedAttachment) string {
	content = strings.TrimSpace(content)
	if len(attachments) == 0 {
		return content
	}

	var builder strings.Builder
	if content != "" {
		builder.WriteString(content)
		builder.WriteString("\n\n")
	}
	builder.WriteString("attachments:\n")
	for _, attachment := range attachments {
		builder.WriteString("- ")
		builder.WriteString(sharedAttachmentLabel(attachment))
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String())
}

func describeDirectAttachments(attachments []downloadedAttachment) string {
	if len(attachments) == 0 {
		return ""
	}
	return appendAttachmentInventory(fmt.Sprintf("User sent %d attachment(s).", len(attachments)), attachments)
}

func replaceAttachmentURLs(content string, attachments []downloadedAttachment) string {
	if strings.TrimSpace(content) == "" || len(attachments) == 0 {
		return strings.TrimSpace(content)
	}
	replaced := content
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.URL) == "" || strings.TrimSpace(attachment.LocalPath) == "" {
			continue
		}
		replaced = strings.ReplaceAll(replaced, attachment.URL, attachment.LocalPath)
	}
	return strings.TrimSpace(replaced)
}

func (s *Service) downloadIncomingAttachment(message *discordgo.Message, attachment *discordgo.MessageAttachment) (string, error) {
	if message == nil || attachment == nil || strings.TrimSpace(attachment.URL) == "" {
		return "", fmt.Errorf("attachment URL must not be empty")
	}

	filename := strings.TrimSpace(attachment.Filename)
	if filename == "" {
		filename = strings.TrimSpace(attachment.ID)
	}
	if filename == "" {
		filename = "attachment"
	}
	filename = filepath.Base(filename)

	targetDir := filepath.Join(s.cfg.Discord.IncomingAttachmentsDir, strings.TrimSpace(message.ChannelID), strings.TrimSpace(message.ID))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create attachment dir: %w", err)
	}
	targetPath := filepath.Join(targetDir, filename)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(strings.TrimSpace(attachment.URL))
	if err != nil {
		return "", fmt.Errorf("download attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("download attachment: unexpected status %d", resp.StatusCode)
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("create attachment file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", fmt.Errorf("write attachment file: %w", err)
	}
	return targetPath, nil
}

func sharedChannelAuthorName(message *discordgo.MessageCreate) string {
	if message == nil {
		return "unknown"
	}
	if message.Member != nil && strings.TrimSpace(message.Member.Nick) != "" {
		return strings.TrimSpace(message.Member.Nick)
	}
	if message.Author != nil && strings.TrimSpace(message.Author.GlobalName) != "" {
		return strings.TrimSpace(message.Author.GlobalName)
	}
	if message.Author != nil && strings.TrimSpace(message.Author.Username) != "" {
		return strings.TrimSpace(message.Author.Username)
	}
	return "unknown"
}

func messageMentionsUser(message *discordgo.MessageCreate, userID string) bool {
	userID = strings.TrimSpace(userID)
	if message == nil || userID == "" {
		return false
	}
	for _, user := range message.Mentions {
		if user != nil && strings.TrimSpace(user.ID) == userID {
			return true
		}
	}
	return false
}

func messageRepliesToUser(message *discordgo.MessageCreate, userID string) bool {
	userID = strings.TrimSpace(userID)
	if message == nil || userID == "" || message.MessageReference == nil {
		return false
	}
	if message.ReferencedMessage != nil && message.ReferencedMessage.Author != nil {
		return strings.TrimSpace(message.ReferencedMessage.Author.ID) == userID
	}
	return false
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func turnAssistantReply(history []llm.Message, previousLen int) (string, bool) {
	if previousLen < 0 {
		previousLen = 0
	}
	if previousLen > len(history) {
		previousLen = len(history)
	}

	turn := history[previousLen:]
	for i := len(turn) - 1; i >= 0; i-- {
		message := turn[i]
		if message.Role != "assistant" {
			continue
		}
		trimmed := strings.TrimSpace(message.Content)
		if trimmed == "" {
			continue
		}
		if trimmed == agent.NoReplyToken {
			return "", true
		}
		if len(message.ToolCalls) == 0 {
			return agent.CompactHistoryForNextTurn([]llm.Message{message})[0].Content, false
		}
	}

	for i := len(turn) - 1; i >= 0; i-- {
		message := turn[i]
		if message.Role != "assistant" {
			continue
		}
		trimmed := strings.TrimSpace(message.Content)
		if trimmed == "" {
			continue
		}
		if trimmed == agent.NoReplyToken {
			return "", true
		}
		return agent.CompactHistoryForNextTurn([]llm.Message{message})[0].Content, false
	}

	return "", false
}

func clearNoReplyToken(history []llm.Message, previousLen int) []llm.Message {
	if previousLen < 0 {
		previousLen = 0
	}
	if previousLen >= len(history) {
		return history
	}

	cleaned := make([]llm.Message, len(history))
	copy(cleaned, history)
	for i := len(cleaned) - 1; i >= previousLen; i-- {
		if cleaned[i].Role != "assistant" {
			continue
		}
		if strings.TrimSpace(cleaned[i].Content) == agent.NoReplyToken {
			cleaned[i].Content = ""
			return cleaned
		}
	}
	return cleaned
}

func (s *Service) logAgentEvent(state *sessionState, event agent.Event) {
	switch event.Kind {
	case agent.EventToolStarted:
		s.audit.Write("tool_start", state.ID, map[string]any{
			"tool":   event.ToolName,
			"detail": event.Detail,
		})
	case agent.EventToolFinished:
		s.audit.Write("tool_done", state.ID, map[string]any{
			"tool":   event.ToolName,
			"detail": event.Detail,
		})
	case agent.EventStatus:
		s.audit.Write("status", state.ID, map[string]any{"message": event.Message})
	case agent.EventAssistant:
		if strings.TrimSpace(event.Message) == "" || strings.TrimSpace(event.Message) == agent.NoReplyToken {
			return
		}
		s.audit.Write("assistant_reply", state.ID, map[string]any{
			"length": len(event.Message),
		})
	}
}

func (s *Service) startTyping(channelID string) func() {
	if channelID == "" {
		return func() {}
	}

	stop := make(chan struct{})

	go func() {
		ticker := time.NewTicker(typingInterval)
		defer ticker.Stop()

		_ = s.discord.ChannelTyping(channelID)

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = s.discord.ChannelTyping(channelID)
			}
		}
	}()

	return func() {
		close(stop)
	}
}

func (s *Service) sendReply(prompt inboundPrompt, content string) error {
	parts := splitOutgoingMessages(content)
	if len(parts) == 0 {
		return nil
	}

	var reference *discordgo.MessageReference
	if s.cfg.Discord.ReplyToMessage && prompt.MessageID != "" {
		reference = &discordgo.MessageReference{
			MessageID: prompt.MessageID,
			ChannelID: prompt.ChannelID,
			GuildID:   prompt.GuildID,
		}
	}

	for _, part := range parts {
		_, err := s.discord.ChannelMessageSendComplex(prompt.ChannelID, &discordgo.MessageSend{
			Content:   part,
			Reference: reference,
			AllowedMentions: &discordgo.MessageAllowedMentions{
				RepliedUser: false,
			},
		})
		if err != nil {
			return fmt.Errorf("send Discord message: %w", err)
		}
	}

	return nil
}

func (s *Service) respondToInteraction(interaction *discordgo.InteractionCreate, content string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}

	if err := s.discord.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   flags,
		},
	}); err != nil {
		s.audit.Write("error", "", map[string]any{"op": "respond_interaction", "error": err.Error()})
	}
}

func (s *Service) authorizeContext(guildID string, userID string) (bool, string) {
	if guildID == "" {
		if s.cfg.DMAllowedForUser(userID) {
			return true, ""
		}
		if !s.cfg.Discord.AllowDirectMessages {
			return false, "Direct messages are disabled for this bot."
		}
		return false, "Direct messages are only enabled for allowed users."
	}

	if _, ok := s.allowedGuild[guildID]; ok {
		return true, ""
	}

	return false, "This server is not in discord.allowed_guild_ids."
}

func (s *Service) lookupSession(key sessionKey) *sessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[key.String()]
}

func (s *Service) statusReport(key sessionKey) string {
	session := s.lookupSession(key)
	activeSessions, queuedTasks, runningTasks, completedTasks, failedTasks, canceledTasks := s.backgroundAndSessionCounts()
	inputBudget := s.cfg.LLMInputTokenBudget()

	lines := []string{
		"Status",
		contextWindowLine(0, inputBudget),
		backgroundTaskLine(queuedTasks, runningTasks, completedTasks, failedTasks, canceledTasks),
	}

	if session == nil {
		lines = append(lines,
			fmt.Sprintf("Open chats: %d", activeSessions),
			"No active chat in this channel yet.",
		)
		return strings.Join(lines, "\n")
	}

	history, _ := session.snapshotForRun()
	usedTokens := agent.EstimateHistoryTokens(history)
	lines = append(lines,
		contextWindowLine(usedTokens, inputBudget),
		fmt.Sprintf("Open chats: %d", activeSessions),
		fmt.Sprintf("This chat: %d saved messages", len(history)),
		fmt.Sprintf("Waiting messages: %d", len(session.Queue)),
		fmt.Sprintf("Last activity: %s", session.updatedAt().In(time.Local).Format("2006-01-02 15:04 MST")),
	)
	return strings.Join(lines, "\n")
}

func contextWindowLine(usedTokens int, inputBudget int) string {
	if inputBudget <= 0 {
		return "Context: unavailable"
	}
	if usedTokens < 0 {
		usedTokens = 0
	}
	percent := 0
	if inputBudget > 0 {
		percent = (usedTokens * 100) / inputBudget
	}
	if percent < 0 {
		percent = 0
	}
	return fmt.Sprintf("Context used: %d%% (%d of about %d input tokens)", percent, usedTokens, inputBudget)
}

func backgroundTaskLine(queued int, running int, completed int, failed int, canceled int) string {
	active := queued + running
	if active == 0 && completed == 0 && failed == 0 && canceled == 0 {
		return "Background jobs: none"
	}
	if active == 0 {
		return fmt.Sprintf("Background jobs: none running, %d done, %d failed, %d canceled", completed, failed, canceled)
	}
	return fmt.Sprintf("Background jobs: %d active (%d queued, %d running), %d done, %d failed, %d canceled", active, queued, running, completed, failed, canceled)
}

func (s *Service) compactSessionForKey(key sessionKey) (string, error) {
	session := s.lookupSession(key)
	if session == nil {
		return "No active session in this channel to compact.", nil
	}

	session.lockRun()
	defer session.unlockRun()

	history, _ := session.snapshotForRun()
	beforeMessages := len(history)
	beforeTokens := agent.EstimateHistoryTokens(history)
	compacted := agent.CompactHistoryForStorage(s.cfg, history)
	afterMessages := len(compacted)
	afterTokens := agent.EstimateHistoryTokens(compacted)

	session.setHistory(compacted)
	session.setUpdatedAt(time.Now().UTC())
	if err := session.persist(); err != nil {
		return "", fmt.Errorf("persist compacted session: %w", err)
	}

	if beforeMessages == afterMessages && beforeTokens == afterTokens {
		return fmt.Sprintf(
			"Context already compact enough. Session `%s` stayed at %d messages and ~%d tokens.",
			session.ID,
			afterMessages,
			afterTokens,
		), nil
	}

	return fmt.Sprintf(
		"Compacted session `%s`: %d -> %d messages, ~%d -> ~%d tokens.",
		session.ID,
		beforeMessages,
		afterMessages,
		beforeTokens,
		afterTokens,
	), nil
}

func (s *sessionState) lockRun() {
	if s == nil || s.RunLock == nil {
		return
	}
	s.RunLock.Lock()
}

func (s *sessionState) unlockRun() {
	if s == nil || s.RunLock == nil {
		return
	}
	s.RunLock.Unlock()
}

func (s *sessionState) snapshotForRun() ([]llm.Message, []skills.Summary) {
	if s == nil {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	history := append([]llm.Message(nil), s.History...)
	skillSnapshot := append([]skills.Summary(nil), s.Skills...)
	return history, skillSnapshot
}

func (s *sessionState) setHistory(history []llm.Message) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append([]llm.Message(nil), history...)
}

func (s *sessionState) setUpdatedAt(updatedAt time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdatedAt = updatedAt
}

func (s *sessionState) updatedAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UpdatedAt
}

func (s *Service) stopSession(key sessionKey) bool {
	keyString := key.String()

	s.mu.Lock()
	state, ok := s.sessions[keyString]
	if ok {
		delete(s.sessions, keyString)
	}
	s.mu.Unlock()

	if ok && state != nil {
		state.Cancel()
	}

	return ok
}

func (s *Service) sessionStillActive(state *sessionState) bool {
	if s == nil || state == nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	current := s.sessions[state.Key.String()]
	return current == state
}

func (s *Service) backgroundAndSessionCounts() (activeSessions int, queued int, running int, completed int, failed int, canceled int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeSessions = len(s.sessions)
	for _, task := range s.tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case backgroundTaskQueued:
			queued++
		case backgroundTaskRunning:
			running++
		case backgroundTaskCompleted:
			completed++
		case backgroundTaskFailed:
			failed++
		case backgroundTaskCanceled:
			canceled++
		}
	}
	return
}

func (s *Service) cancelAllSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, state := range s.sessions {
		state.Cancel()
		delete(s.sessions, key)
	}
}

func (s *Service) currentContext() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runContext
}

func (s *Service) hasBootstrapFile() bool {
	path := filepath.Join(s.cfg.App.WorkspaceRoot, "BOOTSTRAP.md")
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func (s *sessionState) persist() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(persistedSession{
		ID:        s.ID,
		GuildID:   s.Key.GuildID,
		ChannelID: s.Key.ChannelID,
		UserID:    s.Key.UserID,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		History:   s.History,
	}, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}

	if err := os.WriteFile(s.FilePath, data, 0o644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	return nil
}

func (k sessionKey) String() string {
	guildID := k.GuildID
	if guildID == "" {
		guildID = "dm"
	}
	return strings.Join([]string{guildID, k.ChannelID, k.UserID}, ":")
}

func interactionUserID(interaction *discordgo.InteractionCreate) string {
	if interaction == nil {
		return ""
	}
	if interaction.Member != nil && interaction.Member.User != nil {
		return interaction.Member.User.ID
	}
	if interaction.User != nil {
		return interaction.User.ID
	}
	return ""
}

func isEmergencyStopCommand(content string) bool {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) == 0 {
		return false
	}
	return strings.EqualFold(fields[0], "/stop")
}

func isTimeoutError(err error) bool {
	return llm.IsTimeoutError(err)
}

func formatRunErrorForDiscord(err error) string {
	if err == nil {
		return errorReplyText
	}

	summary := strings.Join(strings.Fields(strings.TrimSpace(err.Error())), " ")
	if summary == "" {
		summary = "unknown error"
	}
	if len(summary) > 350 {
		summary = summary[:347] + "..."
	}

	prefix := errorReplyText
	if isTimeoutError(err) {
		prefix = timeoutReplyText
	}
	if strings.Contains(strings.ToLower(err.Error()), "api error (522)") {
		prefix = "The upstream model provider timed out before replying. This is usually a provider or proxy issue, not a Discord bot bug."
	}
	return prefix + "\n\nError: " + summary
}

func newSessionID(now time.Time) string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("session-%d", now.UnixNano())
	}
	return fmt.Sprintf("session-%s-%x", now.Format("20060102-150405"), suffix[:])
}
