package trace

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArtifact(t *testing.T) {
	innerZip := buildInnerTraceZip(t)
	artifactZip := buildArtifactZip(t, map[string][]byte{
		"test-comment-replies-chromium/trace.zip":        innerZip,
		"test-comment-replies-chromium/error-context.md": []byte("## Page snapshot\nSign in form visible"),
	})

	traces, err := ParseArtifact(artifactZip)
	require.NoError(t, err)
	require.Len(t, traces, 1)

	tr := traces[0]
	assert.Equal(t, "test-comment-replies-chromium", tr.TestDir)
	assert.Contains(t, tr.ErrorContext, "Sign in form visible")
	require.NotEmpty(t, tr.Actions)

	for _, a := range tr.Actions {
		assert.Contains(t, []string{"Frame", "Page", "Locator"}, a.Class, "unexpected action class")
	}

	require.NotEmpty(t, tr.ConsoleErrors)
	assert.True(t, containsAny(tr.ConsoleErrors, "403", "Authorization"), "expected a 403 or Authorization console error")

	require.NotEmpty(t, tr.FailedRequests)
	assert.True(t, hasStatus(tr.FailedRequests, 403), "expected a 403 failed request")
}

func TestParseArtifactNoTraces(t *testing.T) {
	artifactZip := buildArtifactZip(t, map[string][]byte{
		"random-file.txt": []byte("hello"),
	})

	_, err := ParseArtifact(artifactZip)
	assert.Error(t, err)
}

func TestFormatSummary(t *testing.T) {
	traces := []TestTrace{
		{
			TestDir:      "test-login-chromium",
			ErrorContext: "Login page snapshot",
			Actions: []Action{
				{Class: "Frame", Method: "goto", Params: `{"url":"/login"}`},
				{Class: "Locator", Method: "fill", Params: `{"value":"user@test.com"}`},
			},
			ConsoleErrors:  []string{"Failed to load resource: 403"},
			FailedRequests: []Request{{Method: "GET", URL: "http://localhost/api/auth", Status: 403}},
		},
	}

	result := FormatSummary(traces)

	for _, expected := range []string{
		"## Test: test-login-chromium",
		"### Last Browser Actions",
		"Frame.goto",
		"Locator.fill",
		"### Console Errors",
		"403",
		"### Failed HTTP Requests",
		"GET http://localhost/api/auth → 403",
		"### Error Context",
		"Login page snapshot",
	} {
		assert.Contains(t, result, expected)
	}
}

func TestMaxActionsLimit(t *testing.T) {
	var lines []string
	lines = append(lines, `{"type":"context-options","version":8}`)
	for i := range 20 {
		line := fmt.Sprintf(`{"type":"before","class":"Frame","method":"goto","params":{"url":"/page","i":%d}}`, i)
		lines = append(lines, line)
	}

	innerZip := buildInnerTraceZipFromLines(t, strings.Join(lines, "\n"), "")
	artifactZip := buildArtifactZip(t, map[string][]byte{
		"test-dir/trace.zip": innerZip,
	})

	traces, err := ParseArtifact(artifactZip)
	require.NoError(t, err)
	assert.Len(t, traces[0].Actions, maxActions)
}

// --- helpers ---

func containsAny(items []string, substrs ...string) bool {
	for _, item := range items {
		for _, sub := range substrs {
			if strings.Contains(item, sub) {
				return true
			}
		}
	}
	return false
}

func hasStatus(reqs []Request, status int) bool {
	for _, r := range reqs {
		if r.Status == status {
			return true
		}
	}
	return false
}

func buildArtifactZip(t *testing.T, files map[string][]byte) []byte {
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

func buildInnerTraceZip(t *testing.T) []byte {
	t.Helper()
	traceLines := []string{
		`{"type":"context-options","version":8,"browserName":"chromium"}`,
		`{"type":"before","callId":"c1","class":"BrowserContext","method":"newPage","params":{}}`,
		`{"type":"before","callId":"c2","class":"Frame","method":"goto","params":{"url":"/ghost/#/signin"}}`,
		`{"type":"before","callId":"c3","class":"Frame","method":"fill","params":{"selector":"input[name=email]","value":"test@ghost.org"}}`,
		`{"type":"before","callId":"c4","class":"Frame","method":"fill","params":{"selector":"input[name=password]","value":"***"}}`,
		`{"type":"before","callId":"c5","class":"Frame","method":"click","params":{"selector":"button[type=submit]"}}`,
		`{"type":"before","callId":"c6","class":"Page","method":"waitForNavigation","params":{}}`,
		`{"type":"console","messageType":"error","text":"Failed to load resource: the server responded with a status of 403 (Forbidden)","time":1000}`,
		`{"type":"console","messageType":"error","text":"oz: Authorization failed","time":1001}`,
		`{"type":"console","messageType":"log","text":"some debug log","time":1002}`,
	}

	networkLines := []string{
		`{"type":"resource-snapshot","snapshot":{"request":{"method":"GET","url":"http://localhost/ghost/"},"response":{"status":200}}}`,
		`{"type":"resource-snapshot","snapshot":{"request":{"method":"GET","url":"http://localhost/ghost/api/admin/users/me/"},"response":{"status":403}}}`,
		`{"type":"resource-snapshot","snapshot":{"request":{"method":"GET","url":"http://localhost/ghost/api/admin/settings/"},"response":{"status":403}}}`,
		`{"type":"resource-snapshot","snapshot":{"request":{"method":"GET","url":"http://localhost/.ghost/activitypub/"},"response":{"status":404}}}`,
	}

	return buildInnerTraceZipFromLines(t,
		strings.Join(traceLines, "\n"),
		strings.Join(networkLines, "\n"),
	)
}

func buildInnerTraceZipFromLines(t *testing.T, traceContent, networkContent string) []byte {
	t.Helper()
	files := map[string][]byte{
		"test.trace": []byte(`{"type":"context-options","version":8}`),
	}
	if traceContent != "" {
		files["0-trace.trace"] = []byte(traceContent)
	}
	if networkContent != "" {
		files["0-trace.network"] = []byte(networkContent)
	}

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
