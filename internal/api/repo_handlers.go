package api

import (
	"net/http"
)

// handleListRepositories returns all repositories for an organization.
func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	repos, err := s.db.ListOrgRepositories(r.Context(), oc.OrgID)
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
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	repoID, err := parseRepoID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	repo, err := s.db.GetRepositoryByID(r.Context(), repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if repo == nil || repo.OrgID != oc.OrgID {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	count, err := s.db.CountRepoAnalyses(r.Context(), repoID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count analyses")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"repository":     repo,
		"analysis_count": count,
	})
}
