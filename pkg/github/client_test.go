package github

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

func TestClassifyArtifact(t *testing.T) {
	tests := []struct {
		name string
		want ci.ArtifactType
	}{
		{"html-report--attempt-1", ci.ArtifactHTML},
		{"html-report--attempt-2", ci.ArtifactHTML},
		{"playwright-report", ci.ArtifactHTML},
		{"test-results.json", ci.ArtifactJSON},
		{"playwright_report.json", ci.ArtifactJSON},
		{"blob-report-1", ci.ArtifactBlob},
		{"blob-report-2", ci.ArtifactBlob},
		{"blob-report-10", ci.ArtifactBlob},
		{"test-results", ""},
		{"e2e-coverage", ""},
		{"combined-test-catalog", ""},
		{"check-reports", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyArtifact(tt.name))
		})
	}
}

func TestNewAPIRequest(t *testing.T) {
	c := &Client{token: "test-token"}
	req, err := c.newAPIRequest(context.Background(), "https://api.github.com/test")

	require.NoError(t, err)
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "https://api.github.com/test", req.URL.String())
	assert.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
	assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
}

func TestNewAPIRequestNoToken(t *testing.T) {
	c := &Client{token: ""}
	req, err := c.newAPIRequest(context.Background(), "https://api.github.com/test")

	require.NoError(t, err)
	assert.Empty(t, req.Header.Get("Authorization"))
	assert.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
}

func buildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		fw, err := w.Create(name)
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestCheckArtifacts_AllExpired(t *testing.T) {
	artifacts := []ci.Artifact{
		{ID: 1, Name: "html-report", Expired: true},
		{ID: 2, Name: "blob-report", Expired: true},
	}

	status := ci.CheckArtifacts(artifacts)

	assert.Equal(t, 2, status.Total)
	assert.Equal(t, 2, status.Expired)
	assert.Equal(t, 0, status.Available)
	assert.False(t, status.HasUsable)
	assert.True(t, status.AllExpired)
}

func TestCheckArtifacts_SomeExpired(t *testing.T) {
	artifacts := []ci.Artifact{
		{ID: 1, Name: "html-report", Expired: true},
		{ID: 2, Name: "blob-report", Expired: false},
	}

	status := ci.CheckArtifacts(artifacts)

	assert.Equal(t, 2, status.Total)
	assert.Equal(t, 1, status.Expired)
	assert.Equal(t, 1, status.Available)
	assert.True(t, status.HasUsable)
	assert.False(t, status.AllExpired)
}

func TestCheckArtifacts_NoneExpired(t *testing.T) {
	artifacts := []ci.Artifact{
		{ID: 1, Name: "html-report", Expired: false},
		{ID: 2, Name: "blob-report", Expired: false},
	}

	status := ci.CheckArtifacts(artifacts)

	assert.Equal(t, 2, status.Total)
	assert.Equal(t, 0, status.Expired)
	assert.Equal(t, 2, status.Available)
	assert.True(t, status.HasUsable)
	assert.False(t, status.AllExpired)
}

func TestCheckArtifacts_Empty(t *testing.T) {
	status := ci.CheckArtifacts(nil)

	assert.Equal(t, 0, status.Total)
	assert.Equal(t, 0, status.Expired)
	assert.Equal(t, 0, status.Available)
	assert.False(t, status.HasUsable)
	assert.False(t, status.AllExpired)
}

func TestListDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"name": "e2e", "type": "dir"},
			{"name": "setup.ts", "type": "file"},
			{"name": "utils.ts", "type": "file"}
		]`))
	}))
	defer srv.Close()

	c := NewTestClient("owner", "repo", srv.URL, srv.Client())
	entries, err := c.ListDirectory(context.Background(), "tests")
	require.NoError(t, err)
	assert.Len(t, entries, 3)
	assert.Equal(t, "e2e/", entries[0])
	assert.Equal(t, "setup.ts", entries[1])
}

func TestListDirectory_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewTestClient("owner", "repo", srv.URL, srv.Client())
	_, err := c.ListDirectory(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestGetChangedFiles_PR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/pulls/42/files", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"filename": "src/pricing.ts", "status": "modified", "additions": 20, "deletions": 5, "patch": "@@ -40,6 +40,7 @@\n+  return 0"},
			{"filename": "tests/checkout.spec.ts", "status": "modified", "additions": 3, "deletions": 1, "patch": "@@ -10,3 +10,5 @@"}
		]`))
	}))
	defer srv.Close()

	c := NewTestClient("owner", "repo", srv.URL, srv.Client())
	files, err := c.GetChangedFiles(context.Background(), ci.ChangeRef{PRNumber: 42})

	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "src/pricing.ts", files[0].Path)
	assert.Equal(t, "modified", files[0].Status)
	assert.Equal(t, 20, files[0].Additions)
	assert.Equal(t, 5, files[0].Deletions)
	assert.Contains(t, files[0].Patch, "return 0")
	assert.Equal(t, "tests/checkout.spec.ts", files[1].Path)
}

func TestGetChangedFiles_Compare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/compare/main...abc123", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"files": [
				{"filename": "src/app.ts", "status": "added", "additions": 50, "deletions": 0, "patch": "@@ -0,0 +1,50 @@"}
			]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("owner", "repo", srv.URL, srv.Client())
	files, err := c.GetChangedFiles(context.Background(), ci.ChangeRef{HeadSHA: "abc123", BaseBranch: "main"})

	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "src/app.ts", files[0].Path)
	assert.Equal(t, "added", files[0].Status)
	assert.Equal(t, 50, files[0].Additions)
}

func TestGetRun_PullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": 123,
			"head_branch": "feature/test",
			"head_sha": "abc",
			"event": "pull_request",
			"pull_requests": [{"number": 42}]
		}`))
	}))
	defer srv.Close()

	c := NewTestClient("owner", "repo", srv.URL, srv.Client())
	run, err := c.GetRun(context.Background(), 123)

	require.NoError(t, err)
	require.Len(t, run.PRNumbers, 1)
	assert.Equal(t, 42, run.PRNumbers[0])
}

func TestName(t *testing.T) {
	c := NewTestClient("o", "r", "http://localhost", http.DefaultClient)
	assert.Equal(t, "github", c.Name())
}

func TestAnalysisHints(t *testing.T) {
	c := NewTestClient("o", "r", "http://localhost", http.DefaultClient)
	assert.Contains(t, c.AnalysisHints(), "Playwright")
}

func TestListRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"workflow_runs":[{"id":1,"name":"CI","conclusion":"failure","head_branch":"main","head_sha":"abc","event":"push"}]}`))
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	runs, err := c.ListRuns(context.Background(), ci.RunFilter{})
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "failure", runs[0].Conclusion)
	assert.Equal(t, "main", runs[0].Branch)
}

func TestListJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jobs":[{"id":10,"name":"build","status":"completed","conclusion":"success"}]}`))
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	jobs, err := c.ListJobs(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "build", jobs[0].Name)
}

func TestListArtifacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"artifacts":[{"id":5,"name":"report","size_in_bytes":1024,"expired":false}]}`))
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	arts, err := c.ListArtifacts(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, arts, 1)
	assert.Equal(t, "report", arts[0].Name)
	assert.Equal(t, int64(1024), arts[0].SizeBytes)
}

func TestGetJobLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("line1\nline2\nERROR: something failed"))
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	logs, err := c.GetJobLogs(context.Background(), 10)
	require.NoError(t, err)
	assert.Contains(t, logs, "ERROR: something failed")
}

func TestDownloadArtifact(t *testing.T) {
	content := []byte("zip-content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	data, err := c.DownloadArtifact(context.Background(), 5)
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestDownloadAndExtract(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	content, err := c.DownloadAndExtract(context.Background(), 5)
	require.NoError(t, err)
	assert.Contains(t, string(content), "<html>")
}

func TestGetRepoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"content":"aGVsbG8=","encoding":"base64"}`))
	}))
	defer srv.Close()
	c := NewTestClient("o", "r", srv.URL, srv.Client())
	content, err := c.GetRepoFile(context.Background(), "README.md")
	require.NoError(t, err)
	assert.Equal(t, "hello", content)
}
