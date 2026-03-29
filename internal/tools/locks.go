package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type resourceLockManager struct {
	mu      sync.Mutex
	entries map[string]*resourceLockEntry
}

type resourceLockEntry struct {
	ch   chan struct{}
	refs int
}

func newResourceLockManager() *resourceLockManager {
	return &resourceLockManager{
		entries: make(map[string]*resourceLockEntry),
	}
}

func (m *resourceLockManager) Acquire(ctx context.Context, keys ...string) (func(), error) {
	if m == nil {
		return func() {}, nil
	}

	normalized := normalizeResourceKeys(keys)
	if len(normalized) == 0 {
		return func() {}, nil
	}

	entries := make([]*resourceLockEntry, 0, len(normalized))
	m.mu.Lock()
	for _, key := range normalized {
		entry := m.entries[key]
		if entry == nil {
			entry = &resourceLockEntry{ch: make(chan struct{}, 1)}
			entry.ch <- struct{}{}
			m.entries[key] = entry
		}
		entry.refs++
		entries = append(entries, entry)
	}
	m.mu.Unlock()

	acquired := 0
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			m.releaseEntries(normalized[:acquired], entries[:acquired])
			m.releaseRefs(normalized[acquired:], entries[acquired:])
			return nil, fmt.Errorf("acquire resource lock: %w", ctx.Err())
		case <-entry.ch:
			acquired++
		}
	}

	released := false
	return func() {
		if released {
			return
		}
		released = true
		m.releaseEntries(normalized, entries)
	}, nil
}

func (m *resourceLockManager) releaseEntries(keys []string, entries []*resourceLockEntry) {
	for idx := len(entries) - 1; idx >= 0; idx-- {
		entries[idx].ch <- struct{}{}
	}
	m.releaseRefs(keys, entries)
}

func (m *resourceLockManager) releaseRefs(keys []string, entries []*resourceLockEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for idx, key := range keys {
		entry := entries[idx]
		if entry == nil {
			continue
		}
		entry.refs--
		if entry.refs <= 0 {
			delete(m.entries, key)
		}
	}
}

func normalizeResourceKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	slices.Sort(normalized)
	return normalized
}
