package trace

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestParseArtifact(t *testing.T) {
	// Build a fake artifact ZIP matching the GitHub structure:
	// test-comment-replies-chromium/trace.zip  (inner ZIP with trace files)
	// test-comment-replies-chromium/error-context.md
	innerZip := buildInnerTraceZip(t)
	artifactZip := buildArtifactZip(t, map[string][]byte{
		"test-comment-replies-chromium/trace.zip":         innerZip,
		"test-comment-replies-chromium/error-context.md":  []byte("## Page snapshot\nSign in form visible"),
	})

	traces, err := ParseArtifact(artifactZip)
	if err != nil {
		t.Fatalf("ParseArtifact: %v", err)
	}

	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}

	tr := traces[0]

	if tr.TestDir != "test-comment-replies-chromium" {
		t.Errorf("TestDir = %q, want %q", tr.TestDir, "test-comment-replies-chromium")
	}

	if !strings.Contains(tr.ErrorContext, "Sign in form visible") {
		t.Errorf("ErrorContext missing expected content, got: %s", tr.ErrorContext)
	}

	if len(tr.Actions) == 0 {
		t.Error("expected actions, got none")
	}

	// Verify actions are from expected classes
	for _, a := range tr.Actions {
		switch a.Class {
		case "Frame", "Page", "Locator":
			// ok
		default:
			t.Errorf("unexpected action class: %s", a.Class)
		}
	}

	if len(tr.ConsoleErrors) == 0 {
		t.Error("expected console errors, got none")
	}

	foundAuthError := false
	for _, msg := range tr.ConsoleErrors {
		if strings.Contains(msg, "403") || strings.Contains(msg, "Authorization") {
			foundAuthError = true
			break
		}
	}
	if !foundAuthError {
		t.Error("expected a 403 or Authorization console error")
	}

	if len(tr.FailedRequests) == 0 {
		t.Error("expected failed requests, got none")
	}

	found403 := false
	for _, r := range tr.FailedRequests {
		if r.Status == 403 {
			found403 = true
			break
		}
	}
	if !found403 {
		t.Error("expected a 403 failed request")
	}
}

func TestParseArtifactNoTraces(t *testing.T) {
	// ZIP with no trace directories
	artifactZip := buildArtifactZip(t, map[string][]byte{
		"random-file.txt": []byte("hello"),
	})

	_, err := ParseArtifact(artifactZip)
	if err == nil {
		t.Fatal("expected error for artifact with no traces")
	}
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

	checks := []string{
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
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("FormatSummary missing %q", check)
		}
	}
}

func TestMaxActionsLimit(t *testing.T) {
	// Build trace with more than maxActions "before" events
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
	if err != nil {
		t.Fatalf("ParseArtifact: %v", err)
	}

	if len(traces[0].Actions) != maxActions {
		t.Errorf("expected %d actions (max), got %d", maxActions, len(traces[0].Actions))
	}
}

// --- helpers ---

func buildArtifactZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

