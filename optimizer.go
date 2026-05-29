package main

import (
	"fmt"
	"strings"
)

// estimateTokens provides a fast, lightweight heuristic for token counting.
// It assumes 1 token is roughly equivalent to 4 bytes of text natively.
func estimateTokens(text string) int {
	return len(text) / 4
}

// cleanWhitespace strips redundant spaces, tabs, and carriage returns to squeeze maximum reasoning capacity.
func cleanWhitespace(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", " ")
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	return strings.TrimSpace(text)
}

// OptimizeHistory implements the Context-Slicing Middleware to compress token history
// and prevent VRAM exhaustion or context bloat.
func OptimizeHistory(history []Message, tokenLimit int) []Message {
	if len(history) == 0 {
		return history
	}

	totalTokens := 0
	for _, msg := range history {
		totalTokens += estimateTokens(msg.Content)
	}

	if totalTokens <= tokenLimit {
		return history
	}

	var optimized []Message

	// a) Always preserve the initial system message at index 0 (Stamis core identity)
	startIndex := 0
	if history[0].Role == "system" {
		optimized = append(optimized, history[0])
		startIndex = 1
	}

	// b) Always preserve the last 3 turns completely intact
	preserveCount := 3
	preserveIndex := len(history) - preserveCount
	if preserveIndex < startIndex {
		preserveIndex = startIndex
	}

	// c) Consolidate the middle message turns into a compressed bulleted History Summary
	if preserveIndex > startIndex {
		var summaryBuilder strings.Builder
		summaryBuilder.WriteString("[Archived History Summary]\n")
		
		droppedTokens := 0
		for i := startIndex; i < preserveIndex; i++ {
			role := strings.ToUpper(string(history[i].Role[0])) + history[i].Role[1:]
			content := cleanWhitespace(history[i].Content)
			
			// Truncate ultra-long individual messages in the summary to save even more space
			if len(content) > 150 {
				content = content[:147] + "..."
			}
			
			summaryBuilder.WriteString(fmt.Sprintf("* %s: %s\n", role, content))
			droppedTokens += estimateTokens(history[i].Content)
		}

		if droppedTokens > 0 {
			optimized = append(optimized, Message{
				Role:    "system",
				Content: summaryBuilder.String(),
			})
		}
	}

	// Append the preserved recent messages intact, ensuring active context remains sharp
	for i := preserveIndex; i < len(history); i++ {
		optimized = append(optimized, history[i])
	}

	return optimized
}
