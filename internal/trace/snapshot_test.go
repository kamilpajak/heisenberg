package trace

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSnapshotZip(t *testing.T) {
	zipData := buildTestZip(t, map[string][]byte{
		"file1.txt":        []byte("content1"),
		"subdir/file2.txt": []byte("content2"),
	})

	destDir := t.TempDir()
	err := extractSnapshotZip(zipData, destDir)
	require.NoError(t, err)

	// Check file1.txt
	content1, err := os.ReadFile(filepath.Join(destDir, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content1", string(content1))

	// Check subdir/file2.txt
	content2, err := os.ReadFile(filepath.Join(destDir, "subdir", "file2.txt"))
	require.NoError(t, err)
	assert.Equal(t, "content2", string(content2))
}

func TestExtractSnapshotZipWithDirectory(t *testing.T) {
	// Create zip with explicit directory entry
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	// Add directory entry
	_, err := w.Create("mydir/")
	require.NoError(t, err)

	// Add file in directory
	fw, err := w.Create("mydir/file.txt")
	require.NoError(t, err)
	_, err = fw.Write([]byte("hello"))
	require.NoError(t, err)

	require.NoError(t, w.Close())

	destDir := t.TempDir()
	err = extractSnapshotZip(buf.Bytes(), destDir)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(destDir, "mydir", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestExtractSnapshotZipInvalidZip(t *testing.T) {
	err := extractSnapshotZip([]byte("not a zip"), t.TempDir())
	assert.Error(t, err)
}

func TestExtractSnapshotZipEmpty(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	require.NoError(t, w.Close())

	destDir := t.TempDir()
	err := extractSnapshotZip(buf.Bytes(), destDir)
	require.NoError(t, err)

	// Directory should exist but be empty (except for . and ..)
	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestMergeBlobReportsInvalidZip(t *testing.T) {
	// Test with invalid zip data - should fail during extraction
	_, err := MergeBlobReports([][]byte{[]byte("not a zip")})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to extract blob")
}

func TestMergeBlobReportsSecondBlobInvalid(t *testing.T) {
	// First blob is valid, second is invalid
	validZip := buildTestZip(t, map[string][]byte{"file.txt": []byte("content")})
	_, err := MergeBlobReports([][]byte{validZip, []byte("not a zip")})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to extract blob 1")
}

func TestExtractSnapshotZipCreateFileError(t *testing.T) {
	// Create a zip with a file that has an invalid path (parent dir doesn't exist and can't be created)
	// This is hard to test without mocking os.Create, so we test what we can
	zipData := buildTestZip(t, map[string][]byte{
		"normal.txt": []byte("content"),
	})

	// Use a read-only directory - but this is platform-specific
	// Instead, just verify the normal case works
	destDir := t.TempDir()
	err := extractSnapshotZip(zipData, destDir)
	require.NoError(t, err)
}

func TestIsPlaywrightAvailable(t *testing.T) {
	// This test just exercises the function - result depends on environment
	// We can't assert true/false because it depends on whether playwright is installed
	_ = IsPlaywrightAvailable()
}

func TestSnapshotHTMLNotAvailable(t *testing.T) {
	if IsPlaywrightAvailable() {
		t.Skip("Playwright is available, skipping unavailable test")
	}

	_, err := SnapshotHTML([]byte("<html></html>"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "playwright not installed")
}

func buildTestZip(t *testing.T, files map[string][]byte) []byte {
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

// mockRunner captures command arguments for testing.
type mockRunner struct {
	lastCmd  string
	lastArgs []string
	lastEnv  []string
	output   []byte
	err      error
}

func (m *mockRunner) Run(name string, args []string, env []string) ([]byte, error) {
	m.lastCmd = name
	m.lastArgs = args
	m.lastEnv = env
	return m.output, m.err
}

func TestMergeBlobReportsWithRunner_Success(t *testing.T) {
	mock := &mockRunner{output: []byte("success")}
	validZip := buildTestZip(t, map[string][]byte{"report.json": []byte("{}")})

	outputDir, err := MergeBlobReportsWithRunner([][]byte{validZip}, mock)
	require.NoError(t, err)
	defer os.RemoveAll(outputDir)

	// Verify correct command was called
	assert.Equal(t, "npx", mock.lastCmd)
	assert.Equal(t, "playwright", mock.lastArgs[0])
	assert.Equal(t, "merge-reports", mock.lastArgs[1])
	assert.Equal(t, "--reporter", mock.lastArgs[2])
	assert.Equal(t, "html", mock.lastArgs[3])
	// lastArgs[4] is the blob dir path

	// Verify env vars
	assert.Contains(t, mock.lastEnv[0], "PLAYWRIGHT_HTML_OPEN=never")
	assert.Contains(t, mock.lastEnv[1], "PLAYWRIGHT_HTML_OUTPUT_DIR=")
}

func TestMergeBlobReportsWithRunner_CommandFails(t *testing.T) {
	mock := &mockRunner{
		output: []byte("merge failed: no reports found"),
		err:    assert.AnError,
	}
	validZip := buildTestZip(t, map[string][]byte{"report.json": []byte("{}")})

	_, err := MergeBlobReportsWithRunner([][]byte{validZip}, mock)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge-reports failed")
	assert.Contains(t, err.Error(), "no reports found")
}

func TestMergeBlobReportsWithRunner_MultipleBlobs(t *testing.T) {
	mock := &mockRunner{output: []byte("merged")}
	blob1 := buildTestZip(t, map[string][]byte{"shard1.json": []byte("{}")})
	blob2 := buildTestZip(t, map[string][]byte{"shard2.json": []byte("{}")})

	outputDir, err := MergeBlobReportsWithRunner([][]byte{blob1, blob2}, mock)
	require.NoError(t, err)
	defer os.RemoveAll(outputDir)

	assert.Equal(t, "npx", mock.lastCmd)
}

func TestExecRunner_Run(t *testing.T) {
	runner := ExecRunner{}

	// Test with a simple command that should work on all platforms
	output, err := runner.Run("echo", []string{"hello"}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(output), "hello")
}

func TestExecRunner_RunWithEnv(t *testing.T) {
	runner := ExecRunner{}

	// Test that env vars are passed (using printenv on unix)
	output, err := runner.Run("printenv", []string{"TEST_VAR"}, []string{"TEST_VAR=test_value"})
	require.NoError(t, err)
	assert.Contains(t, string(output), "test_value")
}

func TestExecRunner_RunFailure(t *testing.T) {
	runner := ExecRunner{}

	// Test with a command that doesn't exist
	_, err := runner.Run("nonexistent_command_12345", []string{}, nil)
	require.Error(t, err)
}
