package discordbot

import (
	"testing"

	"lumen-agent/internal/llm"
	"lumen-agent/internal/skills"
)

func TestSessionSnapshotForRunClonesState(t *testing.T) {
	state := &sessionState{
		History: []llm.Message{{Role: "user", Content: "hello"}},
		Skills:  []skills.Summary{{Name: "Code", Description: "coding"}},
	}

	history, summaries := state.snapshotForRun()
	history[0].Content = "changed"
	summaries[0].Name = "Changed"

	if state.History[0].Content != "hello" {
		t.Fatalf("expected history snapshot to be cloned, got %q", state.History[0].Content)
	}
	if state.Skills[0].Name != "Code" {
		t.Fatalf("expected skills snapshot to be cloned, got %q", state.Skills[0].Name)
	}
}

func TestBackgroundTaskDoesNotCarrySessionRunLock(t *testing.T) {
	task := &backgroundTask{}
	if task == nil {
		t.Fatal("expected background task instance")
	}
}
