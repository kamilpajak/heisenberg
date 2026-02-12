package api

import (
	"net/http"

	"github.com/kamilpajak/heisenberg/internal/auth"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// handleListOrganizations returns all organizations the user belongs to.
func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	orgs, err := s.db.ListUserOrganizations(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"organizations": orgs,
	})
}

// handleCreateOrganization creates a new organization with the user as owner.
func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	org, err := s.db.CreateOrganizationWithOwner(r.Context(), req.Name, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}

	writeJSON(w, http.StatusCreated, org)
}

// handleGetOrganization returns a single organization.
func (s *Server) handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	org, err := s.db.GetOrganizationByID(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if org == nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}

	members, err := s.db.ListOrgMembers(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"organization": org,
		"members":      members,
		"role":         oc.Member.Role,
	})
}

// getCurrentUser returns the database user for the authenticated request.
func (s *Server) getCurrentUser(r *http.Request) (*database.User, error) {
	ctx := r.Context()
	kindeUserID := auth.UserID(ctx)
	if kindeUserID == "" {
		return nil, &authError{"not authenticated"}
	}

	user, err := s.db.GetUserByClerkID(ctx, kindeUserID)
	if err != nil {
		return nil, &authError{"database error"}
	}
	if user == nil {
		return nil, &authError{"user not found - call /api/auth/sync first"}
	}

	return user, nil
}

type authError struct {
	message string
}

func (e *authError) Error() string {
	return e.message
}
