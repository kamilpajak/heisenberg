package logclean

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyWeight_Critical(t *testing.T) {
	critical := []string{
		"FAIL: TestLogin (0.5s)",
		"Exception: connection refused",
		"panic: runtime error: index out of range",
		`Traceback (most recent call last):`,
		"##[error]Process completed with exit code 1.",
	}
	for _, line := range critical {
		assert.Equal(t, weightCritical, classifyWeight(line), "expected critical weight: %q", line)
	}
}

func TestClassifyWeight_Error(t *testing.T) {
	errorLines := []string{
		"ERROR: build failed",
		"FATAL: could not open config",
		"\t/app/pkg/server/handler.go:42",
		`File "/app/tests/test_auth.py", line 42`,
		"at com.example.AppTest.testLogin(AppTest.java:42)",
	}
	for _, line := range errorLines {
		assert.Equal(t, weightError, classifyWeight(line), "expected error weight: %q", line)
	}
}

func TestClassifyWeight_Warning(t *testing.T) {
	warnings := []string{
		"request timed out after 30s",
		"deadlock detected on resource pool",
		"container OOMKilled",
		"dial tcp: connection refused",
	}
	for _, line := range warnings {
		assert.Equal(t, weightWarning, classifyWeight(line), "expected warning weight: %q", line)
	}
}

func TestClassifyWeight_NoiseFilteredByCaller(t *testing.T) {
	// classifyWeight's contract: caller filters noise via classifyLine first.
	// This test documents that contract — noise lines are dropped upstream.
	noise := []string{
		"Current runner version: '2.333.1'",
		"Post job cleanup.",
		"##[group]Run actions/checkout@v6",
	}
	for _, line := range noise {
		assert.Equal(t, lineNoise, classifyLine(line), "caller must filter: %q", line)
	}
}

func TestClassifyWeight_DefaultSignal(t *testing.T) {
	generic := []string{
		"Building project...",
		"Compiling src/main.ts",
		"npm warn deprecated some-package@1.0.0",
	}
	for _, line := range generic {
		assert.Equal(t, weightDefault, classifyWeight(line), "expected default weight: %q", line)
	}
}

func TestExtract_WeightedSelection_PrefersCritical(t *testing.T) {
	t.Setenv("HEISENBERG_LOG_WEIGHTED", "1")

	// High-weight critical line buried in weight-1 filler must survive under
	// budget pressure even when it's earlier than the filler tail.
	var lines []string
	lines = append(lines, "2026-04-04T10:00:00.0000000Z panic: runtime error: index out of range")
	for i := 0; i < 100; i++ {
		lines = append(lines, "2026-04-04T10:00:02.0000000Z Building project filler line "+strings.Repeat("x", 10))
	}
	input := strings.Join(lines, "\n")

	// Budget large enough to fit ~2-3 lines.
	out, _ := Extract(input, 200)

	assert.Contains(t, out, "panic: runtime error", "critical weight line must survive selection pressure")
}

func TestExtract_WeightedSelection_PreservesChronologicalOrder(t *testing.T) {
	t.Setenv("HEISENBERG_LOG_WEIGHTED", "1")

	// Prepend filler so input exceeds budget; keep high-weight markers
	// near the end so adjacency boost doesn't elevate unrelated fillers
	// into the selection set.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "2026-04-04T10:00:00.0000000Z Building project...")
	}
	lines = append(lines,
		"2026-04-04T10:00:01.0000000Z panic: first panic",
		"2026-04-04T10:00:02.0000000Z ERROR: middle error",
		"2026-04-04T10:00:03.0000000Z FAIL: last test",
	)
	input := strings.Join(lines, "\n")

	out, _ := Extract(input, 300)

	iPanic := strings.Index(out, "first panic")
	iError := strings.Index(out, "middle error")
	iFail := strings.Index(out, "last test")
	assert.Greater(t, iPanic, -1)
	assert.Greater(t, iError, -1)
	assert.Greater(t, iFail, -1)
	assert.Less(t, iPanic, iError, "panic should appear before error")
	assert.Less(t, iError, iFail, "error should appear before fail")
}

func TestExtract_WeightedSelection_AdjacencyBoost(t *testing.T) {
	t.Setenv("HEISENBERG_LOG_WEIGHTED", "1")

	// A panic followed by stack-frame-like lines must bring its 3 neighbors
	// along, even when unrelated high-weight lines exist elsewhere.
	fat := strings.Repeat("x", 60)
	var lines []string
	// Competing unrelated ERROR lines early in the log.
	for i := 0; i < 10; i++ {
		lines = append(lines, "2026-04-04T10:00:00.0000000Z ERROR: unrelated "+fat)
	}
	// The panic we care about.
	lines = append(lines, "2026-04-04T10:00:01.0000000Z panic: real root cause")
	lines = append(lines, "2026-04-04T10:00:02.0000000Z goroutine 1 [running]")
	lines = append(lines, "2026-04-04T10:00:03.0000000Z main.foo() returned bad value")
	lines = append(lines, "2026-04-04T10:00:04.0000000Z context: more detail about failure")
	// Filler tail.
	for i := 0; i < 10; i++ {
		lines = append(lines, "2026-04-04T10:00:05.0000000Z Running tests "+fat)
	}
	input := strings.Join(lines, "\n")

	// Budget fits roughly the panic plus its 3 neighbors, but leaves room to
	// compete with one of the unrelated ERRORs.
	out, _ := Extract(input, 400)

	assert.Contains(t, out, "panic: real root cause")
	assert.Contains(t, out, "goroutine 1 [running]", "line immediately after panic must be boosted")
	assert.Contains(t, out, "main.foo() returned bad value", "2nd line after panic must be boosted")
	assert.Contains(t, out, "context: more detail", "3rd line after panic must be boosted")
}

func TestExtract_WeightedSelection_InsertsTruncationMarker(t *testing.T) {
	t.Setenv("HEISENBERG_LOG_WEIGHTED", "1")

	var lines []string
	lines = append(lines, "2026-04-04T10:00:00.0000000Z panic: first panic")
	for i := 0; i < 100; i++ {
		lines = append(lines, "2026-04-04T10:00:01.0000000Z Building project filler "+strings.Repeat("x", 20))
	}
	lines = append(lines, "2026-04-04T10:00:02.0000000Z panic: second panic")
	input := strings.Join(lines, "\n")

	// Budget fits both panic lines but drops most filler between them.
	out, _ := Extract(input, 200)

	assert.Contains(t, out, "first panic")
	assert.Contains(t, out, "second panic")
	assert.Contains(t, out, "[truncated]", "gaps between non-adjacent selected lines must be marked")
}

func TestExtract_WeightedSelection_NoMarkerWhenContiguous(t *testing.T) {
	t.Setenv("HEISENBERG_LOG_WEIGHTED", "1")

	// All selected lines are adjacent — no marker needed. Use large filler
	// so only the 3 contiguous panics fit in budget.
	fat := strings.Repeat("x", 200)
	var lines []string
	lines = append(lines, "2026-04-04T10:00:00.0000000Z panic: A")
	lines = append(lines, "2026-04-04T10:00:01.0000000Z panic: B")
	lines = append(lines, "2026-04-04T10:00:02.0000000Z panic: C")
	for i := 0; i < 20; i++ {
		lines = append(lines, "2026-04-04T10:00:03.0000000Z Running tests "+fat)
	}
	input := strings.Join(lines, "\n")

	out, _ := Extract(input, 150)

	assert.Contains(t, out, "panic: A")
	assert.Contains(t, out, "panic: B")
	assert.Contains(t, out, "panic: C")
	assert.NotContains(t, out, "[truncated]", "no marker when selected lines are contiguous")
}

func TestExtract_WeightedSelection_TiebreakFavorsLaterLines(t *testing.T) {
	t.Setenv("HEISENBERG_LOG_WEIGHTED", "1")

	// Two equally-weighted errors. Under budget pressure (fits one), the later
	// one wins — in CI pipelines the last error is usually the root cause.
	var lines []string
	lines = append(lines, "2026-04-04T10:00:00.0000000Z panic: EARLY panic")
	for i := 0; i < 50; i++ {
		lines = append(lines, "2026-04-04T10:00:01.0000000Z Building project...")
	}
	lines = append(lines, "2026-04-04T10:00:02.0000000Z panic: LATE panic")
	for i := 0; i < 50; i++ {
		lines = append(lines, "2026-04-04T10:00:03.0000000Z more building output filler")
	}
	input := strings.Join(lines, "\n")

	// Budget tight enough to fit roughly one panic line + minimal filler.
	out, _ := Extract(input, 80)

	assert.Contains(t, out, "LATE panic", "later-index critical line should win tiebreak")
	assert.NotContains(t, out, "EARLY panic", "earlier critical line should be dropped when budget forces choice")
}

func TestExtract_WeightedSelection_FlagOff_UnchangedBehavior(t *testing.T) {
	// Flag unset — behavior must match existing Extract.
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "FAIL: TestSomething")
	}
	input := strings.Join(lines, "\n")

	out, stats := Extract(input, 500)

	// Tail truncation: output size ≤ budget, no fallback.
	assert.LessOrEqual(t, len(out), 500)
	assert.False(t, stats.FallbackUsed)
	assert.Contains(t, out, "FAIL: TestSomething")
}
