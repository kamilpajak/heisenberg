package api

import (
	"net/http"

	"github.com/kamilpajak/heisenberg/internal/auth"
)

// handleAuthSync syncs the authenticated user to the database.
// This should be called after login to ensure the user exists in our DB.
func (s *Server) handleAuthSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims := auth.Claims(ctx)
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	kindeUserID := claims.Subject
	email := claims.Email

	if email == "" {
		writeError(w, http.StatusBadRequest, "email not available in token")
		return
	}

	// Create or get user in our database
	user, err := s.db.GetOrCreateUser(ctx, kindeUserID, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sync user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         user.ID,
		"kinde_id":   user.ClerkID, // Note: field name is still ClerkID in DB, stores Kinde ID
		"email":      user.Email,
		"created_at": user.CreatedAt,
	})
}

// handleGetMe returns the current user's information.
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims := auth.Claims(ctx)
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	kindeUserID := claims.Subject

	user, err := s.db.GetUserByClerkID(ctx, kindeUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, "user not found - call /api/auth/sync first")
		return
	}

	// Get user's organizations
	orgs, err := s.db.ListUserOrganizations(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}

	// Include Kinde org info if available
	response := map[string]any{
		"id":            user.ID,
		"kinde_id":      user.ClerkID,
		"email":         user.Email,
		"name":          claims.Name,
		"created_at":    user.CreatedAt,
		"organizations": orgs,
	}

	// Add Kinde org context if present
	if claims.OrgCode != "" {
		response["kinde_org_code"] = claims.OrgCode
		response["kinde_org_name"] = claims.OrgName
	}

	writeJSON(w, http.StatusOK, response)
}
