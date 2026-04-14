package semcluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEmbedder(handler http.HandlerFunc) (*GeminiEmbedder, *httptest.Server) {
	srv := httptest.NewServer(handler)
	e := NewGeminiEmbedder("test-key")
	e.baseURL = srv.URL
	e.httpClient = srv.Client()
	return e, srv
}

func TestGeminiEmbedder_Success(t *testing.T) {
	e, srv := newTestEmbedder(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "embedContent")
		assert.Equal(t, "test-key", r.URL.Query().Get("key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body geminiEmbedRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "hello", body.Content.Parts[0].Text)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(geminiEmbedResponse{
			Embedding: struct {
				Values []float32 `json:"values"`
			}{Values: []float32{0.1, 0.2, 0.3}},
		})
	})
	defer srv.Close()

	vec, err := e.Embed(context.Background(), "hello")

	require.NoError(t, err)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, vec)
}

func TestGeminiEmbedder_APIError_StatusCode(t *testing.T) {
	e, srv := newTestEmbedder(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
	})
	defer srv.Close()

	_, err := e.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "quota exceeded")
}

func TestGeminiEmbedder_APIError_StructuredError(t *testing.T) {
	e, srv := newTestEmbedder(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(geminiEmbedResponse{
			Error: &geminiEmbedError{Code: 400, Message: "bad request"},
		})
	})
	defer srv.Close()

	_, err := e.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "bad request")
}

func TestGeminiEmbedder_EmptyVector(t *testing.T) {
	e, srv := newTestEmbedder(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[]}}`))
	})
	defer srv.Close()

	_, err := e.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty vector")
}

func TestGeminiEmbedder_MalformedJSON(t *testing.T) {
	e, srv := newTestEmbedder(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	})
	defer srv.Close()

	_, err := e.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestGeminiEmbedder_NetworkError(t *testing.T) {
	e, srv := newTestEmbedder(func(_ http.ResponseWriter, _ *http.Request) {})
	srv.Close() // force connection refused

	_, err := e.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "embedding api call")
}

func TestGeminiEmbedder_ContextCancelled(t *testing.T) {
	e, srv := newTestEmbedder(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.Embed(ctx, "hello")

	require.Error(t, err)
}

func TestGeminiEmbedder_InvalidBaseURL(t *testing.T) {
	e := NewGeminiEmbedder("test-key")
	e.baseURL = "://invalid"

	_, err := e.Embed(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create embedding request")
}

func TestNewGeminiEmbedder_Defaults(t *testing.T) {
	e := NewGeminiEmbedder("abc")
	assert.Equal(t, "abc", e.apiKey)
	assert.Equal(t, 768, e.dimensions)
	assert.Equal(t, "gemini-embedding-001", e.model)
	assert.Contains(t, e.baseURL, "generativelanguage.googleapis.com")
	assert.NotNil(t, e.httpClient)
}

func TestGeminiEmbedder_WithHTTPClient(t *testing.T) {
	e := NewGeminiEmbedder("abc")
	custom := &http.Client{}
	returned := e.WithHTTPClient(custom)
	assert.Same(t, e, returned, "WithHTTPClient returns receiver for chaining")
	assert.Same(t, custom, e.httpClient)
}
