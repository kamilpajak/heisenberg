package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeSSE_InvalidRepo(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analyze?repo=invalid")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAnalyzeSSE_MissingRepo(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analyze")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAnalyzeSSE_InvalidRunID(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analyze?repo=owner/repo&run_id=notanumber")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAnalyzeSSE_EmptyOwner(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analyze?repo=/repo")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAnalyzeSSE_EmptyRepoName(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analyze?repo=owner/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStaticFileServing(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should serve index.html or return OK for static files
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound)
}

// SSEEmitter tests

type mockFlusher struct {
	http.ResponseWriter
	flushed bool
}

func (m *mockFlusher) Flush() {
	m.flushed = true
}

func TestNewSSEEmitter_WithFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher := &mockFlusher{ResponseWriter: rec}

	emitter := NewSSEEmitter(flusher)

	assert.NotNil(t, emitter)
}

func TestNewSSEEmitter_WithoutFlusher(t *testing.T) {
	// Create a writer that doesn't implement Flusher
	w := &nonFlusherWriter{}

	emitter := NewSSEEmitter(w)

	assert.Nil(t, emitter)
}

type nonFlusherWriter struct{}

func (w *nonFlusherWriter) Header() http.Header        { return http.Header{} }
func (w *nonFlusherWriter) Write([]byte) (int, error)  { return 0, nil }
func (w *nonFlusherWriter) WriteHeader(statusCode int) {}

func TestSSEEmitter_Emit(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher := &mockFlusher{ResponseWriter: rec}
	emitter := NewSSEEmitter(flusher)

	ev := llm.ProgressEvent{
		Type:    "tool",
		Message: "Test message",
	}

	emitter.Emit(ev)

	body := rec.Body.String()
	assert.Contains(t, body, "data:")
	assert.Contains(t, body, "Test message")
	assert.True(t, flusher.flushed)
}

func TestSSEEmitter_Emit_TruncatesLongPreview(t *testing.T) {
	rec := httptest.NewRecorder()
	flusher := &mockFlusher{ResponseWriter: rec}
	emitter := NewSSEEmitter(flusher)

	// Create a preview longer than ssePreviewLimit (500)
	longPreview := strings.Repeat("a", 600)

	ev := llm.ProgressEvent{
		Type:    "tool",
		Preview: longPreview,
	}

	emitter.Emit(ev)

	body := rec.Body.String()
	// Should contain truncated preview (500 chars + "...")
	assert.Contains(t, body, "...")
	// Should not contain the full 600 character string
	assert.NotContains(t, body, strings.Repeat("a", 600))
}
