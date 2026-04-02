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
// frameBuilder accumulates deduplicated terminal frames from cast events.
type frameBuilder struct {
	frames       []string
	currentLine  string
	lastNorm     string
	spinnerChars string
}

func newFrameBuilder() *frameBuilder {
	return &frameBuilder{spinnerChars: "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"}
}

func (fb *frameBuilder) normalize(s string) string {
	for _, c := range fb.spinnerChars {
		s = strings.ReplaceAll(s, string(c), "⠋")
	}
	return s
}

func (fb *frameBuilder) emit(line string) {
	clean := cleanLine(line)
	if clean == "" {
		return
	}
	norm := fb.normalize(clean)
	if norm != fb.lastNorm {
		fb.frames = append(fb.frames, clean)
		fb.lastNorm = norm
	}
}

func (fb *frameBuilder) processChunk(chunk string, isNewline bool) {
	if isNewline {
		fb.emit(fb.currentLine)
		fb.currentLine = ""
		fb.lastNorm = ""
	}

	for _, cr := range strings.Split(chunk, "\r") {
		cr = strings.ReplaceAll(cr, "\x1b[K", "")
		if cr == "" {
			fb.currentLine = ""
		} else {
			fb.currentLine = cr
		}
	}

	fb.emit(fb.currentLine)
}

func extractFrames(events []castEvent) []string {
	fb := newFrameBuilder()

	for _, ev := range events {
		chunks := strings.Split(ev.Data, "\r\n")
		for i, chunk := range chunks {
			fb.processChunk(chunk, i > 0)
		}
	}

	return fb.frames
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

func assertNoURLs(t *testing.T, frames []string) {
	t.Helper()
	for _, f := range frames {
		assert.NotContains(t, f, "https://", "URLs should not appear in compact mode: %s", f)
	}
}

func assertCloseSummary(t *testing.T, allOutput string) {
	t.Helper()
	hasSuccess := strings.Contains(allOutput, "✓") && strings.Contains(allOutput, "Used")
	hasError := strings.Contains(allOutput, "✗") && strings.Contains(allOutput, "Stopped at")

	switch {
	case hasError:
		assert.Contains(t, allOutput, "Error:")
		assert.Contains(t, allOutput, "Hint:")
		assert.Contains(t, allOutput, "Exit code:")
	case hasSuccess:
		assert.Contains(t, allOutput, "iterations")
	default:
		t.Errorf("compact cast should contain ✓ Used or ✗ Stopped at\nframes:\n%s", allOutput)
	}
}

func assertCounterConsistency(t *testing.T, frames []string) {
	t.Helper()
	closePattern := regexp.MustCompile(`(?:Used|Stopped at) (\d+)/(\d+)`)
	spinnerPattern := regexp.MustCompile(`(\d+)/\d+\s+\(calling model`)
	var closeStep string
	for _, f := range frames {
		if m := closePattern.FindStringSubmatch(f); m != nil {
			closeStep = m[1]
		}
	}
	if closeStep == "" {
		return
	}
	for _, f := range frames {
		if m := spinnerPattern.FindStringSubmatch(f); m != nil {
			assert.LessOrEqual(t, m[1], closeStep,
				"spinner counter %s should not exceed close counter %s in frame: %s", m[1], closeStep, f)
		}
	}
}

func assertNoDuplicateStaticLines(t *testing.T, frames []string) {
	t.Helper()
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

	assertNoURLs(t, frames)
	assertCloseSummary(t, allOutput)
	assertCounterConsistency(t, frames)
	assertNoDuplicateStaticLines(t, frames)
}

func assertHasToolLines(t *testing.T, frames []string) {
	t.Helper()
	toolPattern := regexp.MustCompile(`✓ \w+`)
	for _, f := range frames {
		if toolPattern.MatchString(f) {
			return
		}
	}
	t.Error("verbose cast should have ✓ tool_name lines")
}

func assertCounterAlignment(t *testing.T, frames []string) {
	t.Helper()
	counterRe := regexp.MustCompile(`(\d+/\d+)\s*$`)
	var positions []int
	for _, f := range frames {
		m := counterRe.FindStringIndex(f)
		if m == nil || !strings.Contains(f, "✓") {
			continue
		}
		col := utf8.RuneCountInString(f[:m[0]])
		positions = append(positions, col)
	}
	if len(positions) <= 1 {
		return
	}
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

	assertHasToolLines(t, frames)
	assert.Contains(t, allOutput, "↳", "verbose cast should have ↳ stats lines")
	assert.Contains(t, allOutput, "✓ done", "verbose cast should end with ✓ done")
	assertCounterAlignment(t, frames)
}
