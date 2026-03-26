package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// Handler serves the web dashboard and API endpoints.
type Handler struct {
	mux *http.ServeMux
}

// NewHandler creates a new web handler with all routes registered.
func NewHandler() *Handler {
	h := &Handler{mux: http.NewServeMux()}

	staticFS, _ := fs.Sub(staticFiles, "static")
	h.mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
	h.mux.HandleFunc("GET /api/analyze", h.handleAnalyze)

	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
