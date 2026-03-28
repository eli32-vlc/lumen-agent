package llm

import (
	"regexp"
	"strings"
)

var messageTimeLinePattern = regexp.MustCompile(`(?m)^\[message_time [^\]]+\]\s*$`)

func StripMessageTimeMetadata(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	cleaned := messageTimeLinePattern.ReplaceAllString(trimmed, "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	return cleaned
}
