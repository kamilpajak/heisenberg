package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kamilpajak/heisenberg/internal/llm"
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

// Emit writes a progress event as an SSE data line and flushes.
func (e *SSEEmitter) Emit(ev llm.ProgressEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(e.w, "data: %s\n\n", data)
	e.flusher.Flush()
}
