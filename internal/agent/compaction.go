package agent

import (
	"strings"

	"lumen-agent/internal/config"
	"lumen-agent/internal/llm"
)

const (
	baseCompactionChunkRatio = 0.4
	minCompactionChunkRatio  = 0.15
	compactionSafetyMargin   = 1.2
	compactionOverheadTokens = 256
	defaultCompactionParts   = 2
)

func compactHistoryForStorage(cfg config.Config, history []llm.Message) []llm.Message {
	compacted := CompactHistoryForNextTurn(history)
	if !cfg.App.HistoryCompaction.Enabled || len(compacted) == 0 {
		return compacted
	}

	trigger := cfg.HistoryCompactionTriggerTokens()
	target := cfg.HistoryCompactionTargetTokens()
	preserve := cfg.HistoryCompactionPreserveRecentMessages()
	if trigger <= 0 || target <= 0 || preserve <= 0 {
		return compacted
	}
	if approximateHistoryTokens(compacted) <= trigger {
		return compacted
	}
	if len(compacted) <= preserve {
		return compacted
	}

	tailStart := resolveCompactionTailStart(compacted, preserve)
	if tailStart <= 0 || tailStart >= len(compacted) {
		return historyTail(compacted, preserve)
	}

	head := compacted[:tailStart]
	tail := compacted[tailStart:]
	if len(head) == 0 {
		return compacted
	}

	summary := summarizeHistoryAdaptive(head, target, cfg.LLM.ContextWindowTokens)
	if strings.TrimSpace(summary) == "" {
		return compacted
	}

	result := make([]llm.Message, 0, 1+len(tail))
	result = append(result, llm.Message{
		Role:      "system",
		Content:   summary,
		Timestamp: lastHistoryTimestamp(head),
	})
	result = append(result, tail...)
	return normalizeToolCallHistory(result)
}

func resolveCompactionTailStart(history []llm.Message, preserve int) int {
	tailStart := len(history) - preserve
	if tailStart < 1 {
		tailStart = 1
	}
	for tailStart > 0 && tailStart < len(history) && history[tailStart].Role == "tool" {
		tailStart--
	}
	for tailStart > 0 && len(history[tailStart-1].ToolCalls) > 0 {
		tailStart--
	}
	return tailStart
}

func summarizeHistoryAdaptive(history []llm.Message, targetTokens int, contextWindow int) string {
	if len(history) == 0 || targetTokens <= 0 {
		return ""
	}

	maxChunkTokens := max(1, targetTokens-compactionOverheadTokens)
	parts := splitMessagesByTokenShare(history, normalizeCompactionParts(computeAdaptiveChunkRatio(history, contextWindow), len(history)))
	if len(parts) == 0 {
		return ""
	}

	chunks := make([][]llm.Message, 0, len(parts))
	for _, part := range parts {
		for _, chunk := range chunkMessagesByMaxTokens(part, maxChunkTokens) {
			if len(chunk) > 0 {
				chunks = append(chunks, chunk)
			}
		}
	}
	if len(chunks) == 0 {
		return ""
	}

	partialSummaries := make([]string, 0, len(chunks))
	perChunkTarget := max(48, targetTokens/max(len(chunks), 1))
	for _, chunk := range chunks {
		partial := summarizeHistoryChunk(chunk, perChunkTarget)
		if strings.TrimSpace(partial) != "" {
			partialSummaries = append(partialSummaries, partial)
		}
	}
	if len(partialSummaries) == 0 {
		return ""
	}
	if len(partialSummaries) == 1 {
		return partialSummaries[0]
	}
	return mergePartialSummaries(partialSummaries, targetTokens)
}

func summarizeHistoryChunk(history []llm.Message, targetTokens int) string {
	if len(history) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("Auto-generated conversation summary of earlier context:\n")

	maxLines := len(history)
	if maxLines > 48 {
		maxLines = 48
	}

	lines := 0
	for _, message := range history {
		if lines >= maxLines || approximateTextTokens(builder.String()) >= targetTokens {
			break
		}
		line := historySummaryLine(message)
		if line == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(line)
		builder.WriteByte('\n')
		lines++
	}

	return trimSummaryToTarget(strings.TrimSpace(builder.String()), targetTokens)
}

func mergePartialSummaries(partials []string, targetTokens int) string {
	var builder strings.Builder
	builder.WriteString("Auto-generated conversation summary of earlier context:\n")
	for idx, partial := range partials {
		_ = idx
		trimmed := strings.TrimSpace(partial)
		trimmed = strings.TrimPrefix(trimmed, "Auto-generated conversation summary of earlier context:\n")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			continue
		}
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if line == "" {
				continue
			}
			builder.WriteString("- ")
			builder.WriteString(line)
			builder.WriteByte('\n')
			if approximateTextTokens(builder.String()) >= targetTokens {
				return trimSummaryToTarget(strings.TrimSpace(builder.String()), targetTokens)
			}
		}
	}
	return trimSummaryToTarget(strings.TrimSpace(builder.String()), targetTokens)
}

func trimSummaryToTarget(summary string, targetTokens int) string {
	if summary == "" || targetTokens <= 0 {
		return summary
	}
	if approximateTextTokens(summary) <= targetTokens {
		return summary
	}
	runes := []rune(summary)
	limit := targetTokens * 4
	if limit > 3 && len(runes) > limit {
		return strings.TrimSpace(string(runes[:limit-3])) + "..."
	}
	return summary
}

func splitMessagesByTokenShare(messages []llm.Message, parts int) [][]llm.Message {
	if len(messages) == 0 {
		return nil
	}
	parts = max(1, min(parts, len(messages)))
	if parts == 1 {
		return [][]llm.Message{messages}
	}

	totalTokens := approximateHistoryTokens(messages)
	targetTokens := max(1, totalTokens/parts)
	chunks := make([][]llm.Message, 0, parts)
	current := make([]llm.Message, 0)
	currentTokens := 0

	for _, message := range messages {
		messageTokens := approximateMessageTokens(message)
		if len(chunks) < parts-1 && len(current) > 0 && currentTokens+messageTokens > targetTokens {
			chunks = append(chunks, current)
			current = make([]llm.Message, 0)
			currentTokens = 0
		}
		current = append(current, message)
		currentTokens += messageTokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func chunkMessagesByMaxTokens(messages []llm.Message, maxTokens int) [][]llm.Message {
	if len(messages) == 0 {
		return nil
	}
	effectiveMax := max(1, int(float64(maxTokens)/compactionSafetyMargin))
	chunks := make([][]llm.Message, 0)
	current := make([]llm.Message, 0)
	currentTokens := 0

	for _, message := range messages {
		messageTokens := approximateMessageTokens(message)
		if len(current) > 0 && currentTokens+messageTokens > effectiveMax {
			chunks = append(chunks, current)
			current = make([]llm.Message, 0)
			currentTokens = 0
		}
		current = append(current, message)
		currentTokens += messageTokens
		if messageTokens > effectiveMax {
			chunks = append(chunks, current)
			current = make([]llm.Message, 0)
			currentTokens = 0
		}
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func computeAdaptiveChunkRatio(messages []llm.Message, contextWindow int) float64 {
	if len(messages) == 0 || contextWindow <= 0 {
		return baseCompactionChunkRatio
	}
	totalTokens := approximateHistoryTokens(messages)
	avgTokens := float64(totalTokens) / float64(len(messages))
	avgRatio := (avgTokens * compactionSafetyMargin) / float64(contextWindow)
	if avgRatio <= 0.10 {
		return baseCompactionChunkRatio
	}
	if avgRatio >= 0.25 {
		return minCompactionChunkRatio
	}
	scale := (avgRatio - 0.10) / 0.15
	return baseCompactionChunkRatio - ((baseCompactionChunkRatio - minCompactionChunkRatio) * scale)
}

func normalizeCompactionParts(ratio float64, messageCount int) int {
	if messageCount <= 1 {
		return 1
	}
	if ratio <= 0 {
		return defaultCompactionParts
	}
	parts := int(1.0 / ratio)
	if parts < 1 {
		parts = 1
	}
	if parts > messageCount {
		parts = messageCount
	}
	if parts == 1 && messageCount > 1 {
		return defaultCompactionParts
	}
	return parts
}
