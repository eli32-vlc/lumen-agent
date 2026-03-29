package tools

import (
	"context"
	"testing"
	"time"
)

func TestResourceLockManagerBlocksSameKeyUntilRelease(t *testing.T) {
	manager := newResourceLockManager()

	release, err := manager.Acquire(context.Background(), "file:/tmp/demo")
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := manager.Acquire(ctx, "file:/tmp/demo"); err == nil {
		t.Fatal("expected second acquire on same key to block until context timeout")
	}
}

func TestNormalizeResourceKeysSortsAndDeduplicates(t *testing.T) {
	got := normalizeResourceKeys([]string{
		"file:/b",
		"file:/a",
		"file:/b",
		"",
		" file:/c ",
	})

	want := []string{"file:/a", "file:/b", "file:/c"}
	if len(got) != len(want) {
		t.Fatalf("expected %d keys, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected key %d to be %q, got %q", i, want[i], got[i])
		}
	}
}
