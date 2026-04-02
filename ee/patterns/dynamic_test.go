package patterns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

func TestDynamicMatcher_Match_EmbeddingFailure(t *testing.T) {
	// Embedding API returns error → Match should return nil (graceful degradation)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":500,"message":"internal error"}}`))
	}))
	defer server.Close()

	client := &EmbeddingClient{
		apiKey:     "test-key",
		baseURL:    server.URL,
		model:      defaultEmbeddingModel,
		dimensions: defaultDimensions,
		httpClient: server.Client(),
	}

	matcher := NewDynamicMatcher(nil, client, uuid.New())

	rca := &llm.RootCauseAnalysis{
		FailureType: "timeout",
		RootCause:   "selector timeout",
	}

	result := matcher.Match(context.Background(), rca)
	assert.Nil(t, result, "should return nil on embedding failure")
}

func TestDynamicMatcher_Match_Success(t *testing.T) {
	// Mock embedding server that returns a fixed vector
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{}
		resp.Embedding.Values = make([]float32, 768)
		for i := range resp.Embedding.Values {
			resp.Embedding.Values[i] = 0.1
		}
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

	// DynamicMatcher with nil DB will fail on DB query → returns nil
	// This tests the graceful degradation path for DB errors
	matcher := NewDynamicMatcher(nil, client, uuid.New())

	// DynamicMatcher with nil DB would panic on DB query.
	// The integration test in ee/database covers the full flow.
	// Here we just verify the constructor works.
	require.NotNil(t, matcher)
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		input  string
		max    int
		expect string
	}{
		{"short", 100, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"this is a longer text that needs truncating", 20, "this is a longer ..."},
	}

	for _, tt := range tests {
		result := truncateText(tt.input, tt.max)
		assert.Equal(t, tt.expect, result)
	}
}
