package api

import (
	"net/http"

	"github.com/google/uuid"
)

// handleListRepositories returns all repositories for an organization.
func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
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

	repos, err := s.db.ListOrgRepositories(ctx, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repositories")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
	})
}

// handleGetRepository returns a single repository with analysis count.
func (s *Server) handleGetRepository(w http.ResponseWriter, r *http.Request) {
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

	repo, err := s.db.GetRepositoryByID(ctx, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if repo == nil || repo.OrgID != orgID {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	// Get analysis count
	count, err := s.db.CountRepoAnalyses(ctx, repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count analyses")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"repository":     repo,
		"analysis_count": count,
	})
}
