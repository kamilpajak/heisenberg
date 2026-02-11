package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kamilpajak/heisenberg/pkg/llm"
)

// SSEEmitter implements llm.ProgressEmitter by writing Server-Sent Events.
type SSEEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEEmitter creates an SSEEmitter for the given ResponseWriter.
// Returns nil if the writer does not support flushing.
func NewSSEEmitter(w http.ResponseWriter) *SSEEmitter {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	return &SSEEmitter{w: w, flusher: f}
}

const ssePreviewLimit = 500

// Emit writes a progress event as an SSE data line and flushes.
// Large Preview payloads are truncated to keep SSE lines within reasonable bounds.
func (e *SSEEmitter) Emit(ev llm.ProgressEvent) {
	if len(ev.Preview) > ssePreviewLimit {
		ev.Preview = ev.Preview[:ssePreviewLimit] + "..."
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(e.w, "data: %s\n\n", data)
	e.flusher.Flush()
}
