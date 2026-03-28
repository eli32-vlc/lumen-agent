package discordbot

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitOutgoingMessagesChunks(t *testing.T) {
	parts := splitOutgoingMessages("First reply<chunk>Second reply\n\nMore text")
	if len(parts) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(parts))
	}
	if parts[0] != "First reply" {
		t.Fatalf("unexpected first part: %q", parts[0])
	}
	if parts[1] != "Second reply\n\nMore text" {
		t.Fatalf("unexpected second part: %q", parts[1])
	}
}

func TestSplitOutgoingMessagesDiscordLimit(t *testing.T) {
	text := strings.Repeat("a", discordMessageLimit+250)
	parts := splitOutgoingMessages(text)
	if len(parts) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(parts))
	}
	for i, part := range parts {
		if utf8.RuneCountInString(part) > discordMessageLimit {
			t.Fatalf("part %d exceeds Discord limit: %d", i, utf8.RuneCountInString(part))
		}
	}
	if strings.Join(parts, "") != text {
		t.Fatalf("joined parts did not match original text")
	}
}
