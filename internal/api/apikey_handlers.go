package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/internal/auth"
	"github.com/kamilpajak/heisenberg/internal/database"
)

// handleCreateAPIKey generates a new API key for the organization. Requires admin or owner role.
// The plaintext key is returned once and never stored.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}
	if oc.Member.Role != database.RoleOwner && oc.Member.Role != database.RoleAdmin {
		writeError(w, http.StatusForbidden, "only owners and admins can manage API keys")
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

	// Generate random key with hsb_ prefix
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate key")
		return
	}
	plainKey := "hsb_" + base64.RawURLEncoding.EncodeToString(randomBytes)
	keyHash := auth.HashAPIKey(plainKey)

	key, err := s.db.CreateAPIKey(r.Context(), keyHash, oc.User.ID, oc.OrgID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create API key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   key.ID,
		"name": key.Name,
		"key":  plainKey, // Shown once, never stored
	})
}

// handleListAPIKeys returns all API keys for the organization (without hashes).
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}

	keys, err := s.db.ListOrgAPIKeys(r.Context(), oc.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list API keys")
		return
	}

	// Return without key_hash
	type keyResponse struct {
		ID         uuid.UUID `json:"id"`
		Name       string    `json:"name"`
		CreatedAt  string    `json:"created_at"`
		LastUsedAt *string   `json:"last_used_at,omitempty"`
	}

	result := make([]keyResponse, 0, len(keys))
	for _, k := range keys {
		kr := keyResponse{
			ID:        k.ID,
			Name:      k.Name,
			CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if k.LastUsedAt != nil {
			formatted := k.LastUsedAt.Format("2006-01-02T15:04:05Z")
			kr.LastUsedAt = &formatted
		}
		result = append(result, kr)
	}

	writeJSON(w, http.StatusOK, map[string]any{"keys": result})
}

// handleDeleteAPIKey revokes an API key. Requires admin or owner role.
func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	oc, ok := s.requireOrgMember(w, r)
	if !ok {
		return
	}
	if oc.Member.Role != database.RoleOwner && oc.Member.Role != database.RoleAdmin {
		writeError(w, http.StatusForbidden, "only owners and admins can manage API keys")
		return
	}

	keyID, err := uuid.Parse(r.PathValue("keyID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key ID")
		return
	}

	if err := s.db.DeleteAPIKey(r.Context(), keyID, oc.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete API key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
