package api

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// handleListAnalyses returns analyses for a repository.
func (s *Server) handleListAnalyses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	orgID, err := uuid.Parse(r.PathValue("orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return
	}

	repoID, err := uuid.Parse(r.PathValue("repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	// Check membership
	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return
	}

	// Verify repo belongs to org
	repo, err := s.db.GetRepositoryByID(ctx, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if repo == nil || repo.OrgID != orgID {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Parse query params
	limit := 50
	offset := 0
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

	var category *string
	if c := r.URL.Query().Get("category"); c != "" {
		category = &c
	}

	analyses, err := s.db.ListRepoAnalyses(ctx, database.ListRepoAnalysesParams{
		RepoID:   repoID,
		Limit:    limit,
		Offset:   offset,
		Category: category,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list analyses")
		return
	}

	total, err := s.db.CountRepoAnalyses(ctx, repoID)
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
	ctx := r.Context()
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	orgID, err := uuid.Parse(r.PathValue("orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return
	}

	analysisID, err := uuid.Parse(r.PathValue("analysisID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid analysis ID")
		return
	}

	// Check membership
	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return
	}

	analysis, err := s.db.GetAnalysisByID(ctx, analysisID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if analysis == nil {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	// Verify analysis belongs to a repo in this org
	repo, err := s.db.GetRepositoryByID(ctx, analysis.RepoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if repo == nil || repo.OrgID != orgID {
		writeError(w, http.StatusNotFound, "analysis not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"analysis":   analysis,
		"repository": repo,
	})
}
