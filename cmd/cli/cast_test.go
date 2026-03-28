package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// castEvent represents a single asciinema v3 event: [delay, type, data].
type castEvent struct {
	Delay float64
	Type  string
	Data  string
}

// parseCast reads an asciinema v3 file and returns the output events.
func parseCast(path string) ([]castEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, nil
	}

	var events []castEvent
	for _, line := range lines[1:] { // skip header
		var raw []json.RawMessage
		if json.Unmarshal([]byte(line), &raw) != nil || len(raw) < 3 {
			continue
		}
		var delay float64
		var typ, dat string
		if json.Unmarshal(raw[0], &delay) != nil || json.Unmarshal(raw[1], &typ) != nil || json.Unmarshal(raw[2], &dat) != nil {
			continue
		}
		if typ == "o" {
			events = append(events, castEvent{Delay: delay, Type: typ, Data: dat})
		}
	}
	return events, nil
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?25[hl]`)

// extractFrames replays cast events on a simple terminal simulator and
// returns deduplicated key frames in chronological order. Spinner character
// changes are collapsed so each distinct "content state" appears only once.
func extractFrames(events []castEvent) []string {
	spinnerChars := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

	normalize := func(s string) string {
		for _, c := range spinnerChars {
			s = strings.ReplaceAll(s, string(c), "⠋")
		}
		return s
	}

	var frames []string
	var currentLine string
	lastNorm := ""

	emit := func(line string) {
		clean := cleanLine(line)
		if clean == "" {
			return
		}
		norm := normalize(clean)
		if norm != lastNorm {
			frames = append(frames, clean)
			lastNorm = norm
		}
	}

	for _, ev := range events {
		chunks := strings.Split(ev.Data, "\r\n")
		for i, chunk := range chunks {
			if i > 0 {
				// \r\n commits currentLine as a frame
				emit(currentLine)
				currentLine = ""
				lastNorm = "" // reset after newline so next content is captured
			}

			crParts := strings.Split(chunk, "\r")
			for _, cr := range crParts {
				cr = strings.ReplaceAll(cr, "\x1b[K", "")
				if cr == "" {
					currentLine = ""
				} else {
					currentLine = cr
				}
			}

			// Snapshot the current overwrite line if content changed
			emit(currentLine)
		}
	}

	return frames
}

// cleanLine strips ANSI escape codes and trims whitespace.
func cleanLine(s string) string {
	return strings.TrimSpace(ansiPattern.ReplaceAllString(s, ""))
}

// TestValidateDemoCast checks that all .cast files match the expected UX states.
// Run after recording: asciinema rec -i 2 -c "./heisenberg ..." demo.cast
func TestValidateDemoCast(t *testing.T) {
	// CAST_FILE env var overrides glob — used by scripts/validate-cast.sh
	if single := os.Getenv("CAST_FILE"); single != "" {
		t.Run(filepath.Base(single), func(t *testing.T) {
			validateCast(t, single)
		})
		return
	}

	castFiles, _ := filepath.Glob("../../*.cast")
	if len(castFiles) == 0 {
		t.Skip("no .cast files found — record one first")
	}

	for _, castFile := range castFiles {
		t.Run(filepath.Base(castFile), func(t *testing.T) {
			validateCast(t, castFile)
		})
	}
}

func validateCast(t *testing.T, castFile string) {
	t.Helper()

	if strings.Contains(castFile, "verbose") {
		validateVerboseCast(t, castFile)
	} else {
		validateCompactCast(t, castFile)
	}
}

// commonCastChecks runs assertions shared by both compact and verbose casts.
func commonCastChecks(t *testing.T, frames []string) {
	t.Helper()

	// State 1: info message
	assert.Contains(t, frames[0], "Analyzing run")
	assert.Contains(t, frames[0], "for")

	// At least one progress frame with counter
	hasProgress := false
	progressPattern := regexp.MustCompile(`\d+/\d+`)
	for _, f := range frames[1:] {
		if progressPattern.MatchString(f) {
			hasProgress = true
			break
		}
	}
	assert.True(t, hasProgress, "should have at least one progress frame with counter")

	// No raw JSON
	for _, f := range frames {
		assert.NotContains(t, f, `"error":`, "raw JSON should not appear: %s", f)
		assert.NotContains(t, f, `"code":`, "raw JSON should not appear: %s", f)
	}

	// Error lines should be readable
	for _, f := range frames {
		if strings.HasPrefix(f, "Error:") {
			assert.LessOrEqual(t, len(f), 120, "error line too long: %s", f)
		}
	}

	// RCA should be present on success
	allOutput := strings.Join(frames, "\n")
	if strings.Contains(allOutput, "✓") {
		assert.Contains(t, allOutput, "Root Cause")
	}
}

func validateCompactCast(t *testing.T, castFile string) {
	t.Helper()

	events, err := parseCast(castFile)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	frames := extractFrames(events)
	require.NotEmpty(t, frames, "no frames extracted from cast")

	for i, f := range frames {
		t.Logf("frame[%d]: %s", i, f)
	}

	commonCastChecks(t, frames)

	allOutput := strings.Join(frames, "\n")

	// Compact: no URLs
	for _, f := range frames {
		assert.NotContains(t, f, "https://", "URLs should not appear in compact mode: %s", f)
	}

	// Compact: close summary with ✓ or ✗
	hasSuccess := strings.Contains(allOutput, "✓") && strings.Contains(allOutput, "Used")
	hasError := strings.Contains(allOutput, "✗") && strings.Contains(allOutput, "Stopped at")

	if hasError {
		assert.Contains(t, allOutput, "Error:")
		assert.Contains(t, allOutput, "Hint:")
		assert.Contains(t, allOutput, "Exit code:")
	} else if hasSuccess {
		assert.Contains(t, allOutput, "iterations")
	} else {
		t.Errorf("compact cast should contain ✓ Used or ✗ Stopped at\nframes:\n%s", allOutput)
	}

	// Counter consistency: spinner counter should match Close counter
	closePattern := regexp.MustCompile(`(?:Used|Stopped at) (\d+)/(\d+)`)
	spinnerPattern := regexp.MustCompile(`(\d+)/\d+\s+\(calling model`)
	var closeStep string
	for _, f := range frames {
		if m := closePattern.FindStringSubmatch(f); m != nil {
			closeStep = m[1]
		}
	}
	if closeStep != "" {
		for _, f := range frames {
			if m := spinnerPattern.FindStringSubmatch(f); m != nil {
				assert.LessOrEqual(t, m[1], closeStep,
					"spinner counter %s should not exceed close counter %s in frame: %s", m[1], closeStep, f)
			}
		}
	}

	// No duplicate consecutive static progress lines
	counterPattern := regexp.MustCompile(`^\s*\w[\w\s]+\s+\d+/\d+$`)
	var prevStatic string
	for _, f := range frames {
		if counterPattern.MatchString(f) {
			if f == prevStatic {
				t.Errorf("duplicate static progress line: %s", f)
			}
			prevStatic = f
		}
	}
}

func validateVerboseCast(t *testing.T, castFile string) {
	t.Helper()

	events, err := parseCast(castFile)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	frames := extractFrames(events)
	require.NotEmpty(t, frames, "no frames extracted from cast")

	for i, f := range frames {
		t.Logf("frame[%d]: %s", i, f)
	}

	commonCastChecks(t, frames)

	allOutput := strings.Join(frames, "\n")

	// Verbose: ✓ per tool call with tool name
	toolPattern := regexp.MustCompile(`✓ \w+`)
	hasToolLines := false
	for _, f := range frames {
		if toolPattern.MatchString(f) {
			hasToolLines = true
			break
		}
	}
	assert.True(t, hasToolLines, "verbose cast should have ✓ tool_name lines")

	// Verbose: ↳ stats lines
	assert.Contains(t, allOutput, "↳", "verbose cast should have ↳ stats lines")

	// Verbose: done tool signals completion
	assert.Contains(t, allOutput, "✓ done", "verbose cast should end with ✓ done")

	// Verbose: counter alignment — all counters at similar display column
	counterRe := regexp.MustCompile(`(\d+/\d+)\s*$`)
	var positions []int
	for _, f := range frames {
		if m := counterRe.FindStringIndex(f); m != nil && strings.Contains(f, "✓") {
			// Convert byte offset to rune (column) position
			col := utf8.RuneCountInString(f[:m[0]])
			positions = append(positions, col)
		}
	}
	if len(positions) > 1 {
		minPos, maxPos := positions[0], positions[0]
		for _, p := range positions[1:] {
			if p < minPos {
				minPos = p
			}
			if p > maxPos {
				maxPos = p
			}
		}
		assert.Equal(t, 0, maxPos-minPos, "verbose counters must be exactly aligned (spread: %d)", maxPos-minPos)
	}
}
