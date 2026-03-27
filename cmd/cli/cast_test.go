package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

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

// TestValidateDemoCast checks that demo.cast matches the expected UX states.
// Run after recording: asciinema rec -i 2 -c "./heisenberg ..." demo.cast
func TestValidateDemoCast(t *testing.T) {
	const castFile = "../../demo-429-quota-exhausted.cast"
	if _, err := os.Stat(castFile); os.IsNotExist(err) {
		t.Skip("demo.cast not found — record one first")
	}

	events, err := parseCast(castFile)
	require.NoError(t, err)
	require.NotEmpty(t, events)

	frames := extractFrames(events)
	require.NotEmpty(t, frames, "no frames extracted from cast")

	// Log all frames for debugging
	for i, f := range frames {
		t.Logf("frame[%d]: %s", i, f)
	}

	// State 1: info message
	assert.Contains(t, frames[0], "Analyzing run")
	assert.Contains(t, frames[0], "for")

	// State 2: at least one progress frame with phase + counter
	hasProgress := false
	progressPattern := regexp.MustCompile(`\w+\s+\d+/\d+`)
	for _, f := range frames[1:] {
		if progressPattern.MatchString(f) {
			hasProgress = true
			break
		}
	}
	assert.True(t, hasProgress, "should have at least one progress frame (Phase  X/Y)")

	// Verify no raw JSON or URLs in output
	for _, f := range frames {
		assert.NotContains(t, f, `"error":`, "raw JSON should not appear in compact mode: %s", f)
		assert.NotContains(t, f, `"code":`, "raw JSON should not appear in compact mode: %s", f)
		assert.NotContains(t, f, "https://", "URLs should not appear in compact mode: %s", f)
	}

	// Error lines should be readable (≤ 120 visible chars)
	for _, f := range frames {
		if strings.HasPrefix(f, "Error:") {
			assert.LessOrEqual(t, len(f), 120, "error line too long for terminal: %s", f)
		}
	}

	// Combine all frames for full-output assertions
	allOutput := strings.Join(frames, "\n")

	// Should end with either success (✓ Used) or error (✗ Stopped at)
	hasSuccess := strings.Contains(allOutput, "✓") && strings.Contains(allOutput, "Used")
	hasError := strings.Contains(allOutput, "✗") && strings.Contains(allOutput, "Stopped at")

	if hasError {
		// State 4: error flow
		assert.Contains(t, allOutput, "Error:")
		assert.Contains(t, allOutput, "Hint:")
		assert.Contains(t, allOutput, "Exit code:")
	} else if hasSuccess {
		// State 3: success flow
		assert.Contains(t, allOutput, "iterations")
	} else {
		t.Errorf("output should contain either ✓ (success) or ✗ (error) close summary\nframes:\n%s", allOutput)
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

	// No duplicate consecutive phase lines (same phase+counter without spinner)
	counterPattern := regexp.MustCompile(`^\s*\w[\w\s]+\s+\d+/\d+$`) // "Phase  N/30" without spinner
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
