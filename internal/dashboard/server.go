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

	"lumen-agent/internal/auditlog"
	"lumen-agent/internal/config"
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
	Nodes                 []nodeState     `json:"nodes"`
	Edges                 []edgeState     `json:"edges"`
	ToolCalls             []toolCallState `json:"tool_calls"`
	Logs                  []logEntryState `json:"logs"`
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

	state := BuildState(entries, time.Now().UTC(), s.activityWindow, logLimit, toolLimit)
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

		timestamp, err := time.Parse(time.RFC3339, entry.Time)
		if err != nil || timestamp.Before(cutoff) {
			continue
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

	return stateResponse{
		GeneratedAt:           now.UTC().Format(time.RFC3339),
		ActivityWindowSeconds: int(activityWindow.Seconds()),
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
