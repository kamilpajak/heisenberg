package llm

import (
	"bytes"
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
	return &TextEmitter{w: &buf, tty: false}, &buf
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
