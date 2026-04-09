package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"element-orion/internal/auditlog"
	"element-orion/internal/config"
)

const (
	defaultActivityWindow = 8 * time.Second
	defaultRecentEntries  = 600
	defaultLogLimit       = 200
	defaultToolLimit      = 120
)

//go:embed ui/*
var embeddedUI embed.FS

type stateResponse struct {
	GeneratedAt           string          `json:"generated_at"`
	ActivityWindowSeconds int             `json:"activity_window_seconds"`
	Summary               summaryState    `json:"summary"`
	Memory                memoryState     `json:"memory"`
	Config                configState     `json:"config"`
	Nodes                 []nodeState     `json:"nodes"`
	Edges                 []edgeState     `json:"edges"`
	ToolCalls             []toolCallState `json:"tool_calls"`
	Logs                  []logEntryState `json:"logs"`
}

type summaryState struct {
	RecentTokens     int `json:"recent_tokens"`
	ModelCalls       int `json:"model_calls"`
	RecentToolCalls  int `json:"recent_tool_calls"`
	ToolFailures     int `json:"tool_failures"`
	ActiveNodes      int `json:"active_nodes"`
	ActiveSessions   int `json:"active_sessions"`
	BackgroundEvents int `json:"background_events"`
}

type memoryState struct {
	Available              bool   `json:"available"`
	LoadMode               string `json:"load_mode"`
	FileCount              int    `json:"file_count"`
	ShardCount             int    `json:"shard_count"`
	LoadedShards           int    `json:"loaded_shards"`
	TotalBytes             int64  `json:"total_bytes"`
	HasCuratedMemory       bool   `json:"has_curated_memory"`
	CompactionEnabled      bool   `json:"compaction_enabled"`
	CompactionTriggerToken int    `json:"compaction_trigger_tokens"`
	CompactionTargetToken  int    `json:"compaction_target_tokens"`
	PreserveRecentMessages int    `json:"preserve_recent_messages"`
}

type configState struct {
	Sections []configSectionState `json:"sections"`
}

type configSectionState struct {
	Title string            `json:"title"`
	Items []configItemState `json:"items"`
}

type configItemState struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type nodeState struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
}

type edgeState struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Active bool   `json:"active"`
}

type toolCallState struct {
	Time       string         `json:"time"`
	SessionID  string         `json:"session_id,omitempty"`
	Kind       string         `json:"kind"`
	Tool       string         `json:"tool"`
	Detail     string         `json:"detail,omitempty"`
	FullDetail string         `json:"full_detail,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Success    *bool          `json:"success,omitempty"`
	Background bool           `json:"background"`
	Raw        map[string]any `json:"raw,omitempty"`
}

type logEntryState struct {
	Time      string         `json:"time"`
	Kind      string         `json:"kind"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type Server struct {
	cfg            config.Config
	basePath       string
	activityWindow time.Duration
}

func Run(ctx context.Context, cfg config.Config) error {
	if !cfg.Dashboard.Enabled {
		return nil
	}

	httpServer := &http.Server{
		Addr:              cfg.Dashboard.ListenAddr,
		Handler:           Handler(cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		basePath := normalizeBasePath(cfg.Dashboard.Path)
		log.Printf("dashboard: listening on %s%s", cfg.Dashboard.ListenAddr, basePath)
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("dashboard: listen failed on %s: %v", cfg.Dashboard.ListenAddr, err)
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

func Handler(cfg config.Config) http.Handler {
	server := &Server{
		cfg:            cfg,
		basePath:       normalizeBasePath(cfg.Dashboard.Path),
		activityWindow: defaultActivityWindow,
	}
	return server.routes()
}

func (s *Server) routes() http.Handler {
	sub := http.NewServeMux()
	sub.HandleFunc("/", s.handleIndex)
	sub.HandleFunc("/app.css", s.handleCSS)
	sub.HandleFunc("/app.js", s.handleJS)
	sub.HandleFunc("/api/state", s.handleState)

	if s.basePath == "/" {
		return sub
	}

	root := http.NewServeMux()
	root.HandleFunc(s.basePath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.basePath+"/", http.StatusTemporaryRedirect)
	})
	root.Handle(s.basePath+"/", http.StripPrefix(s.basePath, sub))
	return root
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	content, err := readEmbeddedFile("index.html")
	if err != nil {
		http.Error(w, "dashboard index unavailable", http.StatusInternalServerError)
		return
	}
	content = strings.ReplaceAll(content, "__BASE_PATH__", s.basePath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

func (s *Server) handleCSS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.css" {
		http.NotFound(w, r)
		return
	}
	content, err := readEmbeddedFile("app.css")
	if err != nil {
		http.Error(w, "dashboard css unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

func (s *Server) handleJS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	content, err := readEmbeddedFile("app.js")
	if err != nil {
		http.Error(w, "dashboard js unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(content))
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logLimit := queryInt(r, "limit", defaultLogLimit)
	toolLimit := queryInt(r, "tool_limit", defaultToolLimit)
	readLimit := max(defaultRecentEntries, logLimit*3, toolLimit*4)

	entries, err := readRecentEntries(s.cfg.LogDir(), readLimit)
	if err != nil {
		http.Error(w, fmt.Sprintf("read logs: %v", err), http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	state := BuildState(entries, now, s.activityWindow, logLimit, toolLimit)
	state.Memory = buildMemoryState(s.cfg)
	state.Config = buildConfigState(s.cfg)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(state)
}

func BuildState(entries []auditlog.Entry, now time.Time, activityWindow time.Duration, logLimit int, toolLimit int) stateResponse {
	sorted := append([]auditlog.Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Time > sorted[j].Time
	})

	if logLimit <= 0 {
		logLimit = defaultLogLimit
	}
	if toolLimit <= 0 {
		toolLimit = defaultToolLimit
	}
	if activityWindow <= 0 {
		activityWindow = defaultActivityWindow
	}

	cutoff := now.Add(-activityWindow)
	activeNodes := map[string]bool{
		"discord": false,
		"agent":   false,
		"llms":    false,
		"tool":    false,
	}
	activeEdges := map[string]bool{
		"discord-agent": false,
		"agent-llms":    false,
		"llms-tool":     false,
	}
	activeSessions := make(map[string]struct{})
	summary := summaryState{}

	toolCalls := make([]toolCallState, 0, min(toolLimit, len(sorted)))
	logs := make([]logEntryState, 0, min(logLimit, len(sorted)))

	for _, entry := range sorted {
		if len(logs) < logLimit {
			logs = append(logs, logEntryState{
				Time:      entry.Time,
				Kind:      entry.Kind,
				SessionID: entry.SessionID,
				Data:      cloneMap(entry.Data),
			})
		}

		if len(toolCalls) < toolLimit {
			if call, ok := toToolCall(entry); ok {
				toolCalls = append(toolCalls, call)
			}
		}

		switch entry.Kind {
		case "model_done", "background_model_done":
			summary.ModelCalls++
			summary.RecentTokens += int(int64FromMap(entry.Data, "tokens"))
		case "tool_done", "background_tool_done":
			summary.RecentToolCalls++
			if success, ok := boolFromMap(entry.Data, "success"); ok && !success {
				summary.ToolFailures++
			}
		}
		if strings.HasPrefix(entry.Kind, "background_") {
			summary.BackgroundEvents++
		}

		timestamp, err := time.Parse(time.RFC3339, entry.Time)
		if err != nil || timestamp.Before(cutoff) {
			continue
		}
		if strings.TrimSpace(entry.SessionID) != "" {
			activeSessions[entry.SessionID] = struct{}{}
		}

		switch entry.Kind {
		case "turn_start", "assistant_reply", "background_assistant":
			activeNodes["discord"] = true
			activeNodes["agent"] = true
			activeEdges["discord-agent"] = true
		case "model_done", "background_model_done":
			activeNodes["agent"] = true
			activeNodes["llms"] = true
			activeEdges["agent-llms"] = true
		case "tool_start", "tool_done", "background_tool_start", "background_tool_done":
			activeNodes["agent"] = true
			activeNodes["llms"] = true
			activeNodes["tool"] = true
			activeEdges["agent-llms"] = true
			activeEdges["llms-tool"] = true
		case "status", "background_status":
			activeNodes["agent"] = true
		}
	}
	for _, active := range activeNodes {
		if active {
			summary.ActiveNodes++
		}
	}
	summary.ActiveSessions = len(activeSessions)

	return stateResponse{
		GeneratedAt:           now.UTC().Format(time.RFC3339),
		ActivityWindowSeconds: int(activityWindow.Seconds()),
		Summary:               summary,
		Nodes: []nodeState{
			{ID: "discord", Active: activeNodes["discord"]},
			{ID: "agent", Active: activeNodes["agent"]},
			{ID: "llms", Active: activeNodes["llms"]},
			{ID: "tool", Active: activeNodes["tool"]},
		},
		Edges: []edgeState{
			{ID: "discord-agent", From: "discord", To: "agent", Active: activeEdges["discord-agent"]},
			{ID: "agent-llms", From: "agent", To: "llms", Active: activeEdges["agent-llms"]},
			{ID: "llms-tool", From: "llms", To: "tool", Active: activeEdges["llms-tool"]},
		},
		ToolCalls: toolCalls,
		Logs:      logs,
	}
}

func buildMemoryState(cfg config.Config) memoryState {
	state := memoryState{
		LoadMode:               "current + previous",
		CompactionEnabled:      cfg.App.HistoryCompaction.Enabled,
		CompactionTriggerToken: cfg.HistoryCompactionTriggerTokens(),
		CompactionTargetToken:  cfg.HistoryCompactionTargetTokens(),
		PreserveRecentMessages: cfg.HistoryCompactionPreserveRecentMessages(),
	}
	if cfg.App.LoadAllMemoryShards {
		state.LoadMode = "all shards"
	}

	entries, err := os.ReadDir(cfg.App.MemoryDir)
	if err != nil {
		return state
	}
	state.Available = true

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		state.FileCount++
		if entry.Name() == "MEMORY.md" {
			state.HasCuratedMemory = true
		}
		if isMemoryShardFile(entry.Name()) {
			state.ShardCount++
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			state.TotalBytes += info.Size()
		}
	}

	state.LoadedShards = min(2, state.ShardCount)
	if cfg.App.LoadAllMemoryShards {
		state.LoadedShards = state.ShardCount
	}

	return state
}

func buildConfigState(cfg config.Config) configState {
	return configState{
		Sections: []configSectionState{
			{
				Title: "Runtime",
				Items: []configItemState{
					{Key: "LLM", Value: fallbackString(cfg.LLM.Model, "—")},
					{Key: "Workspace", Value: fallbackString(cfg.App.WorkspaceRoot, "—")},
					{Key: "Session dir", Value: fallbackString(cfg.App.SessionDir, "—")},
					{Key: "Logs", Value: fallbackString(cfg.LogDir(), "—")},
				},
			},
			{
				Title: "Heartbeat",
				Items: []configItemState{
					{Key: "Enabled", Value: yesNo(cfg.HeartbeatEnabled())},
					{Key: "Schedule", Value: fallbackString(cfg.Heartbeat.Every, "off")},
					{Key: "Model", Value: fallbackString(cfg.HeartbeatModel(), "—")},
					{Key: "Context", Value: heartbeatContextSummary(cfg)},
					{Key: "Delivery", Value: heartbeatDeliverySummary(cfg)},
					{Key: "Active hours", Value: heartbeatActiveHoursSummary(cfg)},
					{Key: "Target", Value: heartbeatTargetSummary(cfg)},
					{Key: "Poll interval", Value: fallbackString(cfg.Heartbeat.EventPollInterval, "—")},
				},
			},
			{
				Title: "Background Tasks",
				Items: []configItemState{
					{Key: "Min runtime", Value: fallbackString(cfg.BackgroundTasks.DefaultMinRuntime, "default")},
					{Key: "Inject time", Value: yesNo(cfg.BackgroundTasks.InjectCurrentTime)},
					{Key: "Event log cap", Value: strconv.Itoa(cfg.BackgroundTaskMaxEventLogEntries())},
					{Key: "Sandbox", Value: backgroundSandboxSummary(cfg)},
				},
			},
			{
				Title: "Endpoints",
				Items: []configItemState{
					{Key: "Dashboard", Value: endpointSummary(cfg.Dashboard.Enabled, cfg.Dashboard.ListenAddr, cfg.Dashboard.Path)},
					{Key: "Webhook", Value: webhookSummary(cfg)},
				},
			},
			{
				Title: "MCP",
				Items: buildMCPItems(cfg),
			},
		},
	}
}

func fallbackString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func heartbeatContextSummary(cfg config.Config) string {
	contextMode := "full"
	if cfg.Heartbeat.LightContext {
		contextMode = "light"
	}
	sessionMode := "shared"
	if cfg.Heartbeat.IsolatedSession {
		sessionMode = "isolated"
	}
	return contextMode + " / " + sessionMode
}

func heartbeatDeliverySummary(cfg config.Config) string {
	parts := []string{
		"ok " + onOff(cfg.Heartbeat.ShowOK),
		"alerts " + onOff(cfg.Heartbeat.ShowAlerts),
		"indicator " + onOff(cfg.Heartbeat.UseIndicator),
	}
	return strings.Join(parts, " · ")
}

func heartbeatActiveHoursSummary(cfg config.Config) string {
	start := strings.TrimSpace(cfg.Heartbeat.ActiveHours.Start)
	end := strings.TrimSpace(cfg.Heartbeat.ActiveHours.End)
	if start == "" || end == "" {
		return "always"
	}
	timezone := strings.TrimSpace(cfg.Heartbeat.ActiveHours.Timezone)
	if timezone == "" {
		timezone = "local"
	}
	return start + "-" + end + " (" + timezone + ")"
}

func heartbeatTargetSummary(cfg config.Config) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(cfg.Heartbeat.Target.GuildID) != "" {
		parts = append(parts, "guild")
	}
	if strings.TrimSpace(cfg.Heartbeat.Target.ChannelID) != "" {
		parts = append(parts, "channel")
	}
	if strings.TrimSpace(cfg.Heartbeat.Target.UserID) != "" {
		parts = append(parts, "user")
	}
	if len(parts) == 0 {
		return "unconfigured"
	}
	return strings.Join(parts, " + ")
}

func backgroundSandboxSummary(cfg config.Config) string {
	sandbox := cfg.BackgroundTasks.Sandbox
	if !sandbox.Enabled {
		return "disabled"
	}
	parts := []string{
		fallbackString(sandbox.Provider, "sandbox"),
		"force " + onOff(sandbox.Force),
		"cleanup " + onOff(sandbox.AutoCleanup),
	}
	return strings.Join(parts, " · ")
}

func endpointSummary(enabled bool, listenAddr string, path string) string {
	status := "off"
	if enabled {
		status = "on"
	}
	listen := fallbackString(listenAddr, "—")
	endpointPath := fallbackString(path, "/")
	return status + " · " + listen + endpointPath
}

func webhookSummary(cfg config.Config) string {
	summary := endpointSummary(cfg.EventWebhook.Enabled, cfg.EventWebhook.ListenAddr, cfg.EventWebhook.Path)
	return summary + " · mode " + fallbackString(cfg.EventWebhook.DefaultMode, "now")
}

func buildMCPItems(cfg config.Config) []configItemState {
	servers := cfg.MCP.Servers
	enabledCount := 0
	items := make([]configItemState, 0, len(servers)+1)
	for _, server := range servers {
		if server.Enabled {
			enabledCount++
		}
	}
	items = append(items, configItemState{
		Key:   "Servers",
		Value: fmt.Sprintf("%d configured / %d enabled", len(servers), enabledCount),
	})
	if len(servers) == 0 {
		items = append(items, configItemState{Key: "Status", Value: "none configured"})
		return items
	}
	for _, server := range servers {
		items = append(items, configItemState{
			Key:   fallbackString(server.Name, "unnamed"),
			Value: mcpServerSummary(server),
		})
	}
	return items
}

func mcpServerSummary(server config.MCPServerConfig) string {
	status := "disabled"
	if server.Enabled {
		status = "enabled"
	}
	switch server.Transport {
	case "http", "streamable_http":
		return status + " · " + server.Transport + " · " + fallbackString(server.Endpoint, "—")
	default:
		command := fallbackString(server.Command, "—")
		if len(server.Args) > 0 {
			command += " " + strings.Join(server.Args, " ")
		}
		return status + " · stdio · " + command
	}
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func isMemoryShardFile(name string) bool {
	if !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
		return false
	}

	base := strings.TrimSuffix(name, ".md")
	if strings.HasSuffix(base, "-AM") {
		base = strings.TrimSuffix(base, "-AM")
	} else if strings.HasSuffix(base, "-PM") {
		base = strings.TrimSuffix(base, "-PM")
	} else {
		return false
	}

	_, err := time.Parse("2006-01-02", base)
	return err == nil
}

func readRecentEntries(dir string, limit int) ([]auditlog.Entry, error) {
	if limit <= 0 {
		return nil, nil
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	paths := make([]string, 0, len(items))
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".ndjson") {
			continue
		}
		paths = append(paths, filepath.Join(dir, item.Name()))
	}
	sort.Slice(paths, func(i, j int) bool {
		return filepath.Base(paths[i]) > filepath.Base(paths[j])
	})

	entries := make([]auditlog.Entry, 0, min(limit, 256))
	for _, path := range paths {
		if len(entries) >= limit {
			break
		}
		fileEntries, err := readEntriesFromFile(path, limit-len(entries))
		if err != nil {
			return nil, err
		}
		entries = append(entries, fileEntries...)
	}

	return entries, nil
}

func readEntriesFromFile(path string, limit int) ([]auditlog.Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}

	lines := strings.Split(trimmed, "\n")
	entries := make([]auditlog.Entry, 0, min(limit, len(lines)))
	for i := len(lines) - 1; i >= 0; i-- {
		if len(entries) >= limit {
			break
		}
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var entry auditlog.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func toToolCall(entry auditlog.Entry) (toolCallState, bool) {
	switch entry.Kind {
	case "tool_start", "tool_done", "background_tool_start", "background_tool_done":
	default:
		return toolCallState{}, false
	}

	success, hasSuccess := boolFromMap(entry.Data, "success")
	call := toolCallState{
		Time:       entry.Time,
		SessionID:  entry.SessionID,
		Kind:       entry.Kind,
		Tool:       stringFromMap(entry.Data, "tool"),
		Detail:     stringFromMap(entry.Data, "detail"),
		FullDetail: stringFromMap(entry.Data, "full_detail"),
		DurationMS: int64FromMap(entry.Data, "duration_ms"),
		Background: strings.HasPrefix(entry.Kind, "background_"),
		Raw:        cloneMap(entry.Data),
	}
	if hasSuccess {
		call.Success = &success
	}
	return call, true
}

func queryInt(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func normalizeBasePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return "/"
	}
	return "/" + strings.Trim(path, "/")
}

func readEmbeddedFile(name string) (string, error) {
	sub, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		return "", err
	}
	data, err := fs.ReadFile(sub, name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func stringFromMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func boolFromMap(values map[string]any, key string) (bool, bool) {
	if len(values) == 0 {
		return false, false
	}
	value, ok := values[key]
	if !ok {
		return false, false
	}
	flag, ok := value.(bool)
	return flag, ok
}

func int64FromMap(values map[string]any, key string) int64 {
	if len(values) == 0 {
		return 0
	}
	value, ok := values[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	default:
		return 0
	}
}

func cloneMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func min(values ...int) int {
	filtered := make([]int, 0, len(values))
	for _, value := range values {
		if value > 0 {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		return 0
	}
	slices.Sort(filtered)
	return filtered[0]
}

func max(values ...int) int {
	highest := 0
	for _, value := range values {
		if value > highest {
			highest = value
		}
	}
	return highest
}
