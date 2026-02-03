package llm

import (
	"fmt"
	"io"
)

// ProgressEvent represents a single progress update during analysis.
type ProgressEvent struct {
	Type     string          `json:"type"`               // "step", "tool", "result", "info", "done", "error"
	Step     int             `json:"step,omitempty"`      // current iteration
	MaxStep  int             `json:"max,omitempty"`       // max iterations
	Message  string          `json:"message,omitempty"`   // human-readable message
	Tool     string          `json:"tool,omitempty"`      // tool name
	Args     string          `json:"args,omitempty"`      // tool arguments (JSON)
	Preview  string          `json:"preview,omitempty"`   // truncated result preview
	Analysis *AnalysisResult `json:"analysis,omitempty"`  // final analysis (for "done" type)
}

// ProgressEmitter receives progress events during analysis.
type ProgressEmitter interface {
	Emit(event ProgressEvent)
}

// TextEmitter formats progress events as human-readable text for CLI output.
type TextEmitter struct {
	W io.Writer
}

// Emit writes a formatted progress line to the underlying writer.
func (e *TextEmitter) Emit(ev ProgressEvent) {
	switch ev.Type {
	case "step":
		fmt.Fprintf(e.W, "[step %d/%d] %s\n", ev.Step, ev.MaxStep, ev.Message)
	case "tool":
		fmt.Fprintf(e.W, "[step %d/%d] Tool: %s\n", ev.Step, ev.MaxStep, ev.Tool)
		if ev.Args != "" {
			fmt.Fprintf(e.W, "[step %d/%d]   args: %s\n", ev.Step, ev.MaxStep, ev.Args)
		}
	case "result":
		fmt.Fprintf(e.W, "[step %d/%d]   result: %s\n", ev.Step, ev.MaxStep, ev.Preview)
	case "info":
		fmt.Fprintf(e.W, "  %s\n", ev.Message)
	case "error":
		fmt.Fprintf(e.W, "Error: %s\n", ev.Message)
	}
}
