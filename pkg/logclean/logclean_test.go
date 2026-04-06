package logclean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtract_EmptyLog(t *testing.T) {
	out, stats := Extract("", 30000)
	assert.Empty(t, out)
	assert.Equal(t, 0, stats.InputLines)
	assert.Equal(t, 0, stats.OutputLines)
	assert.False(t, stats.FallbackUsed)
}

func TestExtract_ShortLog_PassesThrough(t *testing.T) {
	input := "line1\nline2\nline3"
	out, stats := Extract(input, 30000)
	assert.Equal(t, input, out)
	assert.Equal(t, 3, stats.InputLines)
	assert.Equal(t, 3, stats.OutputLines)
	assert.Equal(t, 0, stats.DroppedLines)
	assert.False(t, stats.FallbackUsed)
}

func TestExtract_PostJobCleanup_Removed(t *testing.T) {
	input := strings.Join([]string{
		"2026-04-04T10:00:00.0000000Z Building project...",
		"2026-04-04T10:00:01.0000000Z FAIL: TestLogin",
		"2026-04-04T10:00:02.0000000Z error: assertion failed",
		"2026-04-04T10:00:03.0000000Z Post job cleanup.",
		"2026-04-04T10:00:04.0000000Z [command]/usr/bin/git version",
		"2026-04-04T10:00:05.0000000Z git version 2.53.0",
		"2026-04-04T10:00:06.0000000Z [command]/usr/bin/git config --global --add safe.directory /home/runner",
		"2026-04-04T10:00:07.0000000Z Cleaning up orphan processes",
	}, "\n")

	// Use a small budget so extraction is triggered
	out, stats := Extract(input, 100)

	assert.Contains(t, out, "FAIL: TestLogin")
	assert.Contains(t, out, "error: assertion failed")
	assert.NotContains(t, out, "Post job cleanup")
	assert.NotContains(t, out, "Cleaning up orphan")
	assert.NotContains(t, out, "safe.directory")
	assert.Greater(t, stats.DroppedLines, 0)
}

func TestExtract_ErrorAnnotationPreserved(t *testing.T) {
	input := strings.Join([]string{
		"2026-04-04T10:00:00.0000000Z ##[group]Run actions/checkout@v6",
		"2026-04-04T10:00:01.0000000Z ##[endgroup]",
		"2026-04-04T10:00:02.0000000Z Running tests...",
		"2026-04-04T10:00:03.0000000Z ##[error]Process completed with exit code 1.",
		"2026-04-04T10:00:04.0000000Z ##[warning]Node.js 20 actions are deprecated",
		"2026-04-04T10:00:05.0000000Z Post job cleanup.",
		"2026-04-04T10:00:06.0000000Z Cleaning up orphan processes",
	}, "\n")

	out, _ := Extract(input, 100)

	assert.Contains(t, out, "##[error]Process completed with exit code 1.")
	assert.NotContains(t, out, "##[group]")
	assert.NotContains(t, out, "##[endgroup]")
	assert.NotContains(t, out, "##[warning]")
}

func TestExtract_StackTracePreserved(t *testing.T) {
	// Build input larger than maxBytes so extraction is triggered
	noiseBlock := []string{
		"Current runner version: '2.333.1'",
		"Operating System",
		"GITHUB_TOKEN Permissions",
		"##[group]Run actions/checkout@v6",
		"##[endgroup]",
		"Getting action download info",
		"Download action repository 'actions/checkout@v6' (SHA:abc123)",
		"Download action repository 'actions/setup-go@v5' (SHA:def456)",
	}
	signalBlock := []string{
		"panic: runtime error: index out of range",
		"\t/app/pkg/server/handler.go:42",
		"\t/app/pkg/server/router.go:15",
	}
	cleanupBlock := []string{
		"Post job cleanup.",
		"Cleaning up orphan processes",
	}

	var lines []string
	lines = append(lines, noiseBlock...)
	lines = append(lines, signalBlock...)
	lines = append(lines, cleanupBlock...)
	input := strings.Join(lines, "\n")

	out, _ := Extract(input, 300)

	assert.Contains(t, out, "panic: runtime error")
	assert.Contains(t, out, "handler.go:42")
	assert.Contains(t, out, "router.go:15")
	assert.NotContains(t, out, "Current runner version")
	assert.NotContains(t, out, "Operating System")
}

func TestExtract_FallbackOnOveraggressive(t *testing.T) {
	// All lines match noise patterns — signal would be empty.
	// Input must be >500 bytes to trigger fallback (not short-circuit).
	var noiseLines []string
	for i := 0; i < 30; i++ {
		noiseLines = append(noiseLines, "##[group]Run actions/checkout@v6")
		noiseLines = append(noiseLines, "##[endgroup]")
	}
	input := strings.Join(noiseLines, "\n")
	require.Greater(t, len(input), 500, "input must exceed 500 bytes for fallback test")

	out, stats := Extract(input, 300)

	assert.True(t, stats.FallbackUsed)
	assert.NotEmpty(t, out)
	assert.LessOrEqual(t, len(out), 300)
}

func TestExtract_Stats_Populated(t *testing.T) {
	input := strings.Join([]string{
		"2026-04-04T10:00:00.0000000Z Current runner version: '2.333.1'",
		"2026-04-04T10:00:01.0000000Z Operating System",
		"2026-04-04T10:00:02.0000000Z Running tests...",
		"2026-04-04T10:00:03.0000000Z FAIL: TestLogin",
		"2026-04-04T10:00:04.0000000Z Post job cleanup.",
		"2026-04-04T10:00:05.0000000Z Cleaning up orphan processes",
	}, "\n")

	_, stats := Extract(input, 100)

	assert.Equal(t, 6, stats.InputLines)
	assert.Equal(t, 2, stats.OutputLines) // "Running tests..." + "FAIL: TestLogin"
	assert.Equal(t, 4, stats.DroppedLines)
	assert.False(t, stats.FallbackUsed)
}

func TestExtract_LargeLogTruncation(t *testing.T) {
	// Build a log that's mostly signal and exceeds maxBytes
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, "FAIL: TestSomething very important test output line here")
	}
	input := strings.Join(lines, "\n")
	maxBytes := 5000

	out, stats := Extract(input, maxBytes)

	assert.LessOrEqual(t, len(out), maxBytes)
	assert.False(t, stats.FallbackUsed)
	// Should contain content from the end (errors cluster at end)
	assert.Contains(t, out, "FAIL: TestSomething")
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

func TestExtract_RealWorld_DaveMBush(t *testing.T) {
	input := loadFixture(t, "github_dms_workspace.txt")
	out, stats := Extract(input, 30000)

	// Should remove substantial noise
	assert.Greater(t, stats.DroppedLines, 40, "expected significant noise removal")
	assert.Less(t, stats.OutputLines, stats.InputLines, "output should be smaller than input")

	// Should preserve error content
	assert.Contains(t, out, "exit code")

	// Should not contain post-job cleanup
	assert.NotContains(t, out, "Post job cleanup")
}

func TestExtract_RealWorld_BrokkAi(t *testing.T) {
	input := loadFixture(t, "github_brokk.txt")
	out, stats := Extract(input, 30000)

	// Should remove noise
	assert.Greater(t, stats.DroppedLines, 0, "expected some noise removal")

	// Should preserve Java test failure
	assert.Contains(t, out, "FAILED")

	// Should not contain cleanup
	assert.NotContains(t, out, "Cleaning up orphan processes")
}

func TestExtract_RealWorld_Monaco(t *testing.T) {
	input := loadFixture(t, "github_monaco.txt")
	out, stats := Extract(input, 30000)

	// Should preserve test failure details
	assert.Contains(t, out, "fail")

	// Should remove runner metadata noise
	assert.NotContains(t, out, "Current runner version")
	assert.NotContains(t, out, "GITHUB_TOKEN Permissions")

	// Should have dropped lines
	assert.Greater(t, stats.DroppedLines, 0)
}

func TestExtract_ParameterizedBudget(t *testing.T) {
	// Same input, different budgets should produce different-sized outputs
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "FAIL: TestSomething important test output")
	}
	input := strings.Join(lines, "\n")

	out30k, _ := Extract(input, 30000)
	out80k, _ := Extract(input, 80000)

	// 80KB budget should allow more content (or equal if input fits)
	assert.GreaterOrEqual(t, len(out80k), len(out30k))
}
