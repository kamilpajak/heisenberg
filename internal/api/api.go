// Package api provides the SaaS API server.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/kamilpajak/heisenberg/internal/auth"
	"github.com/kamilpajak/heisenberg/internal/billing"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// Server is the API server.
type Server struct {
	db            *database.DB
	authVerifier  *auth.Verifier
	billingClient *billing.Client
	usageChecker  *billing.UsageChecker
	mux           *http.ServeMux
}

// Config holds API server configuration.
type Config struct {
	DB            *database.DB
	AuthVerifier  *auth.Verifier
	BillingClient *billing.Client
}

// NewServer creates a new API server.
func NewServer(cfg Config) *Server {
	s := &Server{
		db:            cfg.DB,
		authVerifier:  cfg.AuthVerifier,
		billingClient: cfg.BillingClient,
		usageChecker:  billing.NewUsageChecker(cfg.DB),
		mux:           http.NewServeMux(),
	}

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	authMiddleware := auth.Middleware(s.authVerifier)

	// Public endpoints
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Auth endpoints (protected - user must be authenticated)
	s.mux.HandleFunc("POST /api/auth/sync", s.withAuth(authMiddleware, s.handleAuthSync))

	// Protected endpoints
	s.mux.HandleFunc("GET /api/me", s.withAuth(authMiddleware, s.handleGetMe))
	s.mux.HandleFunc("GET /api/organizations", s.withAuth(authMiddleware, s.handleListOrganizations))
	s.mux.HandleFunc("POST /api/organizations", s.withAuth(authMiddleware, s.handleCreateOrganization))
	s.mux.HandleFunc("GET /api/organizations/{orgID}", s.withAuth(authMiddleware, s.handleGetOrganization))
	s.mux.HandleFunc("GET /api/organizations/{orgID}/repositories", s.withAuth(authMiddleware, s.handleListRepositories))
	s.mux.HandleFunc("GET /api/organizations/{orgID}/repositories/{repoID}", s.withAuth(authMiddleware, s.handleGetRepository))
	s.mux.HandleFunc("GET /api/organizations/{orgID}/repositories/{repoID}/analyses", s.withAuth(authMiddleware, s.handleListAnalyses))
	s.mux.HandleFunc("GET /api/organizations/{orgID}/analyses/{analysisID}", s.withAuth(authMiddleware, s.handleGetAnalysis))
	s.mux.HandleFunc("GET /api/organizations/{orgID}/usage", s.withAuth(authMiddleware, s.handleGetUsage))

	// Billing endpoints
	s.mux.HandleFunc("POST /api/billing/checkout", s.withAuth(authMiddleware, s.handleCreateCheckout))
	s.mux.HandleFunc("POST /api/billing/portal", s.withAuth(authMiddleware, s.handleCreatePortal))
	s.mux.Handle("POST /api/billing/webhook", s.createWebhookHandler())
}

func (s *Server) withAuth(middleware func(http.Handler) http.Handler, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		middleware(http.HandlerFunc(handler)).ServeHTTP(w, r)
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// Close releases resources.
func (s *Server) Close() {
	if s.authVerifier != nil {
		s.authVerifier.Close()
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
