package analysis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/ci"
	"github.com/kamilpajak/heisenberg/pkg/cluster"
	gh "github.com/kamilpajak/heisenberg/pkg/github"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	htmlReportName  = "html-report"
	testReportLabel = "TEST REPORT"
	e2eJobName      = "E2E 1/4"
	testRunDate     = "2026-02-08"
)

func TestFormatRunDate_ValidRFC3339(t *testing.T) {
	result := formatRunDate("2026-02-08T18:04:50Z")
	assert.Equal(t, testRunDate, result)
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
	result := formatRunDate(testRunDate)
	assert.Equal(t, testRunDate, result)
}

func TestFindRunToAnalyze_LatestIsSuccess(t *testing.T) {
	runs := []ci.Run{
		{ID: 3, Conclusion: "success"},
		{ID: 2, Conclusion: "failure"},
		{ID: 1, Conclusion: "success"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.True(t, skip, "should skip when latest run is success")
	assert.Equal(t, int64(0), runID)
}

func TestFindRunToAnalyze_LatestIsFailure(t *testing.T) {
	runs := []ci.Run{
		{ID: 3, Conclusion: "failure"},
		{ID: 2, Conclusion: "success"},
		{ID: 1, Conclusion: "failure"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.False(t, skip, "should not skip when latest run is failure")
	assert.Equal(t, int64(3), runID)
}

func TestFindRunToAnalyze_NoRuns(t *testing.T) {
	runs := []ci.Run{}

	runID, skip := findRunToAnalyze(runs)

	assert.False(t, skip)
	assert.Equal(t, int64(0), runID)
}

func TestFindRunToAnalyze_AllSuccess(t *testing.T) {
	runs := []ci.Run{
		{ID: 3, Conclusion: "success"},
		{ID: 2, Conclusion: "success"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.True(t, skip, "should skip when all runs are success")
	assert.Equal(t, int64(0), runID)
}

func TestFindRunToAnalyze_LatestIsCancelled(t *testing.T) {
	runs := []ci.Run{
		{ID: 3, Conclusion: "cancelled"},
		{ID: 2, Conclusion: "failure"},
		{ID: 1, Conclusion: "success"},
	}

	runID, skip := findRunToAnalyze(runs)

	assert.False(t, skip, "should analyze failure even if latest is cancelled")
	assert.Equal(t, int64(2), runID)
}

func TestBuildInitialContext_WithTestArtifacts(t *testing.T) {
	run := &ci.Run{ID: 123, Name: "CI", Conclusion: "failure"}
	jobs := []ci.Job{{Name: "test", ID: 1, Status: "completed", Conclusion: "failure"}}
	artifacts := []ci.Artifact{
		{Name: "playwright-report", SizeBytes: 45000, Expired: false},
		{Name: "build-cache", SizeBytes: 500000, Expired: false},
	}

	ctx := buildInitialContext(run, jobs, artifacts)

	// Should classify test artifacts
	assert.Contains(t, ctx, testReportLabel)
	// Should have prioritized instruction
	assert.Contains(t, ctx, "IMPORTANT")
	assert.Contains(t, ctx, "get_artifact")
	// Build cache should not be labeled as test report
	require.NotContains(t, ctx, "build-cache (500000 bytes) [TEST REPORT]")
}

func TestBuildInitialContext_WithoutTestArtifacts(t *testing.T) {
	run := &ci.Run{ID: 123, Name: "CI", Conclusion: "failure"}
	jobs := []ci.Job{{Name: "build", ID: 1, Status: "completed", Conclusion: "failure"}}
	artifacts := []ci.Artifact{
		{Name: "build-output", SizeBytes: 100000, Expired: false},
	}

	ctx := buildInitialContext(run, jobs, artifacts)

	assert.NotContains(t, ctx, "IMPORTANT")
	assert.NotContains(t, ctx, testReportLabel)
}

func TestBuildInitialContext_NoArtifacts(t *testing.T) {
	run := &ci.Run{ID: 123, Name: "CI", Conclusion: "failure"}
	jobs := []ci.Job{}
	artifacts := []ci.Artifact{}

	ctx := buildInitialContext(run, jobs, artifacts)

	assert.Contains(t, ctx, "No artifacts found")
	assert.NotContains(t, ctx, "IMPORTANT")
}

func TestFilterFailed(t *testing.T) {
	jobs := []ci.Job{
		{ID: 1, Name: "Lint", Conclusion: "success"},
		{ID: 2, Name: "Test 1/4", Conclusion: "failure"},
		{ID: 3, Name: "Test 2/4", Conclusion: "failure"},
		{ID: 4, Name: "Build", Conclusion: "success"},
		{ID: 5, Name: "Test 3/4", Conclusion: "skipped"},
	}

	failed := filterFailed(jobs)

	require.Len(t, failed, 2)
	assert.Equal(t, int64(2), failed[0].ID)
	assert.Equal(t, int64(3), failed[1].ID)
}

func TestFilterFailed_None(t *testing.T) {
	jobs := []ci.Job{{ID: 1, Conclusion: "success"}}
	assert.Empty(t, filterFailed(jobs))
}

func TestFilterFailed_Empty(t *testing.T) {
	assert.Empty(t, filterFailed(nil))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 20))
	assert.Equal(t, "this is a lo...", truncate("this is a long string", 15))
	assert.Equal(t, "", truncate("", 10))
}

func TestBuildClusterContext(t *testing.T) {
	run := &ci.Run{
		ID: 12345, Name: "CI", Branch: "main",
		CommitSHA: "abc123", Conclusion: "failure",
	}
	c := cluster.Cluster{
		ID:        1,
		Signature: cluster.ErrorSignature{RawExcerpt: "net::ERR_CONNECTION_REFUSED"},
		Failures: []cluster.FailureInfo{
			{JobID: 101, JobName: e2eJobName, Conclusion: "failure"},
			{JobID: 102, JobName: "E2E 2/4", Conclusion: "failure"},
		},
		Representative: cluster.FailureInfo{
			JobName: e2eJobName,
			LogTail: "Error: connection refused at localhost:7745",
		},
	}
	artifacts := []ci.Artifact{{Name: htmlReportName, SizeBytes: 5000}}

	ctx := buildClusterContext(run, c, 1, 3, nil, artifacts)

	assert.Contains(t, ctx, "Run ID: 12345")
	assert.Contains(t, ctx, "Cluster 1 of 3")
	assert.Contains(t, ctx, "2 jobs")
	assert.Contains(t, ctx, "ERR_CONNECTION_REFUSED")
	assert.Contains(t, ctx, "specific cluster of related failures")
	assert.Contains(t, ctx, e2eJobName)
	assert.Contains(t, ctx, "E2E 2/4")
	assert.Contains(t, ctx, "connection refused at localhost:7745")
	assert.Contains(t, ctx, testReportLabel)
	assert.Contains(t, ctx, "shared root cause")
}

func TestBuildClusterContext_NoArtifacts(t *testing.T) {
	run := &ci.Run{ID: 1, Conclusion: "failure"}
	c := cluster.Cluster{
		Failures:       []cluster.FailureInfo{{JobID: 1, JobName: "Test"}},
		Representative: cluster.FailureInfo{JobName: "Test", LogTail: "error"},
	}

	ctx := buildClusterContext(run, c, 1, 1, nil, nil)

	assert.Contains(t, ctx, "Cluster 1 of 1")
	assert.NotContains(t, ctx, testReportLabel)
}

func TestBuildUnclusteredCluster(t *testing.T) {
	failures := []cluster.FailureInfo{
		{JobID: 1, JobName: "Short", LogTail: "short"},
		{JobID: 2, JobName: "Long", LogTail: "this is a much longer log tail"},
		{JobID: 3, JobName: "Medium", LogTail: "medium length"},
	}

	c := buildUnclusteredCluster(failures, 5)

	assert.Equal(t, 5, c.ID)
	require.Len(t, c.Failures, 3)
	assert.Equal(t, "Long", c.Representative.JobName, "representative should be longest log")
}

func TestBuildUnclusteredCluster_Single(t *testing.T) {
	failures := []cluster.FailureInfo{
		{JobID: 1, JobName: "Only", LogTail: "log"},
	}

	c := buildUnclusteredCluster(failures, 1)
	assert.Equal(t, "Only", c.Representative.JobName)
}

func TestStampRunMeta(t *testing.T) {
	result := &llm.AnalysisResult{Text: "test"}
	p := Params{RunID: 12345, Owner: "org", Repo: "repo"}
	wfRun := &ci.Run{Branch: "main", CommitSHA: "abc123", Event: "pull_request"}
	stampRunMeta(result, p, wfRun)

	assert.Equal(t, int64(12345), result.RunID)
	assert.Equal(t, "org", result.Owner)
	assert.Equal(t, "repo", result.Repo)
	assert.Equal(t, "main", result.Branch)
	assert.Equal(t, "abc123", result.CommitSHA)
	assert.Equal(t, "pull_request", result.Event)
}

func TestStampRunMeta_NilResult(t *testing.T) {
	// Should not panic
	stampRunMeta(nil, Params{}, &ci.Run{})
}

func TestEmitInfo_NilEmitter(t *testing.T) {
	// Should not panic
	emitInfo(nil, "test message")
}

type recordingEmitter struct {
	events []llm.ProgressEvent
}

func (r *recordingEmitter) Emit(ev llm.ProgressEvent) { r.events = append(r.events, ev) }

func TestEmitInfo_WithEmitter(t *testing.T) {
	e := &recordingEmitter{}
	emitInfo(e, "clustering 6 jobs")

	require.Len(t, e.events, 1)
	assert.Equal(t, "info", e.events[0].Type)
	assert.Equal(t, "clustering 6 jobs", e.events[0].Message)
}

func TestBuildClusterContext_ExpiredArtifactsSkipped(t *testing.T) {
	run := &ci.Run{ID: 1, Conclusion: "failure"}
	c := cluster.Cluster{
		Failures:       []cluster.FailureInfo{{JobID: 1, JobName: "Test"}},
		Representative: cluster.FailureInfo{JobName: "Test", LogTail: "error"},
	}
	artifacts := []ci.Artifact{
		{Name: htmlReportName, SizeBytes: 5000, Expired: true},
		{Name: "blob-report", SizeBytes: 3000, Expired: false},
	}

	ctx := buildClusterContext(run, c, 1, 1, nil, artifacts)

	assert.NotContains(t, ctx, htmlReportName, "expired artifacts should be skipped")
	assert.Contains(t, ctx, "blob-report")
}

func TestIsTestArtifact(t *testing.T) {
	assert.True(t, isTestArtifact(htmlReportName))
	assert.True(t, isTestArtifact("playwright-report"))
	assert.True(t, isTestArtifact("test-results"))
	assert.True(t, isTestArtifact("blob-report-1"))
	assert.False(t, isTestArtifact("build-cache"))
	assert.False(t, isTestArtifact("coverage.out"))
}

func TestRun_RejectsInProgressRunWithNoFailedJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/999":
			fmt.Fprint(w, `{"id":999,"status":"in_progress","conclusion":"","head_branch":"main","head_sha":"abc"}`)
		case "/repos/owner/repo/actions/runs/999/jobs":
			fmt.Fprint(w, `{"jobs":[{"id":1,"name":"build","status":"in_progress","conclusion":""}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	_, err := Run(context.Background(), Params{
		Owner: "owner",
		Repo:  "repo",
		RunID: 999,
		CI:    ghClient,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "still in progress")
}

func TestRun_AllowsInProgressRunWithCompletedFailedJobs(t *testing.T) {
	// Simulates Azure manual approval: test job failed, deploy stage pending
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/999":
			fmt.Fprint(w, `{"id":999,"status":"in_progress","conclusion":"","head_branch":"main","head_sha":"abc"}`)
		case "/repos/owner/repo/actions/runs/999/jobs":
			fmt.Fprint(w, `{"jobs":[
				{"id":1,"name":"test","status":"completed","conclusion":"failure"},
				{"id":2,"name":"deploy","status":"queued","conclusion":""}
			]}`)
		case "/repos/owner/repo/actions/runs/999/artifacts":
			fmt.Fprint(w, `{"artifacts":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	// Run() should NOT return an error — the run has actionable failures
	_, err := Run(context.Background(), Params{
		Owner:        "owner",
		Repo:         "repo",
		RunID:        999,
		CI:           ghClient,
		GoogleAPIKey: "fake", // needed to create LLM client
	})

	// The run should pass validation. It will fail later (no real LLM key),
	// but the error should NOT be about "in progress".
	if err != nil {
		assert.NotContains(t, err.Error(), "in progress")
	}
}

func TestRun_RejectsQueuedRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/999":
			fmt.Fprint(w, `{"id":999,"status":"queued","conclusion":"","head_branch":"main","head_sha":"abc"}`)
		case "/repos/owner/repo/actions/runs/999/jobs":
			fmt.Fprint(w, `{"jobs":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	_, err := Run(context.Background(), Params{
		Owner: "owner",
		Repo:  "repo",
		RunID: 999,
		CI:    ghClient,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet completed")
}

func TestRunIsCompleted(t *testing.T) {
	tests := []struct {
		status    string
		completed bool
	}{
		{"completed", true},
		{"in_progress", false},
		{"queued", false},
		{"", false},
	}
	for _, tt := range tests {
		r := &ci.Run{Status: tt.status}
		assert.Equal(t, tt.completed, r.IsCompleted(), "status=%q", tt.status)
	}
}

func TestValidateRunStatus(t *testing.T) {
	t.Run("completed run is allowed", func(t *testing.T) {
		err := validateRunStatus(&ci.Run{ID: 1, Status: "completed"}, nil)
		assert.NoError(t, err)
	})

	t.Run("empty status is allowed (legacy provider)", func(t *testing.T) {
		err := validateRunStatus(&ci.Run{ID: 1, Status: ""}, nil)
		assert.NoError(t, err)
	})

	t.Run("in_progress with no jobs is rejected", func(t *testing.T) {
		err := validateRunStatus(&ci.Run{ID: 1, Status: "in_progress"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "still in progress")
	})

	t.Run("in_progress with completed failed job is allowed", func(t *testing.T) {
		jobs := []ci.Job{
			{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"},
			{ID: 2, Name: "deploy", Status: "queued", Conclusion: ""},
		}
		err := validateRunStatus(&ci.Run{ID: 1, Status: "in_progress"}, jobs)
		assert.NoError(t, err)
	})

	t.Run("in_progress with only running jobs is rejected", func(t *testing.T) {
		jobs := []ci.Job{
			{ID: 1, Name: "build", Status: "in_progress", Conclusion: ""},
		}
		err := validateRunStatus(&ci.Run{ID: 1, Status: "in_progress"}, jobs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "still in progress")
	})

	t.Run("in_progress with completed success jobs only is rejected", func(t *testing.T) {
		jobs := []ci.Job{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
			{ID: 2, Name: "test", Status: "in_progress", Conclusion: ""},
		}
		err := validateRunStatus(&ci.Run{ID: 1, Status: "in_progress"}, jobs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "still in progress")
	})

	t.Run("queued run is rejected", func(t *testing.T) {
		err := validateRunStatus(&ci.Run{ID: 1, Status: "queued"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet completed")
	})
}

func TestFetchFailureLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return different logs per job
		if r.URL.Path == "/repos/owner/repo/actions/jobs/101/logs" {
			fmt.Fprint(w, "2026-03-29T10:00:00.1234567Z Error: connection refused\n")
			return
		}
		if r.URL.Path == "/repos/owner/repo/actions/jobs/102/logs" {
			fmt.Fprint(w, "2026-03-29T10:00:00.1234567Z FAIL: exit code 1\n")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	p := Params{Owner: "owner", Repo: "repo", CI: ghClient}
	jobs := []ci.Job{
		{ID: 101, Name: "E2E 1/2", Conclusion: "failure"},
		{ID: 102, Name: "E2E 2/2", Conclusion: "failure"},
	}

	failures := fetchFailureLogs(context.Background(), p, jobs)

	require.Len(t, failures, 2)
	assert.Equal(t, "E2E 1/2", failures[0].JobName)
	assert.Equal(t, "E2E 2/2", failures[1].JobName)
	// Signatures extracted
	assert.NotEmpty(t, failures[0].Signature.Category)
	assert.NotEmpty(t, failures[1].Signature.Category)
}

func TestFetchFailureLogs_ErrorFetchingLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	p := Params{Owner: "owner", Repo: "repo", CI: ghClient}
	jobs := []ci.Job{
		{ID: 101, Name: "Test", Conclusion: "failure"},
	}

	failures := fetchFailureLogs(context.Background(), p, jobs)

	require.Len(t, failures, 1)
	assert.Contains(t, failures[0].LogTail, "failed to fetch logs")
}
