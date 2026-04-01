package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// debugLine is the JSONL structure written to the debug log file.
type debugLine struct {
	Timestamp string `json:"ts"`
	Type      string `json:"type"`
	Step      int    `json:"step,omitempty"`
	MaxStep   int    `json:"max,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Args      string `json:"args,omitempty"`
	Message   string `json:"message,omitempty"`
	Content   string `json:"content,omitempty"`
	ModelMs   int    `json:"model_ms,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
	Chars     int    `json:"chars,omitempty"`
	ToolMs    int    `json:"tool_ms,omitempty"`
}

// DebugEmitter wraps a ProgressEmitter and writes all events as JSONL to a file.
// It forwards every event to the inner emitter for normal display, and additionally
// writes the full event (including Content) to the debug log.
type DebugEmitter struct {
	inner ProgressEmitter
	file  *os.File
	enc   *json.Encoder
	mu    sync.Mutex
}

// NewDebugEmitter creates a DebugEmitter that wraps the given inner emitter.
// It creates a temporary JSONL file for debug output.
func NewDebugEmitter(inner ProgressEmitter) (*DebugEmitter, error) {
	f, err := os.CreateTemp("", "heisenberg-debug-*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("creating debug log: %w", err)
	}
	return &DebugEmitter{
		inner: inner,
		file:  f,
		enc:   json.NewEncoder(f),
	}, nil
}

// Emit forwards the event to the inner emitter and writes it to the debug log.
func (d *DebugEmitter) Emit(ev ProgressEvent) {
	d.inner.Emit(ev)

	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.enc.Encode(debugLine{
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Type:      ev.Type,
		Step:      ev.Step,
		MaxStep:   ev.MaxStep,
		Tool:      ev.Tool,
		Args:      ev.Args,
		Message:   ev.Message,
		Content:   ev.Content,
		ModelMs:   ev.ModelMs,
		Tokens:    ev.Tokens,
		Chars:     ev.Chars,
		ToolMs:    ev.ToolMs,
	})
}

// Path returns the path to the debug log file.
func (d *DebugEmitter) Path() string {
	return d.file.Name()
}

// Close flushes and closes the debug log file.
func (d *DebugEmitter) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.file.Close()
}
