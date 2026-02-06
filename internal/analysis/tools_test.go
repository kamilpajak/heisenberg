package analysis

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/kamilpajak/heisenberg/internal/github"
	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoneDiagnosis(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(85),
			"missing_information_sensitivity": "low",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, llm.CategoryDiagnosis, h.DiagnosisCategory())
	assert.Equal(t, 85, h.DiagnosisConfidence())
	assert.Equal(t, "low", h.DiagnosisSensitivity())
}

func TestDoneNoFailures(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "no_failures"},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, llm.CategoryNoFailures, h.DiagnosisCategory())
}

func TestDoneNotSupported(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "not_supported"},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, llm.CategoryNotSupported, h.DiagnosisCategory())
}

func TestDoneWithNoArgs(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, llm.CategoryDiagnosis, h.DiagnosisCategory())
	assert.Equal(t, 50, h.DiagnosisConfidence())
	assert.Equal(t, "medium", h.DiagnosisSensitivity())
}

func TestDoneWithFloat64Confidence(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(72.8),
			"missing_information_sensitivity": "high",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, 72, h.DiagnosisConfidence())
}

func TestDoneWithInvalidSensitivity(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":                        "diagnosis",
			"confidence":                      float64(60),
			"missing_information_sensitivity": "invalid",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, "medium", h.DiagnosisSensitivity())
}

func TestDoneWithInvalidCategory(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "unknown_value"},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, llm.CategoryDiagnosis, h.DiagnosisCategory(), "invalid category should fall back to diagnosis")
}

func TestDoneConfidenceClampedAbove100(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "diagnosis", "confidence": float64(150)},
	})
	require.NoError(t, err)
	assert.Equal(t, 100, h.DiagnosisConfidence())
}

func TestDoneConfidenceClampedBelowZero(t *testing.T) {
	h := &ToolHandler{}
	_, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{"category": "diagnosis", "confidence": float64(-10)},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, h.DiagnosisConfidence())
}

func TestDoneSkippedCategory(t *testing.T) {
	h := &ToolHandler{}
	assert.Empty(t, h.DiagnosisCategory())
}

func TestFindArtifactByName(t *testing.T) {
	h := &ToolHandler{
		artifacts: []gh.Artifact{
			{ID: 1, Name: "html-report"},
			{ID: 2, Name: "blob-report-1"},
			{ID: 3, Name: "test-results"},
		},
	}

	artifact := h.findArtifactByName("blob-report-1")
	require.NotNil(t, artifact)
	assert.Equal(t, int64(2), artifact.ID)
}

func TestFindArtifactByNameNotFound(t *testing.T) {
	h := &ToolHandler{
		artifacts: []gh.Artifact{
			{ID: 1, Name: "html-report"},
		},
	}

	artifact := h.findArtifactByName("nonexistent")
	assert.Nil(t, artifact)
}

func TestFindArtifactByNameEmpty(t *testing.T) {
	h := &ToolHandler{artifacts: nil}
	artifact := h.findArtifactByName("any")
	assert.Nil(t, artifact)
}

func TestFindTraceArtifactByName(t *testing.T) {
	h := &ToolHandler{
		artifacts: []gh.Artifact{
			{ID: 1, Name: "html-report", Expired: false},
			{ID: 2, Name: "test-results", Expired: false},
			{ID: 3, Name: "my-test-results", Expired: false},
		},
	}

	artifact := h.findTraceArtifact("my-test-results")
	require.NotNil(t, artifact)
	assert.Equal(t, int64(3), artifact.ID)
}

func TestFindTraceArtifactAutoDetect(t *testing.T) {
	h := &ToolHandler{
		artifacts: []gh.Artifact{
			{ID: 1, Name: "html-report", Expired: false},
			{ID: 2, Name: "test-results", Expired: false},
		},
	}

	artifact := h.findTraceArtifact("")
	require.NotNil(t, artifact)
	assert.Equal(t, int64(2), artifact.ID)
}

func TestFindTraceArtifactSkipsExpired(t *testing.T) {
	h := &ToolHandler{
		artifacts: []gh.Artifact{
			{ID: 1, Name: "test-results", Expired: true},
			{ID: 2, Name: "other-test-results", Expired: false},
		},
	}

	artifact := h.findTraceArtifact("")
	require.NotNil(t, artifact)
	assert.Equal(t, int64(2), artifact.ID)
}

func TestFindTraceArtifactNotFound(t *testing.T) {
	h := &ToolHandler{
		artifacts: []gh.Artifact{
			{ID: 1, Name: "html-report", Expired: false},
		},
	}

	artifact := h.findTraceArtifact("")
	assert.Nil(t, artifact)
}

func TestHasPendingTraces(t *testing.T) {
	tests := []struct {
		name         string
		artifacts    []gh.Artifact
		calledTraces bool
		want         bool
	}{
		{
			name:         "has pending traces",
			artifacts:    []gh.Artifact{{Name: "test-results", Expired: false}},
			calledTraces: false,
			want:         true,
		},
		{
			name:         "already called traces",
			artifacts:    []gh.Artifact{{Name: "test-results", Expired: false}},
			calledTraces: true,
			want:         false,
		},
		{
			name:         "no test-results artifact",
			artifacts:    []gh.Artifact{{Name: "html-report", Expired: false}},
			calledTraces: false,
			want:         false,
		},
		{
			name:         "test-results expired",
			artifacts:    []gh.Artifact{{Name: "test-results", Expired: true}},
			calledTraces: false,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ToolHandler{
				artifacts:    tt.artifacts,
				calledTraces: tt.calledTraces,
			}
			assert.Equal(t, tt.want, h.HasPendingTraces())
		})
	}
}

func TestCacheArtifacts(t *testing.T) {
	artifacts := []gh.Artifact{{ID: 1, Name: "test"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	err := h.cacheArtifacts(context.Background())
	require.NoError(t, err)
	assert.Len(t, h.artifacts, 1)

	// Second call should not make HTTP request (cached)
	err = h.cacheArtifacts(context.Background())
	require.NoError(t, err)
}

func TestCacheArtifactsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	err := h.cacheArtifacts(context.Background())
	assert.Error(t, err)
}

func TestFetchHTMLArtifact(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{
		GitHub:       ghClient,
		Owner:        "owner",
		Repo:         "repo",
		SnapshotHTML: func(content []byte) ([]byte, error) { return []byte("snapshot"), nil },
	}

	result, isDone, err := h.fetchHTMLArtifact(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Equal(t, "snapshot", result)
}

func TestFetchHTMLArtifactNoSnapshot(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{
		GitHub:       ghClient,
		Owner:        "owner",
		Repo:         "repo",
		SnapshotHTML: nil,
	}

	result, _, _ := h.fetchHTMLArtifact(context.Background(), 1)
	assert.Contains(t, result, "HTML rendering not available")
}

func TestFetchBlobInfo(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"data.bin": []byte("blob")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}
	artifact := &gh.Artifact{ID: 1, Name: "blob-report"}

	result, isDone, err := h.fetchBlobInfo(context.Background(), artifact)
	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "blob-report")
	assert.Contains(t, result, "bytes downloaded")
}

func TestFetchDefaultArtifact(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"data.json": []byte(`{"test": true}`)})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}

	result, isDone, err := h.fetchDefaultArtifact(context.Background(), 1)
	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "test")
}

func TestFetchArtifactContent(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{
		GitHub:       ghClient,
		Owner:        "owner",
		Repo:         "repo",
		SnapshotHTML: func(content []byte) ([]byte, error) { return []byte("html snapshot"), nil },
	}

	tests := []struct {
		name     string
		artifact *gh.Artifact
		wantStr  string
	}{
		{
			name:     "HTML artifact",
			artifact: &gh.Artifact{ID: 1, Name: "html-report"},
			wantStr:  "html snapshot",
		},
		{
			name:     "blob artifact",
			artifact: &gh.Artifact{ID: 2, Name: "blob-report-1"},
			wantStr:  "bytes downloaded",
		},
		{
			name:     "default artifact",
			artifact: &gh.Artifact{ID: 3, Name: "other"},
			wantStr:  "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, _ := h.fetchArtifactContent(context.Background(), tt.artifact)
			assert.Contains(t, result, tt.wantStr)
		})
	}
}

func TestFetchHTMLArtifactDownloadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}

	result, _, _ := h.fetchHTMLArtifact(context.Background(), 1)
	assert.Contains(t, result, "error")
}

func TestFetchHTMLArtifactSnapshotError(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{
		GitHub:       ghClient,
		Owner:        "owner",
		Repo:         "repo",
		SnapshotHTML: func(content []byte) ([]byte, error) { return nil, assert.AnError },
	}

	result, _, _ := h.fetchHTMLArtifact(context.Background(), 1)
	assert.Contains(t, result, "error")
}

func TestFetchBlobInfoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}
	artifact := &gh.Artifact{ID: 1, Name: "blob-report"}

	result, _, _ := h.fetchBlobInfo(context.Background(), artifact)
	assert.Contains(t, result, "error")
}

func TestFetchDefaultArtifactError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}

	result, _, _ := h.fetchDefaultArtifact(context.Background(), 1)
	assert.Contains(t, result, "error")
}

func TestFetchDefaultArtifactTruncate(t *testing.T) {
	// Create a large content that will be truncated
	largeContent := make([]byte, 150000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	zipData := buildZip(t, map[string][]byte{"large.txt": largeContent})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}

	result, _, _ := h.fetchDefaultArtifact(context.Background(), 1)
	assert.Len(t, result, 100000)
}

func TestExecuteGetWorkflowFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content":  "bmFtZTogQ0kK", // base64 "name: CI\n"
			"encoding": "base64",
		})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo"}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_workflow_file",
		Args: map[string]any{"path": ".github/workflows/ci.yml"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "name: CI")
}

func TestExecuteGetArtifact(t *testing.T) {
	artifacts := []gh.Artifact{{ID: 1, Name: "test-artifact"}}
	zipData := buildZip(t, map[string][]byte{"data.txt": []byte("artifact content")})

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			// First request: list artifacts
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		} else {
			// Second request: download artifact
			w.Write(zipData)
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_artifact",
		Args: map[string]any{"artifact_name": "test-artifact"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "artifact content")
}

func TestExecuteGetTestTraces(t *testing.T) {
	artifacts := []gh.Artifact{{ID: 1, Name: "test-results", Expired: false}}

	// Build a minimal test-results artifact
	traceZip := buildZip(t, map[string][]byte{
		"0-trace.trace": []byte(`{"type":"context-options"}` + "\n"),
	})
	artifactZip := buildZip(t, map[string][]byte{
		"test-dir/trace.zip": traceZip,
	})

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		} else {
			w.Write(artifactZip)
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "Test:")
	assert.True(t, h.calledTraces)
}

func TestExecuteGetArtifactMissingName(t *testing.T) {
	h := &ToolHandler{}
	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_artifact",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "artifact_name is required")
}

func TestExecuteGetArtifactNotFound(t *testing.T) {
	artifacts := []gh.Artifact{{ID: 1, Name: "other-artifact"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_artifact",
		Args: map[string]any{"artifact_name": "nonexistent"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "not found")
}

func TestExecuteGetTestTracesNotFound(t *testing.T) {
	artifacts := []gh.Artifact{{ID: 1, Name: "html-report", Expired: false}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})

	assert.Contains(t, result, "no test-results artifact found")
}

func TestExecuteGetTestTracesWithNameNotFound(t *testing.T) {
	artifacts := []gh.Artifact{{ID: 1, Name: "test-results", Expired: false}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{"artifact_name": "nonexistent"},
	})

	assert.Contains(t, result, "not found")
}

func TestExecuteGetRepoFileMissingPath(t *testing.T) {
	h := &ToolHandler{}
	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "path is required")
}

func TestExecuteListJobs(t *testing.T) {
	jobs := []gh.Job{
		{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
		{ID: 2, Name: "test", Status: "completed", Conclusion: "failure"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "list_jobs",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "build")
	assert.Contains(t, result, "test")
	assert.Contains(t, result, "failure")
}

func TestExecuteListJobsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "list_jobs",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "error")
}

func TestExecuteGetJobLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Build started\nTest failed: expected 1, got 2\nBuild finished"))
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_job_logs",
		Args: map[string]any{"job_id": float64(456)},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "Test failed")
}

func TestExecuteGetJobLogsMissingJobID(t *testing.T) {
	h := &ToolHandler{}
	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_job_logs",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "job_id is required")
}

func TestExecuteGetJobLogsInvalidJobID(t *testing.T) {
	h := &ToolHandler{}
	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_job_logs",
		Args: map[string]any{"job_id": "not a number"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "must be a number")
}

func TestExecuteGetJobLogsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_job_logs",
		Args: map[string]any{"job_id": float64(999)},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "error")
}

func TestExecuteGetJobLogsTruncate(t *testing.T) {
	// Create logs larger than 80000 bytes
	largeLogs := make([]byte, 100000)
	for i := range largeLogs {
		largeLogs[i] = 'x'
	}
	// Add marker at the end
	copy(largeLogs[len(largeLogs)-10:], []byte("END_MARKER"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(largeLogs)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient(srv.URL, srv.Client())
	h := &ToolHandler{GitHub: ghClient, Owner: "owner", Repo: "repo", RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_job_logs",
		Args: map[string]any{"job_id": float64(1)},
	})

	// Should be truncated to last 80000 chars, including the END_MARKER
	assert.Len(t, result, 80000)
	assert.Contains(t, result, "END_MARKER")
}

func TestExecuteUnknownTool(t *testing.T) {
	h := &ToolHandler{}
	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "unknown_tool",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "unknown tool")
}

func TestGetEmitter(t *testing.T) {
	emitter := &mockEmitter{}
	h := &ToolHandler{Emitter: emitter}
	assert.Equal(t, emitter, h.GetEmitter())
}

func TestGetEmitterNil(t *testing.T) {
	h := &ToolHandler{}
	assert.Nil(t, h.GetEmitter())
}

func TestIntArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		key     string
		want    int64
		wantErr bool
	}{
		{
			name: "float64 value",
			args: map[string]any{"job_id": float64(123)},
			key:  "job_id",
			want: 123,
		},
		{
			name: "json.Number value",
			args: map[string]any{"job_id": json.Number("456")},
			key:  "job_id",
			want: 456,
		},
		{
			name:    "missing key",
			args:    map[string]any{},
			key:     "job_id",
			wantErr: true,
		},
		{
			name:    "wrong type",
			args:    map[string]any{"job_id": "not a number"},
			key:     "job_id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := intArg(tt.args, tt.key)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestIntArgOrDefault(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		key  string
		def  int
		want int
	}{
		{
			name: "float64 value",
			args: map[string]any{"conf": float64(85)},
			key:  "conf",
			def:  50,
			want: 85,
		},
		{
			name: "json.Number value",
			args: map[string]any{"conf": json.Number("90")},
			key:  "conf",
			def:  50,
			want: 90,
		},
		{
			name: "missing key returns default",
			args: map[string]any{},
			key:  "conf",
			def:  50,
			want: 50,
		},
		{
			name: "wrong type returns default",
			args: map[string]any{"conf": "high"},
			key:  "conf",
			def:  50,
			want: 50,
		},
		{
			name: "invalid json.Number returns default",
			args: map[string]any{"conf": json.Number("invalid")},
			key:  "conf",
			def:  50,
			want: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intArgOrDefault(tt.args, tt.key, tt.def)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToolDeclarations(t *testing.T) {
	decls := ToolDeclarations()

	// Check we have all expected tools
	names := make([]string, len(decls))
	for i, d := range decls {
		names[i] = d.Name
	}

	assert.Contains(t, names, "list_jobs")
	assert.Contains(t, names, "get_job_logs")
	assert.Contains(t, names, "get_artifact")
	assert.Contains(t, names, "get_workflow_file")
	assert.Contains(t, names, "get_repo_file")
	assert.Contains(t, names, "get_test_traces")
	assert.Contains(t, names, "done")
	assert.Len(t, decls, 7)
}

type mockEmitter struct{}

func (m *mockEmitter) Emit(llm.ProgressEvent) {}

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
