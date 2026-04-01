package llm

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureEmitter struct {
	events []ProgressEvent
}

func (c *captureEmitter) Emit(ev ProgressEvent) {
	c.events = append(c.events, ev)
}

func TestDebugEmitter_WritesJSONL(t *testing.T) {
	inner := &captureEmitter{}
	d, err := NewDebugEmitter(inner)
	require.NoError(t, err)
	defer func() { _ = os.Remove(d.Path()) }()

	d.Emit(ProgressEvent{Type: "system_prompt", Content: "You are an expert..."})
	d.Emit(ProgressEvent{Type: "tool", Step: 1, MaxStep: 30, Tool: "get_job_logs", Args: `{"job_id":123}`})
	d.Emit(ProgressEvent{Type: "tool_result", Step: 1, Tool: "get_job_logs", Chars: 80000, Content: "full log content here..."})
	d.Emit(ProgressEvent{Type: "model_response", Step: 2, Content: "Based on the logs, I can see..."})
	d.Close()

	// Verify inner emitter received all events
	assert.Len(t, inner.events, 4)
	assert.Equal(t, "system_prompt", inner.events[0].Type)
	assert.Equal(t, "tool", inner.events[1].Type)

	// Verify JSONL file
	f, err := os.Open(d.Path())
	require.NoError(t, err)
	defer f.Close()

	var lines []debugLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // handle large lines
	for scanner.Scan() {
		var line debugLine
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		lines = append(lines, line)
	}
	require.NoError(t, scanner.Err())

	assert.Len(t, lines, 4)

	// system_prompt
	assert.Equal(t, "system_prompt", lines[0].Type)
	assert.Equal(t, "You are an expert...", lines[0].Content)
	assert.NotEmpty(t, lines[0].Timestamp)

	// tool call
	assert.Equal(t, "tool", lines[1].Type)
	assert.Equal(t, "get_job_logs", lines[1].Tool)
	assert.Equal(t, `{"job_id":123}`, lines[1].Args)

	// tool result with full content
	assert.Equal(t, "tool_result", lines[2].Type)
	assert.Equal(t, 80000, lines[2].Chars)
	assert.Equal(t, "full log content here...", lines[2].Content)

	// model response
	assert.Equal(t, "model_response", lines[3].Type)
	assert.Equal(t, "Based on the logs, I can see...", lines[3].Content)
}

func TestDebugEmitter_Path(t *testing.T) {
	inner := &captureEmitter{}
	d, err := NewDebugEmitter(inner)
	require.NoError(t, err)
	defer func() { _ = os.Remove(d.Path()) }()
	defer d.Close()

	path := d.Path()
	assert.Contains(t, path, "heisenberg-debug-")
	assert.Contains(t, path, ".jsonl")

	// File should exist
	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestDebugEmitter_ForwardsToInner(t *testing.T) {
	inner := &captureEmitter{}
	d, err := NewDebugEmitter(inner)
	require.NoError(t, err)
	defer func() { _ = os.Remove(d.Path()) }()
	defer d.Close()

	d.Emit(ProgressEvent{Type: "info", Message: "test message"})
	assert.Len(t, inner.events, 1)
	assert.Equal(t, "info", inner.events[0].Type)
	assert.Equal(t, "test message", inner.events[0].Message)
}
