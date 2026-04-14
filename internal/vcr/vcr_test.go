package vcr

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordAndReplay(t *testing.T) {
	// Start a test server that returns predictable responses.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("X-Custom", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"hello"}`))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "test-cassette")

	// Record
	rec, err := New(cassettePath, ModeRecord)
	require.NoError(t, err)

	client := rec.HTTPClient()
	resp, err := client.Get(srv.URL + "/api/test?key=secret123")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"result":"hello"}`, string(body))
	assert.Equal(t, 1, callCount)

	require.NoError(t, rec.Stop())

	// Verify cassette file exists
	_, err = os.Stat(cassettePath + ".json.gz")
	require.NoError(t, err)

	// Replay
	rec2, err := New(cassettePath, ModeReplay)
	require.NoError(t, err)

	client2 := rec2.HTTPClient()
	resp2, err := client2.Get(srv.URL + "/api/test?key=different-key")
	require.NoError(t, err)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, `{"result":"hello"}`, string(body2))
	// Server was NOT called again — still 1.
	assert.Equal(t, 1, callCount)

	require.NoError(t, rec2.Stop())
}

func TestScrub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cassettePath := filepath.Join(t.TempDir(), "scrub-test")

	scrub := WithScrub(func(ix *Interaction) {
		ix.Request.Header.Del("Authorization")
	})

	rec, err := New(cassettePath, ModeRecord, scrub)
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", srv.URL+"/secret", nil)
	req.Header.Set("Authorization", "Bearer top-secret")
	resp, err := rec.HTTPClient().Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.NoError(t, rec.Stop())

	// Load cassette and verify auth was scrubbed.
	rec2, err := New(cassettePath, ModeReplay)
	require.NoError(t, err)

	assert.Empty(t, rec2.cassette.Interactions[0].Request.Header.Get("Authorization"))
}

func TestReplayNoMatch(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "empty")

	// Record an empty cassette.
	rec, err := New(cassettePath, ModeRecord)
	require.NoError(t, err)
	require.NoError(t, rec.Stop())

	// Replay — no interactions to match.
	rec2, err := New(cassettePath, ModeReplay)
	require.NoError(t, err)

	_, err = rec2.HTTPClient().Get("http://example.com/missing")
	assert.ErrorContains(t, err, "no matching interaction")
}

func TestReplayMissingCassette(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "nonexistent"), ModeReplay)
	assert.Error(t, err)
}

func TestDefaultMatch_StripKey(t *testing.T) {
	tests := []struct {
		name    string
		reqURL  string
		recURL  string
		method  string
		matches bool
	}{
		{
			name:    "same URL",
			reqURL:  "https://api.example.com/v1/models",
			recURL:  "https://api.example.com/v1/models",
			method:  "GET",
			matches: true,
		},
		{
			name:    "different API key",
			reqURL:  "https://api.example.com/v1?key=abc123",
			recURL:  "https://api.example.com/v1?key=REDACTED",
			method:  "GET",
			matches: true,
		},
		{
			name:    "key in middle of query",
			reqURL:  "https://api.example.com/v1?key=abc&alt=json",
			recURL:  "https://api.example.com/v1?key=xyz&alt=json",
			method:  "GET",
			matches: true,
		},
		{
			name:    "different method",
			reqURL:  "https://api.example.com/v1",
			recURL:  "https://api.example.com/v1",
			method:  "POST",
			matches: false, // req is GET, recorded is POST
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tt.reqURL, nil)
			rec := Request{Method: tt.method, URL: tt.recURL}
			assert.Equal(t, tt.matches, DefaultMatch(req, rec))
		})
	}
}
