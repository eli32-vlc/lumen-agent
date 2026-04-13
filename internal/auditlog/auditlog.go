// Package auditlog writes structured NDJSON audit entries to daily log files
// under <log_dir>/YYYY-MM-DD.ndjson. Files older than retainDays are pruned
// whenever a new file is opened. The logger is safe for concurrent use.
package auditlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const retainDays = 2

const redactedValue = "[redacted]"

var redactedKeys = []string{
	"message",
	"messages",
	"content",
	"raw_content",
	"detail",
	"full_detail",
	"user_parts",
	"parts",
	"tool_calls",
	"response_items",
	"request_payload",
	"raw_response",
}

// Entry is a single log record. Extra fields are in Data.
type Entry struct {
	Time      string         `json:"time"`
	Kind      string         `json:"kind"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Logger writes to daily NDJSON files and prunes old ones.
type Logger struct {
	dir string

	mu   sync.Mutex
	day  string // current open file's date label, e.g. "2026-03-13"
	file *os.File
}

// New creates a Logger that stores files in dir.
// The directory is created if it does not exist.
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}
	l := &Logger{dir: dir}
	if err := l.pruneOld(time.Now()); err != nil {
		// non-fatal
		_ = err
	}
	return l, nil
}

// Write appends a structured entry to today's log file.
func (l *Logger) Write(kind string, sessionID string, data map[string]any) {
	now := time.Now().UTC()
	entry := Entry{
		Time:      now.Format(time.RFC3339),
		Kind:      kind,
		SessionID: sessionID,
		Data:      sanitizeAuditData(data),
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.rotateIfNeeded(now); err != nil {
		return
	}

	_, _ = l.file.Write(append(line, '\n'))
}

func sanitizeAuditData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return data
	}

	sanitized := make(map[string]any, len(data))
	for key, value := range data {
		sanitized[key] = sanitizeAuditValue(key, value)
	}
	return sanitized
}

func sanitizeAuditValue(key string, value any) any {
	if slices.Contains(redactedKeys, key) {
		return redactedValue
	}

	switch typed := value.(type) {
	case map[string]any:
		nested := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			nested[nestedKey] = sanitizeAuditValue(nestedKey, nestedValue)
		}
		return nested
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = sanitizeAuditValue("", item)
		}
		return items
	default:
		return value
	}
}

// Close flushes and closes the current file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

func (l *Logger) rotateIfNeeded(now time.Time) error {
	day := now.Format("2006-01-02")
	if l.file != nil && l.day == day {
		return nil
	}

	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}

	path := filepath.Join(l.dir, day+".ndjson")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log file: %w", err)
	}

	l.file = f
	l.day = day

	_ = l.pruneOld(now) // best-effort cleanup on rotation
	return nil
}

func (l *Logger) pruneOld(now time.Time) error {
	cutoff := now.AddDate(0, 0, -retainDays)
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) < 10 || name[len(name)-7:] != ".ndjson" {
			continue
		}
		dateStr := name[:10]
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			_ = os.Remove(filepath.Join(l.dir, name))
		}
	}
	return nil
}
