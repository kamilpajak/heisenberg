package api

import (
	"net/http"
	"strconv"

	"github.com/kamilpajak/heisenberg/internal/database"
)

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
		writeError(w, http.StatusInternalServerError, "database error")
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

	total, err := s.db.CountRepoAnalyses(r.Context(), repoID)
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
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	// Verify analysis belongs to a repo in this org
	repo, err := s.db.GetRepositoryByID(r.Context(), analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
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
