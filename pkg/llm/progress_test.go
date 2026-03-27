package llm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
)

func init() {
	// Disable colors in tests for predictable output.
	color.NoColor = true
}

func newTestEmitter() (*TextEmitter, *bytes.Buffer) {
	var buf bytes.Buffer
	return &TextEmitter{w: &buf, tty: false, noColor: true, verbose: true}, &buf
}

func newCompactTestEmitter() (*TextEmitter, *bytes.Buffer) {
	var buf bytes.Buffer
	return &TextEmitter{w: &buf, tty: false, noColor: true, verbose: false}, &buf
}

func TestEmit_Step(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "step", Step: 1, MaxStep: 10, Message: "Calling model..."})
	assert.Contains(t, buf.String(), "Calling model...")
}

func TestEmit_Tool(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 1, MaxStep: 10, Tool: "get_job_logs"})
	out := buf.String()
	assert.Contains(t, out, "✓")
	assert.Contains(t, out, "get_job_logs")
	assert.Contains(t, out, "1/10")
}

func TestEmit_ToolWithArgs(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 2, MaxStep: 10, Tool: "get_artifact", Args: `{"artifact_name":"html-report"}`})
	out := buf.String()
	assert.Contains(t, out, "get_artifact")
	assert.Contains(t, out, "artifact: html-report")
	assert.Contains(t, out, "2/10")
}

func TestEmit_ToolDoneSkipsArgs(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 3, MaxStep: 10, Tool: "done", Args: `{"category":"diagnosis","confidence":85}`})
	out := buf.String()
	assert.Contains(t, out, "done")
	assert.NotContains(t, out, "category")
	assert.NotContains(t, out, "confidence")
}

func TestEmit_Result(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "result", ModelMs: 3200, Tokens: 12847, Chars: 80000})
	out := buf.String()
	assert.Contains(t, out, "↳")
	assert.Contains(t, out, "model 3.2s, 12,847 tok")
	assert.Contains(t, out, "result 80,000 chars")
}

func TestEmit_ResultModelOnly(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "result", ModelMs: 2800, Tokens: 19105})
	out := buf.String()
	assert.Contains(t, out, "model 2.8s, 19,105 tok")
	assert.NotContains(t, out, "result")
}

func TestEmit_Info(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "info", Message: "Analyzing run 123..."})
	assert.Contains(t, buf.String(), "Analyzing run 123...")
}

func TestEmit_Error(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "error", Message: "something failed"})
	assert.Contains(t, buf.String(), "Error: something failed")
}

func TestEmit_Close(t *testing.T) {
	e, _ := newTestEmitter()
	e.Close() // should not panic
}

func TestEmit_ToolCounterAlignment(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 1, MaxStep: 10, Tool: "get_job_logs"})
	line1 := buf.String()
	buf.Reset()

	e.Emit(ProgressEvent{Type: "tool", Step: 2, MaxStep: 10, Tool: "done"})
	line2 := buf.String()

	// Both lines should have their counters at approximately the same position
	idx1 := bytes.Index([]byte(line1), []byte("1/10"))
	idx2 := bytes.Index([]byte(line2), []byte("2/10"))
	assert.InDelta(t, idx1, idx2, 1, "counters should be aligned")
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int
		want string
	}{
		{0, "0ms"},
		{500, "500ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{3200, "3.2s"},
		{10000, "10.0s"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatDuration(tt.ms), "formatDuration(%d)", tt.ms)
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{12847, "12,847"},
		{80000, "80,000"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatNumber(tt.n), "formatNumber(%d)", tt.n)
	}
}

func TestFormatStats(t *testing.T) {
	tests := []struct {
		name string
		ev   ProgressEvent
		want string
	}{
		{
			name: "model and result",
			ev:   ProgressEvent{ModelMs: 3200, Tokens: 12847, Chars: 80000},
			want: "model 3.2s, 12,847 tok · result 80,000 chars",
		},
		{
			name: "model only",
			ev:   ProgressEvent{ModelMs: 2800, Tokens: 19105},
			want: "model 2.8s, 19,105 tok",
		},
		{
			name: "result only",
			ev:   ProgressEvent{Chars: 856},
			want: "result 856 chars",
		},
		{
			name: "empty",
			ev:   ProgressEvent{},
			want: "ok",
		},
		{
			name: "model time only",
			ev:   ProgressEvent{ModelMs: 500},
			want: "model 500ms",
		},
		{
			name: "tokens only",
			ev:   ProgressEvent{Tokens: 5000},
			want: "model, 5,000 tok",
		},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatStats(tt.ev), tt.name)
	}
}

func TestHumanizeArgs(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "single string arg",
			json: `{"job_id":"62201770461"}`,
			want: "(job_id: 62201770461)",
		},
		{
			name: "integer float",
			json: `{"confidence":85}`,
			want: "(confidence: 85)",
		},
		{
			name: "fractional float",
			json: `{"confidence":72.8}`,
			want: "(confidence: 72.8)",
		},
		{
			name: "shortens artifact_name",
			json: `{"artifact_name":"test-results-6"}`,
			want: "(artifact: test-results-6)",
		},
		{
			name: "shortens sensitivity",
			json: `{"missing_information_sensitivity":"high"}`,
			want: "(sensitivity: high)",
		},
		{
			name: "empty args",
			json: `{}`,
			want: "",
		},
		{
			name: "invalid json",
			json: `not-json`,
			want: "not-json",
		},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, humanizeArgs(tt.json), tt.name)
	}
}

func TestHumanizeArgs_DeterministicOrder(t *testing.T) {
	// Multiple keys should always produce the same output (sorted alphabetically)
	json := `{"zebra":"last","alpha":"first","middle":"center"}`
	expected := "(alpha: first, middle: center, zebra: last)"

	// Run multiple times to verify deterministic ordering
	for i := 0; i < 10; i++ {
		result := humanizeArgs(json)
		assert.Equal(t, expected, result, "iteration %d should produce same result", i)
	}
}

func TestNewTextEmitter_NoColor(t *testing.T) {
	var buf bytes.Buffer
	e := NewTextEmitter(&buf, false)
	// Non-TTY writer should have noColor=true
	assert.True(t, e.noColor, "non-TTY should have noColor=true")
}

func TestToolPhase(t *testing.T) {
	tests := []struct {
		tool string
		want string
	}{
		{"list_jobs", "Listing jobs"},
		{"get_job_logs", "Reading logs"},
		{"get_artifact", "Fetching artifacts"},
		{"get_test_traces", "Analyzing traces"},
		{"get_repo_file", "Reading source"},
		{"get_workflow_file", "Reading source"},
		{"done", "Finalizing"},
		{"unknown_tool", "Analyzing"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, toolPhase(tt.tool), "toolPhase(%q)", tt.tool)
	}
}

func TestCompactMode_ToolShowsPhaseAndCounter(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 3, MaxStep: 30, Tool: "get_repo_file"})
	assert.Equal(t, "  Reading source     3/30\n", buf.String())
}

func TestCompactMode_StepIsQuietOnNonTTY(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "step", Step: 1, MaxStep: 30, Message: "Calling model..."})
	assert.Empty(t, buf.String(), "compact spinner is a no-op on non-TTY")
}

func TestCompactMode_ResultSuppressed(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "result", ModelMs: 3200, Tokens: 12847})
	assert.Empty(t, buf.String(), "compact mode should suppress result events")
}

func TestCompactMode_CloseSuccess(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 9, MaxStep: 30, Tool: "done"})
	buf.Reset()
	e.Close()
	assert.Equal(t, "  ✓  Used 9/30 iterations\n", buf.String())
}

func TestCompactMode_CloseError(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 2, MaxStep: 30, Tool: "get_job_logs"})
	e.MarkFailed()
	buf.Reset()
	e.Close()
	assert.Equal(t, "  ✗  Stopped at 2/30 iterations\n", buf.String())
}

func TestCompactMode_CloseError_AfterStepWithoutTool(t *testing.T) {
	e, buf := newCompactTestEmitter()
	// Step 1 + tool 1 complete
	e.Emit(ProgressEvent{Type: "step", Step: 1, MaxStep: 30, Message: "Calling model..."})
	e.Emit(ProgressEvent{Type: "tool", Step: 1, MaxStep: 30, Tool: "get_artifact"})
	// Step 2 + tool 2 complete
	e.Emit(ProgressEvent{Type: "step", Step: 2, MaxStep: 30, Message: "Calling model..."})
	e.Emit(ProgressEvent{Type: "tool", Step: 2, MaxStep: 30, Tool: "get_job_logs"})
	// Step 3 starts but model returns error — no tool event
	e.Emit(ProgressEvent{Type: "step", Step: 3, MaxStep: 30, Message: "Calling model..."})
	e.MarkFailed()
	buf.Reset()
	e.Close()
	assert.Equal(t, "  ✗  Stopped at 3/30 iterations\n", buf.String())
}

func TestCompactMode_InfoStillPrints(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "info", Message: "Analyzing run 123..."})
	assert.Contains(t, buf.String(), "Analyzing run 123...")
}

func TestCompactMode_ErrorStillPrints(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "error", Message: "something failed"})
	assert.Contains(t, buf.String(), "Error: something failed")
}

func TestCompactProgressLine(t *testing.T) {
	e, _ := newCompactTestEmitter()

	// Initial state (before any tool) — phase padded to fixed width
	e.lastStep = 0
	e.lastMax = 30
	e.lastTool = ""
	assert.Equal(t, "  Analyzing          0/30", e.compactProgressLine())

	// Mid-progress — different phase, same alignment
	e.lastStep = 3
	e.lastTool = "get_job_logs"
	assert.Equal(t, "  Reading logs       3/30", e.compactProgressLine())

	// Longer phase — still aligned
	e.lastStep = 2
	e.lastTool = "get_artifact"
	assert.Equal(t, "  Fetching artifacts 2/30", e.compactProgressLine())

	// Done tool
	e.lastStep = 9
	e.lastTool = "done"
	assert.Equal(t, "  Finalizing         9/30", e.compactProgressLine())
}

func TestCompactProgressLine_WithHint(t *testing.T) {
	e, _ := newCompactTestEmitter()
	e.lastStep = 2
	e.lastMax = 30
	e.lastTool = "get_job_logs"

	assert.Equal(t, "  Reading logs       3/30  (calling model...)", e.compactProgressLineWithHint(3, "calling model..."))
	assert.Equal(t, "  Reading logs       3/30", e.compactProgressLineWithHint(3, ""))
}

func TestCompactProgressLine_ZeroMaxStep(t *testing.T) {
	e, _ := newCompactTestEmitter()
	e.lastStep = 0
	e.lastMax = 0

	// Should not panic on division by zero
	assert.NotPanics(t, func() {
		line := e.compactProgressLine()
		assert.Contains(t, line, "0/0")
	})
}

func TestVerboseClose_WithProgress_IsNoOp(t *testing.T) {
	e, buf := newTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 5, MaxStep: 30, Tool: "get_job_logs"})
	buf.Reset()
	e.Close()
	assert.Empty(t, buf.String(), "verbose Close should not print iteration summary")
}

func TestCompactMode_ToolNonTTY_HasNewline(t *testing.T) {
	e, buf := newCompactTestEmitter()
	e.Emit(ProgressEvent{Type: "tool", Step: 1, MaxStep: 10, Tool: "get_job_logs"})
	out := buf.String()
	assert.True(t, strings.HasSuffix(out, "\n"), "non-TTY compact tool output should end with newline")
	assert.NotContains(t, out, "\033", "non-TTY output should not contain ANSI escapes")
}
