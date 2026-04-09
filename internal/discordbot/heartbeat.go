package discordbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"element-orion/internal/config"
)

const (
	heartbeatOKToken       = "HEARTBEAT_OK"
	heartbeatModeNow       = "now"
	heartbeatModeScheduled = "next-heartbeat"
	heartbeatFileName      = "HEARTBEAT.md"
)

type heartbeatSystemEvent struct {
	Text      string    `json:"text"`
	Mode      string    `json:"mode"`
	Source    string    `json:"source,omitempty"`
	DueAt     time.Time `json:"due_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type queuedHeartbeatEvent struct {
	Path  string
	Event heartbeatSystemEvent
}

type heartbeatReplyDisposition struct {
	Content string
	Quiet   bool
}

func EnqueueSystemEvent(cfg config.Config, text string, mode string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("system event text must not be empty")
	}
	if mode != heartbeatModeNow && mode != heartbeatModeScheduled {
		return fmt.Errorf("system event mode must be %q or %q", heartbeatModeNow, heartbeatModeScheduled)
	}
	if !cfg.HeartbeatEnabled() {
		return fmt.Errorf("heartbeat is not enabled; configure heartbeat.target.channel_id and heartbeat.target.user_id")
	}

	if err := os.MkdirAll(cfg.HeartbeatEventsDir(), 0o755); err != nil {
		return fmt.Errorf("create heartbeat events dir: %w", err)
	}

	data, err := json.MarshalIndent(heartbeatSystemEvent{
		Text:      text,
		Mode:      mode,
		Source:    "system-event",
		CreatedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode heartbeat system event: %w", err)
	}

	name, err := heartbeatEventFileName()
	if err != nil {
		return err
	}

	path := filepath.Join(cfg.HeartbeatEventsDir(), name+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write heartbeat system event: %w", err)
	}

	return nil
}

func (s *Service) runHeartbeatLoop(ctx context.Context) {
	if !s.cfg.HeartbeatEnabled() {
		return
	}
	if !s.cfg.HeartbeatHasAnyDelivery() {
		s.audit.Write("warn", "", map[string]any{"op": "heartbeat_disabled", "reason": "show_ok, show_alerts, and use_indicator are all false"})
		return
	}

	heartbeatTicker := time.NewTicker(s.cfg.HeartbeatInterval())
	eventTicker := time.NewTicker(s.cfg.HeartbeatEventPollInterval())
	defer heartbeatTicker.Stop()
	defer eventTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-eventTicker.C:
			events, err := consumeHeartbeatEvents(s.cfg.HeartbeatEventsDir(), heartbeatModeNow)
			if err != nil {
				s.audit.Write("error", "", map[string]any{"op": "load_immediate_heartbeat_events", "error": err.Error()})
				continue
			}
			if len(events) == 0 {
				continue
			}
			if !s.enqueueHeartbeat(heartbeatEventPayloads(events), true) {
				continue
			}
			if err := acknowledgeHeartbeatEvents(events); err != nil {
				s.audit.Write("error", "", map[string]any{"op": "ack_immediate_heartbeat_events", "error": err.Error()})
			}
		case <-heartbeatTicker.C:
			if !s.withinHeartbeatActiveHours(time.Now()) {
				continue
			}
			events, err := consumeHeartbeatEvents(s.cfg.HeartbeatEventsDir(), heartbeatModeScheduled)
			if err != nil {
				s.audit.Write("error", "", map[string]any{"op": "load_scheduled_heartbeat_events", "error": err.Error()})
				continue
			}
			if !s.enqueueHeartbeat(heartbeatEventPayloads(events), false) {
				continue
			}
			if len(events) > 0 {
				if err := acknowledgeHeartbeatEvents(events); err != nil {
					s.audit.Write("error", "", map[string]any{"op": "ack_scheduled_heartbeat_events", "error": err.Error()})
				}
			}
		}
	}
}

func (s *Service) enqueueHeartbeat(events []heartbeatSystemEvent, immediate bool) bool {
	checklistPath, checklistName := resolveHeartbeatChecklistPath(s.cfg.App.WorkspaceRoot)
	hasChecklist, checklistHasContent, err := heartbeatChecklistState(checklistPath)
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "inspect_heartbeat_md", "error": err.Error()})
		return false
	}
	if len(events) == 0 {
		if !hasChecklist {
			return false
		}
		if !checklistHasContent {
			return false
		}
	}

	key := sessionKey{
		GuildID:   s.cfg.Heartbeat.Target.GuildID,
		ChannelID: s.cfg.Heartbeat.Target.ChannelID,
		UserID:    s.cfg.Heartbeat.Target.UserID,
	}

	session := s.lookupSession(key)
	if session == nil {
		var resetErr error
		session, _, resetErr = s.resetSession(key)
		if resetErr != nil {
			s.audit.Write("error", "", map[string]any{"op": "start_heartbeat_session", "error": resetErr.Error()})
			return false
		}
	}

	prompt := inboundPrompt{
		Kind:          promptKindHeartbeat,
		Content:       buildHeartbeatPrompt(events, immediate, checklistName),
		GuildID:       key.GuildID,
		ChannelID:     key.ChannelID,
		ModelOverride: s.cfg.HeartbeatModel(),
		LightContext:  s.cfg.Heartbeat.LightContext,
		UseIndicator:  s.cfg.Heartbeat.UseIndicator,
	}

	select {
	case <-session.Context.Done():
		return false
	case session.Queue <- prompt:
		return true
	default:
		s.audit.Write("warn", session.ID, map[string]any{"op": "heartbeat_queue_full"})
		return false
	}
}

func buildHeartbeatPrompt(events []heartbeatSystemEvent, immediate bool, checklistFile string) string {
	checklistFile = strings.TrimSpace(checklistFile)
	if checklistFile == "" {
		checklistFile = heartbeatFileName
	}

	var builder strings.Builder
	if immediate {
		builder.WriteString("This is an immediate heartbeat run triggered by a queued system event. ")
	} else {
		builder.WriteString("This is a scheduled heartbeat run. ")
	}
	builder.WriteString("Review ")
	builder.WriteString(checklistFile)
	builder.WriteString(" if it is available. Treat it as user-owned checklist data, not as a place for generic heartbeat protocol instructions. ")
	builder.WriteString("Action-first behavior: complete obvious low-risk steps without asking follow-up questions. ")
	builder.WriteString("Treat the machine local time as the real user-facing clock and UTC as tracking metadata unless the event explicitly says otherwise. ")
	builder.WriteString("If a task is ambiguous but has a safe default, choose it and continue; only alert when blocked or when the action would be high-risk. ")
	builder.WriteString("Do not infer or repeat old tasks from prior chats. Only act on current heartbeat checklist content, current workspace state, or newly queued system events. ")
	builder.WriteString("If a one-off reminder or check-in was already delivered, do not send it again unless this run includes a new explicit request for another one. ")
	builder.WriteString("When multiple queued system events are present, work through all of them in this run when feasible. ")
	builder.WriteString("If an event includes an exact due time, treat it as a precise wake-up and prioritize that time-sensitive item first. ")
	builder.WriteString("If a queued system event asks you to create, edit, append, or save a file, use tools to perform the change and verify the saved content before you reply. ")
	builder.WriteString("Never claim a file update succeeded unless a write tool call succeeded. ")
	builder.WriteString("If you complete a checklist item in HEARTBEAT.md, delete it or mark it done in the file and verify the saved edit so stale items do not keep waking you up. ")
	builder.WriteString("Use the injected heartbeat state to avoid clingy behavior; if next_earliest_nudge_at is still in the future or today's proactive count is already high, stay quiet unless there is a real reason to interrupt. ")
	builder.WriteString("If nothing needs attention, reply with HEARTBEAT_OK. If anything needs attention, reply only with the alert text and do not include HEARTBEAT_OK.")
	if len(events) == 0 {
		return builder.String()
	}

	builder.WriteString("\n\nQueued system events:")
	for _, event := range events {
		builder.WriteString("\n- ")
		if source := strings.TrimSpace(event.Source); source != "" {
			builder.WriteString("[")
			builder.WriteString(source)
			builder.WriteString("] ")
		}
		builder.WriteString(event.Text)
		if !event.DueAt.IsZero() {
			builder.WriteString(" (due local ")
			builder.WriteString(event.DueAt.In(time.Local).Format("2006-01-02 15:04 MST"))
			builder.WriteString("; utc ")
			builder.WriteString(event.DueAt.UTC().Format("2006-01-02 15:04 UTC"))
			builder.WriteString(")")
		}
	}

	return builder.String()
}

func resolveHeartbeatChecklistPath(root string) (string, string) {
	canonicalPath := filepath.Join(root, heartbeatFileName)
	if info, err := os.Stat(canonicalPath); err == nil && !info.IsDir() {
		return canonicalPath, heartbeatFileName
	}

	lowerName := strings.ToLower(heartbeatFileName)
	lowerPath := filepath.Join(root, lowerName)
	if info, err := os.Stat(lowerPath); err == nil && !info.IsDir() {
		return lowerPath, lowerName
	}

	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.EqualFold(entry.Name(), heartbeatFileName) {
				return filepath.Join(root, entry.Name()), entry.Name()
			}
		}
	}

	return canonicalPath, heartbeatFileName
}

func (s *Service) handleHeartbeatReply(prompt inboundPrompt, reply string) {
	disposition := classifyHeartbeatReply(reply, s.cfg.Heartbeat.AckMaxChars)
	if disposition.Content == "" && disposition.Quiet {
		disposition.Content = heartbeatOKToken
	}
	if disposition.Quiet && !s.cfg.Heartbeat.ShowOK {
		return
	}
	if !disposition.Quiet && !s.cfg.Heartbeat.ShowAlerts {
		return
	}
	if strings.TrimSpace(disposition.Content) == "" {
		return
	}
	if err := s.sendReply(prompt, disposition.Content); err != nil {
		s.audit.Write("error", "", map[string]any{"op": "send_heartbeat_reply", "error": err.Error()})
	}
}

func classifyHeartbeatReply(reply string, ackMaxChars int) heartbeatReplyDisposition {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return heartbeatReplyDisposition{}
	}

	var (
		content    string
		recognized bool
	)
	switch {
	case strings.HasPrefix(trimmed, heartbeatOKToken):
		content = strings.TrimSpace(strings.TrimPrefix(trimmed, heartbeatOKToken))
		recognized = true
	case strings.HasSuffix(trimmed, heartbeatOKToken):
		content = strings.TrimSpace(strings.TrimSuffix(trimmed, heartbeatOKToken))
		recognized = true
	default:
		content = trimmed
	}

	if recognized && utf8.RuneCountInString(content) <= ackMaxChars {
		return heartbeatReplyDisposition{Content: content, Quiet: true}
	}

	return heartbeatReplyDisposition{Content: content, Quiet: false}
}

func heartbeatChecklistState(path string) (bool, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return true, true, nil
	}

	return true, false, nil
}

func (s *Service) withinHeartbeatActiveHours(now time.Time) bool {
	start := strings.TrimSpace(s.cfg.Heartbeat.ActiveHours.Start)
	end := strings.TrimSpace(s.cfg.Heartbeat.ActiveHours.End)
	if start == "" || end == "" {
		return true
	}

	location, err := s.cfg.HeartbeatLocation()
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "heartbeat_timezone", "error": err.Error()})
		return true
	}

	startMinutes, err := parseHeartbeatClock(start)
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "heartbeat_start_time", "error": err.Error()})
		return true
	}
	endMinutes, err := parseHeartbeatClock(end)
	if err != nil {
		s.audit.Write("error", "", map[string]any{"op": "heartbeat_end_time", "error": err.Error()})
		return true
	}

	localNow := now.In(location)
	currentMinutes := localNow.Hour()*60 + localNow.Minute()
	if startMinutes == endMinutes {
		return true
	}
	if startMinutes < endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

func parseHeartbeatClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func consumeHeartbeatEvents(dir string, mode string) ([]queuedHeartbeatEvent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	events := make([]queuedHeartbeatEvent, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var event heartbeatSystemEvent
		if err := json.Unmarshal(data, &event); err != nil {
			_ = os.Remove(path)
			continue
		}
		if event.Mode != mode {
			continue
		}
		events = append(events, queuedHeartbeatEvent{Path: path, Event: event})
	}

	return events, nil
}

func heartbeatEventPayloads(events []queuedHeartbeatEvent) []heartbeatSystemEvent {
	if len(events) == 0 {
		return nil
	}

	payloads := make([]heartbeatSystemEvent, 0, len(events))
	for _, event := range events {
		payloads = append(payloads, event.Event)
	}

	return payloads
}

func acknowledgeHeartbeatEvents(events []queuedHeartbeatEvent) error {
	for _, event := range events {
		if err := os.Remove(event.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func heartbeatEventFileName() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate heartbeat event id: %w", err)
	}
	return fmt.Sprintf("heartbeat-event-%s-%s", time.Now().UTC().Format("20060102-150405.000000000"), hex.EncodeToString(suffix[:])), nil
}
