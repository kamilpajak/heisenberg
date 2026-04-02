package patterns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeEmbeddingText(t *testing.T) {
	tests := []struct {
		name     string
		rca      llm.RootCauseAnalysis
		contains []string
		excludes []string
	}{
		{
			name: "full RCA with location and symptom",
			rca: llm.RootCauseAnalysis{
				FailureType: "timeout",
				Location:    &llm.CodeLocation{FilePath: "tests/checkout.spec.ts"},
				RootCause:   "waitForSelector timed out in beforeEach hook",
				Symptom:     "Test setup failed with TimeoutError",
			},
			contains: []string{
				"failure_type: timeout",
				"file_pattern: *.ts",
				"root_cause: waitForSelector timed out in beforeEach hook",
				"symptom: Test setup failed with TimeoutError",
			},
		},
		{
			name: "RCA without location",
			rca: llm.RootCauseAnalysis{
				FailureType: "network",
				RootCause:   "connection refused on port 5432",
			},
			contains: []string{
				"failure_type: network",
				"root_cause: connection refused on port 5432",
			},
			excludes: []string{"file_pattern:", "symptom:"},
		},
		{
			name: "RCA with empty file path",
			rca: llm.RootCauseAnalysis{
				FailureType: "assertion",
				Location:    &llm.CodeLocation{FilePath: ""},
				RootCause:   "expected 200 got 500",
				Symptom:     "API returned wrong status",
			},
			contains: []string{
				"failure_type: assertion",
				"root_cause: expected 200 got 500",
				"symptom: API returned wrong status",
			},
			excludes: []string{"file_pattern:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := ComputeEmbeddingText(&tt.rca)
			for _, s := range tt.contains {
				assert.Contains(t, text, s)
			}
			for _, s := range tt.excludes {
				assert.NotContains(t, text, s)
			}
		})
	}
}

func TestEmbeddingClient_Embed(t *testing.T) {
	expectedVector := make([]float32, 768)
	for i := range expectedVector {
		expectedVector[i] = float32(i) / 768.0
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "embedContent")
		assert.Contains(t, r.URL.RawQuery, "key=test-key")

		var req embedRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "models/gemini-embedding-001", req.Model)
		assert.Equal(t, 768, req.OutputDimensionality)
		assert.Len(t, req.Content.Parts, 1)
		assert.NotEmpty(t, req.Content.Parts[0].Text)

		resp := embedResponse{}
		resp.Embedding.Values = expectedVector
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &EmbeddingClient{
		apiKey:     "test-key",
		baseURL:    server.URL,
		model:      defaultEmbeddingModel,
		dimensions: defaultDimensions,
		httpClient: server.Client(),
	}

	vector, err := client.Embed(context.Background(), "test failure text")
	require.NoError(t, err)
	assert.Len(t, vector, 768)
	assert.InDelta(t, 0.0, vector[0], 0.001)
}

func TestEmbeddingClient_Embed_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":429,"message":"quota exceeded"}}`))
	}))
	defer server.Close()

	client := &EmbeddingClient{
		apiKey:     "test-key",
		baseURL:    server.URL,
		model:      defaultEmbeddingModel,
		dimensions: defaultDimensions,
		httpClient: server.Client(),
	}

	_, err := client.Embed(context.Background(), "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestNewEmbeddingClient_MissingKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := NewEmbeddingClient("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GOOGLE_API_KEY")
}
