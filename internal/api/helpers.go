package api

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// orgContext holds the validated organization context for a request.
type orgContext struct {
	User   *database.User
	OrgID  uuid.UUID
	Member *database.OrgMember
}

// requireOrgMember validates the user is authenticated and a member of the organization.
// It extracts orgID from the "orgID" path parameter.
func (s *Server) requireOrgMember(w http.ResponseWriter, r *http.Request) (*orgContext, bool) {
	ctx := r.Context()

	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return nil, false
	}

	orgID, err := uuid.Parse(r.PathValue("orgID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return nil, false
	}

	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return nil, false
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return nil, false
	}

	return &orgContext{
		User:   user,
		OrgID:  orgID,
		Member: member,
	}, true
}

// requireOrgAdmin validates the user is authenticated and an owner or admin of the organization.
// It extracts orgID from the request body field "org_id".
func (s *Server) requireOrgAdminFromBody(w http.ResponseWriter, r *http.Request, orgIDStr string) (*orgContext, bool) {
	ctx := r.Context()

	user, err := s.getCurrentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return nil, false
	}

	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid organization ID")
		return nil, false
	}

	member, err := s.db.GetOrgMember(ctx, orgID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return nil, false
	}
	if member == nil {
		writeError(w, http.StatusForbidden, "not a member of this organization")
		return nil, false
	}
	if member.Role != database.RoleOwner && member.Role != database.RoleAdmin {
		writeError(w, http.StatusForbidden, "only owners and admins can manage billing")
		return nil, false
	}

	return &orgContext{
		User:   user,
		OrgID:  orgID,
		Member: member,
	}, true
}

// parseRepoID parses the repository ID from the path parameter.
func parseRepoID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(r.PathValue("repoID"))
}

// parseAnalysisID parses the analysis ID from the path parameter.
func parseAnalysisID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(r.PathValue("analysisID"))
}
