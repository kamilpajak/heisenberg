package logclean

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	weightedFlagEnv    = "HEISENBERG_LOG_WEIGHTED"
	adjacencyBoostSize = 3
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

	// Budget enforcement: if signal exceeds maxBytes, select by weight (if enabled)
	// or take the tail.
	if len(result) > maxBytes {
		if weightedEnabled() {
			result, stats.OutputLines = selectByWeight(lines, stripped, cleanupStart, maxBytes)
			stats.DroppedLines = stats.InputLines - stats.OutputLines
		} else {
			result = result[len(result)-maxBytes:]
		}
	}

	return result, stats
}

func weightedEnabled() bool {
	return os.Getenv(weightedFlagEnv) == "1"
}

// selectByWeight picks the highest-weight lines that fit in maxBytes, preserving
// chronological order. Returns the joined result and the count of selected lines.
func selectByWeight(original, stripped []string, cleanupStart, maxBytes int) (string, int) {
	type indexedLine struct {
		idx    int
		weight int
		size   int
	}

	candidates := make([]indexedLine, 0, len(stripped))
	for i, s := range stripped {
		if cleanupStart >= 0 && i >= cleanupStart {
			continue
		}
		if classifyLine(s) == lineNoise {
			continue
		}
		w := classifyWeight(s)
		candidates = append(candidates, indexedLine{idx: i, weight: w, size: len(original[i]) + 1})
	}

	// Adjacency boost: raise weight of up to adjacencyBoostSize lines that
	// follow a critical line, so stack frames and context stay with their
	// panic/exception header. Boost to weightError so they compete at the
	// error tier (not critical — we don't want them to outrank fresh panics).
	for i, c := range candidates {
		if c.weight != weightCritical {
			continue
		}
		boostTarget := c.idx + adjacencyBoostSize
		for j := i + 1; j < len(candidates) && candidates[j].idx <= boostTarget; j++ {
			if candidates[j].weight < weightError {
				candidates[j].weight = weightError
			}
		}
	}

	// Sort by weight desc; tiebreak on later index (in CI, the last error is
	// usually the root cause — earlier errors are often cascading consequences).
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].weight != candidates[j].weight {
			return candidates[i].weight > candidates[j].weight
		}
		return candidates[i].idx > candidates[j].idx
	})

	selected := make(map[int]bool, len(candidates))
	budget := maxBytes
	for _, c := range candidates {
		if c.size > budget {
			continue
		}
		selected[c.idx] = true
		budget -= c.size
	}

	// Re-emit in chronological order, inserting a truncation marker where
	// consecutive selected indices have a gap. This prevents the LLM from
	// inferring causality between non-adjacent log events.
	var out []string
	prev := -1
	kept := 0
	for i := range original {
		if !selected[i] {
			continue
		}
		if prev >= 0 && i > prev+1 {
			out = append(out, truncationMarker)
		}
		out = append(out, original[i])
		prev = i
		kept++
	}
	return strings.Join(out, "\n"), kept
}

const truncationMarker = "... [truncated] ..."

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
