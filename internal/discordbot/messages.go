package discordbot

import (
	"strings"
	"unicode/utf8"
)

const discordMessageLimit = 2000

func splitOutgoingMessages(content string) []string {
	rawParts := strings.Split(content, "<chunk>")
	parts := make([]string, 0, len(rawParts))
	for _, raw := range rawParts {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parts = append(parts, splitDiscordSized(trimmed)...)
	}
	return parts
}

func splitDiscordSized(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	if utf8.RuneCountInString(trimmed) <= discordMessageLimit {
		return []string{trimmed}
	}

	runes := []rune(trimmed)
	parts := make([]string, 0, (len(runes)/discordMessageLimit)+1)

	for len(runes) > 0 {
		if len(runes) <= discordMessageLimit {
			parts = append(parts, strings.TrimSpace(string(runes)))
			break
		}

		cut := findSplitPoint(string(runes[:discordMessageLimit]))
		if cut <= 0 || cut > discordMessageLimit {
			cut = discordMessageLimit
		}

		parts = append(parts, strings.TrimSpace(string(runes[:cut])))
		runes = []rune(strings.TrimSpace(string(runes[cut:])))
	}

	return parts
}

func findSplitPoint(prefix string) int {
	preferred := []string{"\n\n", "\n", ". ", "! ", "? ", " ", ", "}
	best := 0

	for _, token := range preferred {
		idx := strings.LastIndex(prefix, token)
		if idx <= 0 {
			continue
		}

		candidate := utf8.RuneCountInString(prefix[:idx+len(token)])
		if candidate >= discordMessageLimit/2 {
			return candidate
		}
		if candidate > best {
			best = candidate
		}
	}

	return best
}
