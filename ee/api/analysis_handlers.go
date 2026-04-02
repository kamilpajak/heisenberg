package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kamilpajak/heisenberg/ee/database"
	"github.com/kamilpajak/heisenberg/pkg/llm"
)

const errDatabase = "database error"

// handleListAnalyses returns analyses for a repository.
func (s *Server) handleListAnalyses(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	repoID, err := parseRepoID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	// Verify repo belongs to org
	repo, err := s.db.GetRepositoryByID(r.Context(), repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Parse query params
	limit, offset := parsePagination(r)
	var category *string
	if c := r.URL.Query().Get("category"); c != "" {
		category = &c
	}

	analyses, err := s.db.ListRepoAnalyses(r.Context(), database.ListRepoAnalysesParams{
		RepoID:   repoID,
		Limit:    limit,
		Offset:   offset,
		Category: category,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list analyses")
		return
	}

	total, err := s.db.CountRepoAnalyses(r.Context(), repoID, category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count analyses")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"analyses": analyses,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// handleGetAnalysis returns a single analysis.
func (s *Server) handleGetAnalysis(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	analysisID, err := parseAnalysisID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid analysis ID")
		return
	}

	analysis, err := s.db.GetAnalysisByID(r.Context(), analysisID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	// Verify analysis belongs to a repo in this org
	repo, err := s.db.GetRepositoryByID(r.Context(), analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errDatabase)
		return
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"analysis":   analysis,
		"repository": repo,
	})
}

// parsePagination extracts limit and offset from query parameters with defaults.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}

// handleCreateAnalysis accepts an analysis result from the CLI and persists it.
func (s *Server) handleCreateAnalysis(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	var req struct {
		Owner       string                  `json:"owner"`
		Repo        string                  `json:"repo"`
		RunID       int64                   `json:"run_id"`
		Branch      string                  `json:"branch"`
		CommitSHA   string                  `json:"commit_sha"`
		Category    string                  `json:"category"`
		Confidence  *int                    `json:"confidence"`
		Sensitivity string                  `json:"sensitivity"`
		RCAs        []llm.RootCauseAnalysis `json:"analyses"`
		Text        string                  `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Owner == "" || req.Repo == "" || req.RunID == 0 || req.Category == "" || req.Text == "" {
		writeError(w, http.StatusBadRequest, "owner, repo, run_id, category, and text are required")
		return
	}

	// Check usage limit
	canAnalyze, err := s.usageChecker.CanAnalyze(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check usage")
		return
	}
	if !canAnalyze {
		writeError(w, http.StatusTooManyRequests, "monthly analysis limit exceeded")
		return
	}

	// Get or create repository
	repo, err := s.db.GetOrCreateRepository(r.Context(), oc.OrgID, req.Owner, req.Repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve repository")
		return
	}

	// Build params
	params := database.CreateAnalysisParams{
		RepoID:   repo.ID,
		RunID:    req.RunID,
		Category: req.Category,
		Text:     req.Text,
		RCAs:     req.RCAs,
	}
	if req.Branch != "" {
		params.Branch = &req.Branch
	}
	if req.CommitSHA != "" {
		params.CommitSHA = &req.CommitSHA
	}
	if req.Confidence != nil {
		params.Confidence = req.Confidence
	}
	if req.Sensitivity != "" {
		params.Sensitivity = &req.Sensitivity
	}

	analysis, err := s.db.CreateAnalysis(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "analysis for this run already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create analysis")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": analysis.ID,
	})
}
