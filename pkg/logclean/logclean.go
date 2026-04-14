package logclean

import (
	"regexp"
	"strings"
)

// GitHub Actions timestamp prefix: 2024-01-15T10:30:00.1234567Z
var timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s?`)

// Stats reports what the extraction did.
type Stats struct {
	InputLines   int
	OutputLines  int
	DroppedLines int
	FallbackUsed bool
}

// Extract removes noise from a CI job log and returns failure-relevant content,
// capped at maxBytes. If extraction removes too much (< 250 bytes of signal
// from a >500 byte input), it falls back to naive tail truncation.
func Extract(logText string, maxBytes int) (string, Stats) {
	if logText == "" {
		return "", Stats{}
	}
	if len(logText) <= maxBytes {
		lines := strings.Split(logText, "\n")
		return logText, Stats{
			InputLines:  len(lines),
			OutputLines: len(lines),
		}
	}

	lines := strings.Split(logText, "\n")
	stats := Stats{InputLines: len(lines)}

	// Strip timestamps and leading whitespace from each line for classification
	stripped := make([]string, len(lines))
	for i, line := range lines {
		s := timestampRe.ReplaceAllString(line, "")
		stripped[i] = strings.TrimLeft(s, " \t")
	}

	// Phase 1: Find trailing post-job cleanup section and exclude it
	cleanupStart := findTrailingCleanup(stripped)

	// Phase 2: Classify each line
	var signalLines []string
	for i, s := range stripped {
		if cleanupStart >= 0 && i >= cleanupStart {
			stats.DroppedLines++
			continue
		}
		if classifyLine(s) == lineNoise {
			stats.DroppedLines++
			continue
		}
		signalLines = append(signalLines, lines[i]) // preserve original (with timestamp)
	}

	// Safety check: if signal is too small, fall back to naive tail
	result := strings.Join(signalLines, "\n")
	if len(result) < 250 && len(logText) > 500 {
		stats.FallbackUsed = true
		stats.OutputLines = 0
		stats.DroppedLines = 0
		if len(logText) > maxBytes {
			return logText[len(logText)-maxBytes:], stats
		}
		return logText, stats
	}

	stats.OutputLines = len(signalLines)

	// Budget enforcement: if signal exceeds maxBytes, take the tail
	if len(result) > maxBytes {
		result = result[len(result)-maxBytes:]
	}

	return result, stats
}

// findTrailingCleanup finds the start index of the trailing post-job cleanup
// section. Returns -1 if not found.
func findTrailingCleanup(lines []string) int {
	// Scan from the end looking for "Post job cleanup." marker
	// Only match if it's near the end (within last 30% of lines)
	threshold := len(lines) - len(lines)*30/100
	if threshold < 0 {
		threshold = 0
	}

	for i := len(lines) - 1; i >= threshold; i-- {
		if strings.HasPrefix(lines[i], "Post job cleanup.") {
			return i
		}
	}
	return -1
}
