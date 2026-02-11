package github

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyArtifact(t *testing.T) {
	tests := []struct {
		name string
		want ArtifactType
	}{
		{"html-report--attempt-1", ArtifactHTML},
		{"html-report--attempt-2", ArtifactHTML},
		{"playwright-report", ArtifactHTML},
		{"test-results.json", ArtifactJSON},
		{"playwright_report.json", ArtifactJSON},
		{"blob-report-1", ArtifactBlob},
		{"blob-report-2", ArtifactBlob},
		{"blob-report-10", ArtifactBlob},
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

func TestClassifyAll(t *testing.T) {
	artifacts := []Artifact{
		{ID: 1, Name: "html-report--attempt-1", Expired: false},
		{ID: 2, Name: "blob-report-1", Expired: false},
		{ID: 3, Name: "blob-report-2", Expired: false},
		{ID: 4, Name: "test-results.json", Expired: false},
		{ID: 5, Name: "expired-html-report", Expired: true},
		{ID: 6, Name: "coverage", Expired: false}, // unknown type
	}

	c := classifyAll(artifacts)

	assert.Len(t, c.html, 1)
	assert.Equal(t, int64(1), c.html[0].ID)

	assert.Len(t, c.blob, 2)
	assert.Equal(t, int64(2), c.blob[0].ID)
	assert.Equal(t, int64(3), c.blob[1].ID)

	assert.Len(t, c.json, 1)
	assert.Equal(t, int64(4), c.json[0].ID)
}

func TestClassifyAllEmpty(t *testing.T) {
	c := classifyAll(nil)
	assert.Empty(t, c.html)
	assert.Empty(t, c.json)
	assert.Empty(t, c.blob)
}

func TestReadZipEntry(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"test.txt": []byte("hello world"),
	})
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	require.NoError(t, err)

	content := readZipEntry(reader.File[0])
	assert.Equal(t, []byte("hello world"), content)
}

func TestReadZipEntryEmpty(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"empty.txt": {},
	})
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	require.NoError(t, err)

	content := readZipEntry(reader.File[0])
	assert.Nil(t, content)
}

func TestExtractFirstFileHTML(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"other.txt":   []byte("other content"),
		"report.html": []byte("<html>test</html>"),
	})

	content, err := extractFirstFile(zipData)
	require.NoError(t, err)
	assert.Equal(t, []byte("<html>test</html>"), content)
}

func TestExtractFirstFileJSON(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"other.txt":   []byte("other content"),
		"report.json": []byte(`{"test": true}`),
	})

	content, err := extractFirstFile(zipData)
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"test": true}`), content)
}

func TestExtractFirstFileFallback(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"data.txt": []byte("fallback content"),
	})

	content, err := extractFirstFile(zipData)
	require.NoError(t, err)
	assert.Equal(t, []byte("fallback content"), content)
}

func TestExtractFirstFileEmpty(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{})

	_, err := extractFirstFile(zipData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no files found")
}

func TestExtractFirstFileInvalidZip(t *testing.T) {
	_, err := extractFirstFile([]byte("not a zip"))
	assert.Error(t, err)
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

func TestFetchFirstContent(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"report.html": []byte("<html>test</html>"),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	artifacts := []Artifact{{ID: 1, Name: "html-report"}}

	result := c.fetchFirstContent(context.Background(), "owner", "repo", artifacts, ArtifactHTML)
	require.NotNil(t, result)
	assert.Equal(t, ArtifactHTML, result.Type)
	assert.Equal(t, []byte("<html>test</html>"), result.Content)
}

func TestFetchFirstContentEmpty(t *testing.T) {
	c := &Client{}
	result := c.fetchFirstContent(context.Background(), "owner", "repo", nil, ArtifactHTML)
	assert.Nil(t, result)
}

func TestFetchFirstContentAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	artifacts := []Artifact{{ID: 1, Name: "html-report"}}

	result := c.fetchFirstContent(context.Background(), "owner", "repo", artifacts, ArtifactHTML)
	assert.Nil(t, result)
}

func TestFetchBlobs(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{"data.bin": []byte("blob data")})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	artifacts := []Artifact{
		{ID: 1, Name: "blob-report-1"},
		{ID: 2, Name: "blob-report-2"},
	}

	result := c.fetchBlobs(context.Background(), "owner", "repo", artifacts)
	require.NotNil(t, result)
	assert.Equal(t, ArtifactBlob, result.Type)
	assert.Len(t, result.Blobs, 2)
}

func TestFetchBlobsEmpty(t *testing.T) {
	c := &Client{}
	result := c.fetchBlobs(context.Background(), "owner", "repo", nil)
	assert.Nil(t, result)
}

func TestFetchBlobsAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	artifacts := []Artifact{{ID: 1, Name: "blob-report"}}

	result := c.fetchBlobs(context.Background(), "owner", "repo", artifacts)
	assert.Nil(t, result)
}

func TestCheckArtifacts_AllExpired(t *testing.T) {
	artifacts := []Artifact{
		{ID: 1, Name: "html-report", Expired: true},
		{ID: 2, Name: "blob-report", Expired: true},
	}

	status := CheckArtifacts(artifacts)

	assert.Equal(t, 2, status.Total)
	assert.Equal(t, 2, status.Expired)
	assert.Equal(t, 0, status.Available)
	assert.False(t, status.HasUsable)
	assert.True(t, status.AllExpired)
}

func TestCheckArtifacts_SomeExpired(t *testing.T) {
	artifacts := []Artifact{
		{ID: 1, Name: "html-report", Expired: true},
		{ID: 2, Name: "blob-report", Expired: false},
	}

	status := CheckArtifacts(artifacts)

	assert.Equal(t, 2, status.Total)
	assert.Equal(t, 1, status.Expired)
	assert.Equal(t, 1, status.Available)
	assert.True(t, status.HasUsable)
	assert.False(t, status.AllExpired)
}

func TestCheckArtifacts_NoneExpired(t *testing.T) {
	artifacts := []Artifact{
		{ID: 1, Name: "html-report", Expired: false},
		{ID: 2, Name: "blob-report", Expired: false},
	}

	status := CheckArtifacts(artifacts)

	assert.Equal(t, 2, status.Total)
	assert.Equal(t, 0, status.Expired)
	assert.Equal(t, 2, status.Available)
	assert.True(t, status.HasUsable)
	assert.False(t, status.AllExpired)
}

func TestCheckArtifacts_Empty(t *testing.T) {
	status := CheckArtifacts(nil)

	assert.Equal(t, 0, status.Total)
	assert.Equal(t, 0, status.Expired)
	assert.Equal(t, 0, status.Available)
	assert.False(t, status.HasUsable)
	assert.False(t, status.AllExpired)
}

func TestSelectAndFetch(t *testing.T) {
	zipData := buildZip(t, map[string][]byte{
		"report.html": []byte("<html>test</html>"),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	artifacts := []Artifact{
		{ID: 1, Name: "html-report", Expired: false},
		{ID: 2, Name: "blob-report-1", Expired: false},
	}

	result := c.selectAndFetch(context.Background(), "owner", "repo", artifacts)
	require.NotNil(t, result)
	assert.Equal(t, ArtifactHTML, result.Type)
}
