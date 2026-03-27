package saas

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

func TestNewClient_ReturnsNilWhenNotConfigured(t *testing.T) {
	t.Setenv("HEISENBERG_API_URL", "")
	t.Setenv("HEISENBERG_API_KEY", "")
	c := NewClient()
	assert.Nil(t, c)
}

func TestNewClient_ReturnsClientWhenConfigured(t *testing.T) {
	t.Setenv("HEISENBERG_API_URL", "https://api.heisenberg.dev")
	t.Setenv("HEISENBERG_API_KEY", "hb_test_key")
	c := NewClient()
	assert.NotNil(t, c)
	assert.Equal(t, "https://api.heisenberg.dev", c.BaseURL())
}

func TestSubmitAnalysis_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/organizations/org-123/analyses", r.URL.Path)
		assert.Equal(t, "Bearer hb_test_key", r.Header.Get("Authorization"))

		var body map[string]any
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "testowner", body["owner"])
		assert.Equal(t, "testrepo", body["repo"])
		assert.Equal(t, float64(12345), body["run_id"])

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "analysis-uuid-123"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "hb_test_key", http: http.DefaultClient}
	id, err := c.SubmitAnalysis(context.Background(), SubmitParams{
		OrgID: "org-123",
		Owner: "testowner",
		Repo:  "testrepo",
		RunID: 12345,
		Result: &llm.AnalysisResult{
			Text:     "Analysis text",
			Category: llm.CategoryDiagnosis,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "analysis-uuid-123", id)
}

func TestSubmitAnalysis_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "bad_key", http: http.DefaultClient}
	_, err := c.SubmitAnalysis(context.Background(), SubmitParams{
		OrgID:  "org-123",
		Owner:  "testowner",
		Repo:   "testrepo",
		RunID:  12345,
		Result: &llm.AnalysisResult{Text: "x", Category: "diagnosis"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestSubmitAnalysis_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already exists"})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, apiKey: "key", http: http.DefaultClient}
	_, err := c.SubmitAnalysis(context.Background(), SubmitParams{
		OrgID:  "org-123",
		Owner:  "testowner",
		Repo:   "testrepo",
		RunID:  12345,
		Result: &llm.AnalysisResult{Text: "x", Category: "diagnosis"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "409")
}
