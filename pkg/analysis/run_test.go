package analysis

import (
	"testing"

	gh "github.com/kamilpajak/heisenberg/pkg/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatRunDate_ValidRFC3339(t *testing.T) {
	result := formatRunDate("2026-02-08T18:04:50Z")
	assert.Equal(t, "2026-02-08", result)
}

func TestFormatRunDate_WithTimezone(t *testing.T) {
	result := formatRunDate("2026-01-15T10:30:00+01:00")
	assert.Equal(t, "2026-01-15", result)
}

func TestFormatRunDate_Empty(t *testing.T) {
	result := formatRunDate("")
	assert.Equal(t, "unknown date", result)
}

func TestFormatRunDate_InvalidFormat(t *testing.T) {
	result := formatRunDate("not-a-date")
	assert.Equal(t, "not-a-date", result)
}

func TestFormatRunDate_PartialDate(t *testing.T) {
	result := formatRunDate("2026-02-08")
	assert.Equal(t, "2026-02-08", result)
}

func TestFindRunToAnalyze_LatestIsSuccess(t *testing.T) {
	runs := []gh.WorkflowRun{
		{ID: 3, Conclusion: "success"},
		{ID: 2, Conclusion: "failure"},
		{ID: 1, Conclusion: "success"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.True(t, skip, "should skip when latest run is success")
	assert.Equal(t, int64(0), runID)
}

func TestFindRunToAnalyze_LatestIsFailure(t *testing.T) {
	runs := []gh.WorkflowRun{
		{ID: 3, Conclusion: "failure"},
		{ID: 2, Conclusion: "success"},
		{ID: 1, Conclusion: "failure"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.False(t, skip, "should not skip when latest run is failure")
	assert.Equal(t, int64(3), runID)
}

func TestFindRunToAnalyze_NoRuns(t *testing.T) {
	runs := []gh.WorkflowRun{}

	runID, skip := findRunToAnalyze(runs)

	assert.False(t, skip)
	assert.Equal(t, int64(0), runID)
}

func TestFindRunToAnalyze_AllSuccess(t *testing.T) {
	runs := []gh.WorkflowRun{
		{ID: 3, Conclusion: "success"},
		{ID: 2, Conclusion: "success"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.True(t, skip, "should skip when all runs are success")
	assert.Equal(t, int64(0), runID)
}

func TestFindRunToAnalyze_LatestIsCancelled(t *testing.T) {
	runs := []gh.WorkflowRun{
		{ID: 3, Conclusion: "cancelled"},
		{ID: 2, Conclusion: "failure"},
		{ID: 1, Conclusion: "success"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.False(t, skip, "should analyze failure even if latest is cancelled")
	assert.Equal(t, int64(2), runID)
}

func TestBuildInitialContext_WithTestArtifacts(t *testing.T) {
	run := &gh.WorkflowRun{ID: 123, Name: "CI", Conclusion: "failure"}
	jobs := []gh.Job{{Name: "test", ID: 1, Status: "completed", Conclusion: "failure"}}
	artifacts := []gh.Artifact{
		{Name: "playwright-report", SizeBytes: 45000, Expired: false},
		{Name: "build-cache", SizeBytes: 500000, Expired: false},
	}

	ctx := buildInitialContext(run, jobs, artifacts)

	// Should classify test artifacts
	assert.Contains(t, ctx, "TEST REPORT")
	// Should have prioritized instruction
	assert.Contains(t, ctx, "IMPORTANT")
	assert.Contains(t, ctx, "get_artifact")
	// Build cache should not be labeled as test report
	require.NotContains(t, ctx, "build-cache (500000 bytes) [TEST REPORT]")
}

func TestBuildInitialContext_WithoutTestArtifacts(t *testing.T) {
	run := &gh.WorkflowRun{ID: 123, Name: "CI", Conclusion: "failure"}
	jobs := []gh.Job{{Name: "build", ID: 1, Status: "completed", Conclusion: "failure"}}
	artifacts := []gh.Artifact{
		{Name: "build-output", SizeBytes: 100000, Expired: false},
	}

	ctx := buildInitialContext(run, jobs, artifacts)

	assert.NotContains(t, ctx, "IMPORTANT")
	assert.NotContains(t, ctx, "TEST REPORT")
}

func TestBuildInitialContext_NoArtifacts(t *testing.T) {
	run := &gh.WorkflowRun{ID: 123, Name: "CI", Conclusion: "failure"}
	jobs := []gh.Job{}
	artifacts := []gh.Artifact{}

	ctx := buildInitialContext(run, jobs, artifacts)

	assert.Contains(t, ctx, "No artifacts found")
	assert.NotContains(t, ctx, "IMPORTANT")
}
