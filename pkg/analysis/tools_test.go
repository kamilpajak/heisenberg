package analysis

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/ci"
	gh "github.com/kamilpajak/heisenberg/pkg/github"
	"github.com/kamilpajak/heisenberg/pkg/llm"
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

func TestDoneWithAnalysesArray(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":   "diagnosis",
			"confidence": float64(90),
			"analyses": []any{
				map[string]any{
					"title":        "Timeout in checkout",
					"failure_type": "timeout",
					"file_path":    "tests/checkout.spec.ts",
					"line_number":  float64(45),
					"bug_location": "test",
					"root_cause":   "Cookie banner overlay",
					"remediation":  "Dismiss banner first",
				},
				map[string]any{
					"title":        "Assertion failed in login",
					"failure_type": "assertion",
					"file_path":    "tests/login.spec.ts",
					"line_number":  float64(12),
					"bug_location": "production",
					"root_cause":   "Changed redirect URL",
					"remediation":  "Update expected URL",
				},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)

	rcas := h.DiagnosisRCAs()
	require.Len(t, rcas, 2)

	assert.Equal(t, "Timeout in checkout", rcas[0].Title)
	assert.Equal(t, llm.FailureTypeTimeout, rcas[0].FailureType)
	assert.Equal(t, "tests/checkout.spec.ts", rcas[0].Location.FilePath)
	assert.Equal(t, 45, rcas[0].Location.LineNumber)
	assert.Equal(t, llm.BugLocationTest, rcas[0].BugLocation)

	assert.Equal(t, "Assertion failed in login", rcas[1].Title)
	assert.Equal(t, llm.FailureTypeAssertion, rcas[1].FailureType)
	assert.Equal(t, llm.BugLocationProduction, rcas[1].BugLocation)
}

func TestDoneWithFlatArgsBackwardCompat(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category":     "diagnosis",
			"confidence":   float64(80),
			"title":        "Network error",
			"failure_type": "network",
			"file_path":    "tests/api.spec.ts",
			"root_cause":   "API server down",
			"remediation":  "Retry",
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)

	rcas := h.DiagnosisRCAs()
	require.Len(t, rcas, 1)
	assert.Equal(t, "Network error", rcas[0].Title)
	assert.Equal(t, llm.FailureTypeNetwork, rcas[0].FailureType)
}

func TestDoneNoFailures_SkipsRCAs(t *testing.T) {
	h := &ToolHandler{}
	_, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "done",
		Args: map[string]any{
			"category": "no_failures",
			"analyses": []any{
				map[string]any{
					"title":        "Should be ignored",
					"failure_type": "assertion",
				},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, isDone)
	assert.Equal(t, llm.CategoryNoFailures, h.DiagnosisCategory())
	assert.Empty(t, h.DiagnosisRCAs(), "no_failures should not parse analyses")
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
		artifacts: []ci.Artifact{
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
		artifacts: []ci.Artifact{
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
		artifacts: []ci.Artifact{
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
		artifacts: []ci.Artifact{
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
		artifacts: []ci.Artifact{
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
		artifacts: []ci.Artifact{
			{ID: 1, Name: "html-report", Expired: false},
		},
	}

	artifact := h.findTraceArtifact("")
	assert.Nil(t, artifact)
}

func TestHasPendingTraces(t *testing.T) {
	tests := []struct {
		name         string
		artifacts    []ci.Artifact
		calledTraces bool
		want         bool
	}{
		{
			name:         "has pending traces",
			artifacts:    []ci.Artifact{{Name: "test-results", Expired: false}},
			calledTraces: false,
			want:         true,
		},
		{
			name:         "already called traces",
			artifacts:    []ci.Artifact{{Name: "test-results", Expired: false}},
			calledTraces: true,
			want:         false,
		},
		{
			name:         "no test-results artifact",
			artifacts:    []ci.Artifact{{Name: "html-report", Expired: false}},
			calledTraces: false,
			want:         false,
		},
		{
			name:         "test-results expired",
			artifacts:    []ci.Artifact{{Name: "test-results", Expired: true}},
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
	artifacts := []ci.Artifact{{ID: 1, Name: "test"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	err := h.cacheArtifacts(context.Background())
	assert.Error(t, err)
}

func TestFetchHTMLArtifact(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:           ghClient,
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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:           ghClient,
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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}
	artifact := &ci.Artifact{ID: 1, Name: "blob-report"}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:           ghClient,
		SnapshotHTML: func(content []byte) ([]byte, error) { return []byte("html snapshot"), nil },
	}

	tests := []struct {
		name     string
		artifact *ci.Artifact
		wantStr  string
	}{
		{
			name:     "HTML artifact",
			artifact: &ci.Artifact{ID: 1, Name: "html-report"},
			wantStr:  "html snapshot",
		},
		{
			name:     "blob artifact",
			artifact: &ci.Artifact{ID: 2, Name: "blob-report-1"},
			wantStr:  "bytes downloaded",
		},
		{
			name:     "default artifact",
			artifact: &ci.Artifact{ID: 3, Name: "other"},
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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}

	result, _, _ := h.fetchHTMLArtifact(context.Background(), 1)
	assert.Contains(t, result, "error")
}

func TestFetchHTMLArtifactSnapshotError(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"report.html": []byte("<html>test</html>")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:           ghClient,
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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}
	artifact := &ci.Artifact{ID: 1, Name: "blob-report"}

	result, _, _ := h.fetchBlobInfo(context.Background(), artifact)
	assert.Contains(t, result, "error")
}

func TestFetchDefaultArtifactError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_workflow_file",
		Args: map[string]any{"path": ".github/workflows/ci.yml"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "name: CI")
}

func TestExecuteGetArtifact(t *testing.T) {
	artifacts := []ci.Artifact{{ID: 1, Name: "test-artifact"}}
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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_artifact",
		Args: map[string]any{"artifact_name": "test-artifact"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "artifact content")
}

func TestExecuteGetTestTraces(t *testing.T) {
	artifacts := []ci.Artifact{{ID: 1, Name: "test-results", Expired: false}}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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
	artifacts := []ci.Artifact{{ID: 1, Name: "other-artifact"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_artifact",
		Args: map[string]any{"artifact_name": "nonexistent"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "not found")
}

func TestExecuteGetArtifactCacheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_artifact",
		Args: map[string]any{"artifact_name": "test"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "error")
}

func TestExecuteGetTestTracesNotFound(t *testing.T) {
	artifacts := []ci.Artifact{{ID: 1, Name: "html-report", Expired: false}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})

	assert.Contains(t, result, "no test-results artifact found")
}

func TestExecuteGetTestTracesWithNameNotFound(t *testing.T) {
	artifacts := []ci.Artifact{{ID: 1, Name: "test-results", Expired: false}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{"artifact_name": "nonexistent"},
	})

	assert.Contains(t, result, "not found")
}

func TestExecuteGetTestTracesCacheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})

	assert.Contains(t, result, "error")
}

func TestExecuteGetTestTracesDownloadError(t *testing.T) {
	artifacts := []ci.Artifact{{ID: 1, Name: "test-results", Expired: false}}
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})

	assert.Contains(t, result, "error")
}

func TestExecuteGetTestTracesParseError(t *testing.T) {
	artifacts := []ci.Artifact{{ID: 1, Name: "test-results", Expired: false}}
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		} else {
			// Return invalid zip
			w.Write([]byte("not a zip"))
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_test_traces",
		Args: map[string]any{},
	})

	assert.Contains(t, result, "error")
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

func TestExecuteGetRepoFileError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "nonexistent.txt"},
	})

	require.NoError(t, err)
	assert.False(t, isDone)
	assert.Contains(t, result, "error")
}

func TestStringArgOrDefault(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		key  string
		def  string
		want string
	}{
		{
			name: "string value",
			args: map[string]any{"cat": "diagnosis"},
			key:  "cat",
			def:  "default",
			want: "diagnosis",
		},
		{
			name: "missing key returns default",
			args: map[string]any{},
			key:  "cat",
			def:  "default",
			want: "default",
		},
		{
			name: "wrong type returns default",
			args: map[string]any{"cat": 123},
			key:  "cat",
			def:  "default",
			want: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringArgOrDefault(tt.args, tt.key, tt.def)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExecuteListJobs(t *testing.T) {
	jobs := []ci.Job{
		{ID: 1, Name: "build", Status: "completed", Conclusion: "success"},
		{ID: 2, Name: "test", Status: "completed", Conclusion: "failure"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{CI: ghClient, RunID: 123}

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
	decls := ToolDeclarations(nil)

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
	assert.Contains(t, names, "get_pr_diff")
	assert.Contains(t, names, "done")
	assert.Len(t, decls, 8)
}

func TestToolDeclarations_DoneAnalysesArrayHasItems(t *testing.T) {
	decls := ToolDeclarations(nil)

	// Find the done tool
	var doneDecl *llm.FunctionDeclaration
	for i := range decls {
		if decls[i].Name == "done" {
			doneDecl = &decls[i]
			break
		}
	}
	require.NotNil(t, doneDecl, "done tool should exist")
	require.NotNil(t, doneDecl.Parameters, "done tool should have parameters")

	// Check analyses array exists and has proper nested schema
	analysesSchema, ok := doneDecl.Parameters.Properties["analyses"]
	require.True(t, ok, "done tool should have analyses property")
	assert.Equal(t, "array", analysesSchema.Type, "analyses should be array type")
	require.NotNil(t, analysesSchema.Items, "analyses array MUST have Items schema for Gemini API")
	assert.Equal(t, "object", analysesSchema.Items.Type, "analyses items should be objects")

	// Check evidence nested inside analyses items
	evidenceSchema, ok := analysesSchema.Items.Properties["evidence"]
	require.True(t, ok, "analyses items should have evidence property")
	assert.Equal(t, "array", evidenceSchema.Type, "evidence should be array type")
	require.NotNil(t, evidenceSchema.Items, "evidence array MUST have Items schema for Gemini API")
	assert.Equal(t, "object", evidenceSchema.Items.Type, "evidence items should be objects")
	assert.Contains(t, evidenceSchema.Items.Properties, "type", "evidence items should have type property")
	assert.Contains(t, evidenceSchema.Items.Properties, "content", "evidence items should have content property")
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

func TestPrerequisiteGating_BlocksRepoFileBeforeArtifacts(t *testing.T) {
	h := &ToolHandler{
		artifacts: []ci.Artifact{{Name: "playwright-report", Expired: false}},
	}
	result, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "package.json"},
	})
	require.NoError(t, err)
	assert.Contains(t, result, "ACCESS_DENIED")
	assert.Contains(t, result, "fetch test failure data")
}

func TestPrerequisiteGating_AllowsAfterJobLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/logs") {
			w.Write([]byte("test log output"))
		} else if strings.Contains(r.URL.Path, "/contents/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content":  "cGFja2FnZS5qc29u", // base64 "package.json"
				"encoding": "base64",
			})
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:        ghClient,
		RunID:     123,
		artifacts: []ci.Artifact{{Name: "playwright-report", Expired: false}},
	}

	// Before job logs → blocked
	result, _, _ := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "package.json"},
	})
	assert.Contains(t, result, "ACCESS_DENIED")

	// Fetch job logs
	_, _, _ = h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_job_logs",
		Args: map[string]any{"job_id": float64(1)},
	})

	// After job logs → allowed
	result, _, _ = h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "package.json"},
	})
	assert.NotContains(t, result, "ACCESS_DENIED")
}

func TestPrerequisiteGating_DisabledWhenNoTestArtifacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content":  "cGFja2FnZS5qc29u",
			"encoding": "base64",
		})
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:        ghClient,
		RunID:     123,
		artifacts: []ci.Artifact{}, // no artifacts
	}

	// No artifacts → get_repo_file allowed immediately
	result, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "package.json"},
	})
	require.NoError(t, err)
	assert.NotContains(t, result, "ACCESS_DENIED")
}

func TestHasTestArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		artifacts []ci.Artifact
		want      bool
	}{
		{"with playwright report", []ci.Artifact{{Name: "playwright-report", Expired: false}}, true},
		{"with blob report", []ci.Artifact{{Name: "blob-report-1", Expired: false}}, true},
		{"with test results", []ci.Artifact{{Name: "test-results", Expired: false}}, true},
		{"with html report", []ci.Artifact{{Name: "html-report--attempt-1", Expired: false}}, true},
		{"build cache only", []ci.Artifact{{Name: "build-cache", Expired: false}}, false},
		{"docker image only", []ci.Artifact{{Name: "docker-image.tar", Expired: false}}, false},
		{"expired report", []ci.Artifact{{Name: "playwright-report", Expired: true}}, false},
		{"empty", []ci.Artifact{}, false},
		{"mixed", []ci.Artifact{
			{Name: "build-cache", Expired: false},
			{Name: "playwright-report", Expired: false},
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ToolHandler{artifacts: tt.artifacts}
			assert.Equal(t, tt.want, h.HasTestArtifacts())
		})
	}
}

func TestSmartNotFound_ReturnsDirectoryListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/contents/tests/auth.spec.ts") {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not Found"}`))
		} else if strings.Contains(path, "/contents/tests") {
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"name": "e2e", "type": "dir"},
				{"name": "setup.ts", "type": "file"},
				{"name": "utils.ts", "type": "file"},
			})
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:                  ghClient,
		RunID:               123,
		hasReadErrorContext: true, // bypass prerequisite gating
	}

	result, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "tests/auth.spec.ts"},
	})
	require.NoError(t, err)

	assert.Contains(t, result, "file not found")
	assert.Contains(t, result, "e2e/")
	assert.Contains(t, result, "setup.ts")
	assert.Contains(t, result, "contents")
}

func TestSmartNotFound_RootDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/contents/nonexistent.ts") {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Not Found"}`))
		} else if strings.HasSuffix(path, "/contents/.") || strings.HasSuffix(path, "/contents/") {
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"name": "src", "type": "dir"},
				{"name": "package.json", "type": "file"},
			})
		}
	}))
	defer srv.Close()

	ghClient := gh.NewTestClient("owner", "repo", srv.URL, srv.Client())
	h := &ToolHandler{
		CI:                  ghClient,
		RunID:               123,
		hasReadErrorContext: true,
	}

	result, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_repo_file",
		Args: map[string]any{"path": "nonexistent.ts"},
	})
	require.NoError(t, err)

	assert.Contains(t, result, "file not found")
	assert.Contains(t, result, "package.json")
}

func TestClassifyFilePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"tests/checkout.spec.ts", "test"},
		{"src/components/Button.test.tsx", "test"},
		{"pkg/analysis/tools_test.go", "test"},
		{"test/fixtures/data.json", "test"},
		{"src/pricing.ts", "production"},
		{"app/models/user.rb", "production"},
		{"pkg/llm/client.go", "production"},
		{"lib/utils.js", "production"},
		{".github/workflows/ci.yml", "config"},
		{"Dockerfile", "config"},
		{"docker-compose.yml", "config"},
		{"Makefile", "config"},
		{"README.md", "other"},
		{"docs/guide.md", "other"},
		{"package-lock.json", "other"},
		{"pnpm-lock.yaml", "other"},
		{"assets/logo.png", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, classifyFilePath(tt.path), "classifyFilePath(%q)", tt.path)
	}
}

func TestGetPRDiff_WithPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/pulls/42/files") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[
				{"filename": "src/pricing.ts", "status": "modified", "additions": 20, "deletions": 5, "patch": "@@ -40,6 +40,7 @@\n+return 0"},
				{"filename": "tests/checkout.spec.ts", "status": "modified", "additions": 3, "deletions": 1, "patch": "@@ -10 @@"},
				{"filename": "package-lock.json", "status": "modified", "additions": 500, "deletions": 400, "patch": ""}
			]`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	h := &ToolHandler{
		CI:       gh.NewTestClient("owner", "repo", srv.URL, srv.Client()),
		PRNumber: 42,
	}

	result, isDone, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_pr_diff",
		Args: map[string]any{},
	})

	require.NoError(t, err)
	assert.False(t, isDone)

	// Parse structured response
	var diff struct {
		Kind       string `json:"kind"`
		PRNumber   int    `json:"pr_number"`
		TotalFiles int    `json:"total_files"`
		Files      []struct {
			Path     string `json:"path"`
			Category string `json:"category"`
			Patch    string `json:"patch"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(result), &diff))

	assert.Equal(t, "pull_request", diff.Kind)
	assert.Equal(t, 42, diff.PRNumber)
	// package-lock.json should be filtered
	assert.Equal(t, 2, diff.TotalFiles)
	assert.Equal(t, "production", diff.Files[0].Category)
	assert.Equal(t, "test", diff.Files[1].Category)
}

func TestGetPRDiff_NoPR(t *testing.T) {
	// CI provider is needed but will return error since PRNumber=0 and HeadSHA=""
	h := &ToolHandler{
		CI:       gh.NewTestClient("owner", "repo", "http://unused", http.DefaultClient),
		PRNumber: 0,
	}

	result, _, err := h.Execute(context.Background(), llm.FunctionCall{
		Name: "get_pr_diff",
		Args: map[string]any{},
	})

	require.NoError(t, err)

	var diff struct {
		Kind string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal([]byte(result), &diff))
	assert.Equal(t, "none", diff.Kind)
}
