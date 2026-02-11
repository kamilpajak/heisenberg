package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kamilpajak/heisenberg/pkg/analysis"
	"github.com/kamilpajak/heisenberg/pkg/llm"
	"github.com/kamilpajak/heisenberg/pkg/trace"
)

func (h *Handler) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		http.Error(w, "repo parameter required", http.StatusBadRequest)
		return
	}

	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "invalid repo format, use: owner/repo", http.StatusBadRequest)
		return
	}

	var runID int64
	if rid := r.URL.Query().Get("run_id"); rid != "" {
		var err error
		runID, err = strconv.ParseInt(rid, 10, 64)
		if err != nil {
			http.Error(w, "invalid run_id", http.StatusBadRequest)
			return
		}
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	emitter := NewSSEEmitter(w)
	if emitter == nil {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	result, err := analysis.Run(r.Context(), analysis.Params{
		Owner:        parts[0],
		Repo:         parts[1],
		RunID:        runID,
		Verbose:      true,
		Emitter:      emitter,
		SnapshotHTML: trace.SnapshotHTML,
	})
	if err != nil {
		emitter.Emit(llm.ProgressEvent{Type: "error", Message: err.Error()})
		return
	}

	emitter.Emit(llm.ProgressEvent{Type: "done", Analysis: result})
}
