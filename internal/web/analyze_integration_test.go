//go:build integration

package web

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kamilpajak/heisenberg/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseEvent is the parsed form of a single SSE data line.
type sseEvent struct {
	Type     string              `json:"type"`
	Step     int                 `json:"step,omitempty"`
	MaxStep  int                 `json:"max,omitempty"`
	Message  string              `json:"message,omitempty"`
	Tool     string              `json:"tool,omitempty"`
	Args     string              `json:"args,omitempty"`
	Preview  string              `json:"preview,omitempty"`
	Analysis *llm.AnalysisResult `json:"analysis,omitempty"`
}

func requireEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("GITHUB_TOKEN not set")
	}
	if os.Getenv("GOOGLE_API_KEY") == "" {
		t.Skip("GOOGLE_API_KEY not set")
	}
}

func readSSEEvents(t *testing.T, resp *http.Response) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var ev sseEvent
		require.NoError(t, json.Unmarshal([]byte(payload), &ev), "raw: %s", payload)
		events = append(events, ev)
	}
	require.NoError(t, scanner.Err())
	return events
}

func TestAnalyzeSSE_Diagnosis(t *testing.T) {
	requireEnv(t)

	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	// TryGhost/Ghost — 1 Playwright failure (comment-replies.test.ts)
	resp, err := http.Get(srv.URL + "/api/analyze?repo=TryGhost/Ghost&run_id=21588220201")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	events := readSSEEvents(t, resp)
	require.NotEmpty(t, events)

	var hasStep, hasTool, hasDone bool
	var doneEvent sseEvent
	for _, ev := range events {
		switch ev.Type {
		case "step":
			hasStep = true
			assert.Positive(t, ev.Step)
			assert.Positive(t, ev.MaxStep)
		case "tool":
			hasTool = true
			assert.NotEmpty(t, ev.Tool)
		case "done":
			hasDone = true
			doneEvent = ev
		case "error":
			t.Fatalf("received error event: %s", ev.Message)
		}
	}

	assert.True(t, hasStep, "expected at least one step event")
	assert.True(t, hasTool, "expected at least one tool event")
	require.True(t, hasDone, "expected a done event")

	a := doneEvent.Analysis
	require.NotNil(t, a)
	assert.Equal(t, llm.CategoryDiagnosis, a.Category)
	assert.InDelta(t, 50, a.Confidence, 50, "confidence should be 1-100")
	assert.Contains(t, []string{"high", "medium", "low"}, a.Sensitivity)
	assert.NotEmpty(t, a.Text)
}

func TestAnalyzeSSE_PassingRun(t *testing.T) {
	requireEnv(t)

	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	// carbon-design-system/carbon — 0 failures, 10 skipped
	// LLM may classify as "no_failures" or "diagnosis" (flagging skipped tests).
	resp, err := http.Get(srv.URL + "/api/analyze?repo=carbon-design-system/carbon&run_id=21585598963")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	events := readSSEEvents(t, resp)

	var doneEvent *sseEvent
	for i, ev := range events {
		require.NotEqual(t, "error", ev.Type, "received error event: %s", ev.Message)
		if ev.Type == "done" {
			doneEvent = &events[i]
		}
	}
	require.NotNil(t, doneEvent, "expected a done event")

	a := doneEvent.Analysis
	require.NotNil(t, a)
	assert.Contains(t, []string{llm.CategoryDiagnosis, llm.CategoryNoFailures, llm.CategoryNotSupported}, a.Category)
	assert.NotEmpty(t, a.Text)
	t.Logf("category=%s confidence=%d sensitivity=%s", a.Category, a.Confidence, a.Sensitivity)
}
