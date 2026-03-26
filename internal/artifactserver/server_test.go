package artifactserver

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerStartAndStop(t *testing.T) {
	content := []byte("<html><body>test</body></html>")
	srv, err := Start(content, "index.html")
	require.NoError(t, err)
	require.NotNil(t, srv)
	defer srv.Stop()

	// Test URL generation
	url := srv.URL("index.html")
	assert.Contains(t, url, "http://127.0.0.1:")
	assert.Contains(t, url, "/index.html")

	// Test serving content
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, content, body)
}

func TestServerServesMultipleFiles(t *testing.T) {
	content := []byte("test content")
	srv, err := Start(content, "report.html")
	require.NoError(t, err)
	defer srv.Stop()

	// File should be accessible
	resp, err := http.Get(srv.URL("report.html"))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Non-existent file should 404
	resp2, err := http.Get(srv.URL("nonexistent.html"))
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}
