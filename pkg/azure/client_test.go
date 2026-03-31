package azure

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/ci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface checks
var _ ci.Provider = (*Client)(nil)
var _ ci.TestResultsProvider = (*Client)(nil)

func TestName(t *testing.T) {
	c := NewTestClient("myorg", "myproject", "http://localhost", http.DefaultClient)
	assert.Equal(t, "azure", c.Name())
}

func TestAnalysisHints(t *testing.T) {
	c := NewTestClient("myorg", "myproject", "http://localhost", http.DefaultClient)
	hints := c.AnalysisHints()
	assert.Contains(t, hints, "get_test_results")
	assert.Contains(t, hints, "Azure")
}

func TestAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// PAT "test-token" → base64(":test-token") = "OnRlc3QtdG9rZW4="
		assert.Equal(t, "Basic OnRlc3QtdG9rZW4=", r.Header.Get("Authorization"))
		w.Write([]byte(`{"count":0,"value":[]}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	_, _ = c.ListRuns(context.Background(), ci.RunFilter{})
}

func TestListRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/_apis/build/builds")
		assert.Equal(t, "7.1", r.URL.Query().Get("api-version"))
		w.Write([]byte(`{
			"count": 2,
			"value": [
				{
					"id": 100,
					"buildNumber": "20260331.1",
					"status": "completed",
					"result": "failed",
					"sourceBranch": "refs/heads/main",
					"sourceVersion": "abc123",
					"reason": "individualCI",
					"queueTime": "2026-03-31T10:00:00Z",
					"definition": {"name": "CI", "path": "\\pipelines\\ci.yml"}
				},
				{
					"id": 99,
					"buildNumber": "20260330.2",
					"status": "completed",
					"result": "succeeded",
					"sourceBranch": "refs/heads/feature/test",
					"sourceVersion": "def456",
					"reason": "pullRequest",
					"queueTime": "2026-03-30T09:00:00Z",
					"definition": {"name": "CI", "path": "\\pipelines\\ci.yml"}
				}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	runs, err := c.ListRuns(context.Background(), ci.RunFilter{Status: "completed", PerPage: 10})
	require.NoError(t, err)
	assert.Len(t, runs, 2)

	assert.Equal(t, int64(100), runs[0].ID)
	assert.Equal(t, "failure", runs[0].Conclusion)
	assert.Equal(t, "main", runs[0].Branch)
	assert.Equal(t, "abc123", runs[0].CommitSHA)
	assert.Equal(t, "individualCI", runs[0].Event)

	assert.Equal(t, int64(99), runs[1].ID)
	assert.Equal(t, "success", runs[1].Conclusion)
	assert.Equal(t, "feature/test", runs[1].Branch)
}

func TestListRuns_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	_, err := c.ListRuns(context.Background(), ci.RunFilter{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestGetRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/builds/123")
		w.Write([]byte(`{
			"id": 123,
			"buildNumber": "20260331.1",
			"status": "completed",
			"result": "failed",
			"sourceBranch": "refs/heads/main",
			"sourceVersion": "abc123",
			"reason": "individualCI",
			"queueTime": "2026-03-31T10:00:00Z",
			"definition": {"name": "CI", "path": "\\pipelines\\ci.yml"}
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	run, err := c.GetRun(context.Background(), 123)
	require.NoError(t, err)
	assert.Equal(t, int64(123), run.ID)
	assert.Equal(t, "failure", run.Conclusion)
	assert.Equal(t, "main", run.Branch)
}

func TestGetRun_PR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id": 456,
			"buildNumber": "20260331.2",
			"status": "completed",
			"result": "failed",
			"sourceBranch": "refs/pull/42/merge",
			"sourceVersion": "def456",
			"reason": "pullRequest",
			"queueTime": "2026-03-31T11:00:00Z",
			"definition": {"name": "PR", "path": "\\pipelines\\pr.yml"}
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	run, err := c.GetRun(context.Background(), 456)
	require.NoError(t, err)
	assert.Equal(t, []int{42}, run.PRNumbers)
	assert.Equal(t, "pullRequest", run.Event)
}

func TestListJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/builds/100/timeline")
		w.Write([]byte(`{
			"records": [
				{"id": "stage-1", "parentId": "", "type": "Stage", "name": "Build", "state": "completed", "result": "succeeded"},
				{"id": "job-1", "parentId": "stage-1", "type": "Job", "name": "compile", "state": "completed", "result": "succeeded", "log": {"id": 5}},
				{"id": "job-2", "parentId": "stage-1", "type": "Job", "name": "test", "state": "completed", "result": "failed", "log": {"id": 6}},
				{"id": "task-1", "parentId": "job-1", "type": "Task", "name": "npm install", "state": "completed", "result": "succeeded", "log": {"id": 7}},
				{"id": "task-2", "parentId": "job-1", "type": "Task", "name": "npm build", "state": "completed", "result": "succeeded", "log": {"id": 8}},
				{"id": "task-3", "parentId": "job-2", "type": "Task", "name": "npm test", "state": "completed", "result": "failed", "log": {"id": 9}}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	jobs, err := c.ListJobs(context.Background(), 100)
	require.NoError(t, err)
	assert.Len(t, jobs, 2)

	assert.Equal(t, "compile", jobs[0].Name)
	assert.Equal(t, "completed", jobs[0].Status)
	assert.Equal(t, "success", jobs[0].Conclusion)

	assert.Equal(t, "test", jobs[1].Name)
	assert.Equal(t, "failure", jobs[1].Conclusion)

	// Verify bit-shift encoding: Job.ID = (buildID << 32) | logID
	expectedID0 := (int64(100) << 32) | int64(5)
	assert.Equal(t, expectedID0, jobs[0].ID)
	expectedID1 := (int64(100) << 32) | int64(6)
	assert.Equal(t, expectedID1, jobs[1].ID)
}

func TestListJobs_NilLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"records": [
				{"id": "job-1", "parentId": "", "type": "Job", "name": "skipped-job", "state": "completed", "result": "skipped", "log": null}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	jobs, err := c.ListJobs(context.Background(), 200)
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	// logID=0 encoded: (200 << 32) | 0
	assert.Equal(t, int64(200)<<32, jobs[0].ID)
}

func TestGetJobLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect /builds/100/logs/6
		assert.Contains(t, r.URL.Path, "/builds/100/logs/6")
		w.Write([]byte("2026-03-31T10:00:00Z Error: test failed\nAssertionError: expected 1 to equal 2"))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	// Encode jobID: (buildID=100 << 32) | logID=6
	jobID := (int64(100) << 32) | int64(6)
	logs, err := c.GetJobLogs(context.Background(), jobID)
	require.NoError(t, err)
	assert.Contains(t, logs, "AssertionError")
}

func TestGetJobLogs_NoLog(t *testing.T) {
	c := NewTestClient("myorg", "myproject", "http://localhost", http.DefaultClient)
	// logID=0 → no logs
	jobID := int64(200) << 32
	_, err := c.GetJobLogs(context.Background(), jobID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no logs available")
}

func TestListArtifacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/builds/100/artifacts")
		w.Write([]byte(`{
			"count": 2,
			"value": [
				{"id": 1, "name": "test-report", "resource": {"downloadUrl": "https://dev.azure.com/dl/1"}},
				{"id": 2, "name": "build-logs", "resource": {"downloadUrl": "https://dev.azure.com/dl/2"}}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	artifacts, err := c.ListArtifacts(context.Background(), 100)
	require.NoError(t, err)
	assert.Len(t, artifacts, 2)
	assert.Equal(t, int64(1), artifacts[0].ID)
	assert.Equal(t, "test-report", artifacts[0].Name)
	assert.False(t, artifacts[0].Expired)
}

func TestDownloadArtifact(t *testing.T) {
	zipContent := []byte("fake-zip-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/myproject/_apis/build/builds/100/artifacts" {
			w.Write([]byte(`{
				"count": 1,
				"value": [{"id": 1, "name": "report", "resource": {"downloadUrl": "` + "http://" + r.Host + `/download/1"}}]
			}`))
			return
		}
		if r.URL.Path == "/download/1" {
			w.Write(zipContent)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	// First populate cache
	_, err := c.ListArtifacts(context.Background(), 100)
	require.NoError(t, err)

	data, err := c.DownloadArtifact(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, zipContent, data)
}

func TestDownloadArtifact_NotCached(t *testing.T) {
	c := NewTestClient("myorg", "myproject", "http://localhost", http.DefaultClient)
	_, err := c.DownloadArtifact(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetRepoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/_apis/git/repositories/myproject/items")
		assert.Equal(t, "src/main.ts", r.URL.Query().Get("path"))
		w.Write([]byte(`export function main() { console.log("hello"); }`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	content, err := c.GetRepoFile(context.Background(), "src/main.ts")
	require.NoError(t, err)
	assert.Contains(t, content, "main()")
}

func TestGetRepoFile_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	_, err := c.GetRepoFile(context.Background(), "nonexistent.ts")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestListDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "src", r.URL.Query().Get("scopePath"))
		assert.Equal(t, "OneLevel", r.URL.Query().Get("recursionLevel"))
		w.Write([]byte(`{
			"count": 3,
			"value": [
				{"path": "/src", "isFolder": true},
				{"path": "/src/components", "isFolder": true},
				{"path": "/src/main.ts", "isFolder": false},
				{"path": "/src/utils.ts", "isFolder": false}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	entries, err := c.ListDirectory(context.Background(), "src")
	require.NoError(t, err)
	// Should skip the root entry ("/src") and return children
	assert.Contains(t, entries, "components/")
	assert.Contains(t, entries, "main.ts")
	assert.Contains(t, entries, "utils.ts")
}

func TestGetChangedFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/diffs/commits")
		assert.Equal(t, "main", r.URL.Query().Get("baseVersion"))
		assert.Equal(t, "abc123", r.URL.Query().Get("targetVersion"))
		w.Write([]byte(`{
			"changes": [
				{"item": {"path": "/src/app.ts"}, "changeType": "edit"},
				{"item": {"path": "/tests/app.test.ts"}, "changeType": "add"},
				{"item": {"path": "/old.ts"}, "changeType": "delete"}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	files, err := c.GetChangedFiles(context.Background(), ci.ChangeRef{HeadSHA: "abc123", BaseBranch: "main"})
	require.NoError(t, err)
	assert.Len(t, files, 3)
	assert.Equal(t, "src/app.ts", files[0].Path)
	assert.Equal(t, "modified", files[0].Status)
	assert.Equal(t, "tests/app.test.ts", files[1].Path)
	assert.Equal(t, "added", files[1].Status)
	assert.Equal(t, "old.ts", files[2].Path)
	assert.Equal(t, "removed", files[2].Status)
}

func TestGetTestRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/_apis/test/runs")
		assert.Equal(t, "vstfs:///Build/Build/100", r.URL.Query().Get("buildUri"))
		w.Write([]byte(`{
			"count": 1,
			"value": [
				{
					"id": 501,
					"name": "Unit Tests",
					"totalTests": 120,
					"passedTests": 115,
					"unanalyzedTests": 5
				}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	runs, err := c.GetTestRuns(context.Background(), 100)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, int64(501), runs[0].ID)
	assert.Equal(t, "Unit Tests", runs[0].Name)
	assert.Equal(t, 120, runs[0].TotalTests)
	assert.Equal(t, 115, runs[0].PassedTests)
	assert.Equal(t, 5, runs[0].FailedTests)
}

func TestGetTestResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/_apis/test/runs/501/results")
		assert.Equal(t, "Failed", r.URL.Query().Get("outcomes"))
		w.Write([]byte(`{
			"count": 2,
			"value": [
				{
					"id": 1001,
					"testCaseTitle": "should calculate total price",
					"outcome": "Failed",
					"errorMessage": "Expected 100 but got 0",
					"stackTrace": "at calculateTotal (src/pricing.ts:42)\nat Object.<anonymous> (tests/pricing.test.ts:15)",
					"durationInMs": 245.5
				},
				{
					"id": 1002,
					"testCaseTitle": "should handle empty cart",
					"outcome": "Failed",
					"errorMessage": "TypeError: Cannot read property 'length' of undefined",
					"stackTrace": "at getCartItems (src/cart.ts:10)",
					"durationInMs": 12.3
				}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	results, err := c.GetTestResults(context.Background(), 501)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	assert.Equal(t, int64(1001), results[0].ID)
	assert.Equal(t, "should calculate total price", results[0].TestName)
	assert.Equal(t, "Failed", results[0].Outcome)
	assert.Equal(t, "Expected 100 but got 0", results[0].ErrorMessage)
	assert.Contains(t, results[0].StackTrace, "pricing.ts:42")
	assert.Equal(t, 245.5, results[0].DurationMs)
}

func TestGetTestResults_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count": 0, "value": []}`))
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	results, err := c.GetTestResults(context.Background(), 501)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestDownloadAndExtract(t *testing.T) {
	zipData := buildTestZip(t, "report.html", []byte("<html><body>test</body></html>"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/myproject/_apis/build/builds/100/artifacts" {
			w.Write([]byte(`{"count":1,"value":[{"id":1,"name":"report","resource":{"downloadUrl":"http://` + r.Host + `/dl"}}]}`))
			return
		}
		if r.URL.Path == "/dl" {
			w.Write(zipData)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := NewTestClient("myorg", "myproject", srv.URL, srv.Client())
	_, _ = c.ListArtifacts(context.Background(), 100)
	content, err := c.DownloadAndExtract(context.Background(), 1)
	require.NoError(t, err)
	assert.Contains(t, string(content), "<html>")
}

func TestExtractFirstFile(t *testing.T) {
	data := buildTestZip(t, "data.json", []byte(`{"key":"value"}`))
	content, err := extractFirstFile(data)
	require.NoError(t, err)
	assert.Equal(t, `{"key":"value"}`, string(content))
}

func TestExtractFirstFile_Empty(t *testing.T) {
	data := buildTestZip(t, "", nil)
	_, err := extractFirstFile(data)
	assert.Error(t, err)
}

func TestMapResult(t *testing.T) {
	tests := []struct{ in, want string }{
		{"succeeded", "success"},
		{"failed", "failure"},
		{"canceled", "cancelled"},
		{"partiallySucceeded", "failure"},
		{"other", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, mapResult(tt.in))
	}
}

func TestMapChangeType(t *testing.T) {
	tests := []struct{ in, want string }{
		{"add", "added"},
		{"edit", "modified"},
		{"delete", "removed"},
		{"rename", "renamed"},
		{"other", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, mapChangeType(tt.in))
	}
}

func buildTestZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if name != "" && content != nil {
		fw, err := w.Create(name)
		require.NoError(t, err)
		_, err = fw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}
