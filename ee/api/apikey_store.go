package api

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/kamilpajak/heisenberg/ee/auth"
	"github.com/kamilpajak/heisenberg/ee/database"
)

// dbAPIKeyStore adapts *database.DB to auth.APIKeyStore.
type dbAPIKeyStore struct {
	db *database.DB
}

func (s *dbAPIKeyStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (auth.APIKeyInfo, error) {
	key, err := s.db.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return auth.APIKeyInfo{}, err
	}
	if key == nil {
		return auth.APIKeyInfo{}, fmt.Errorf("API key not found")
	}

	// Look up user to get their ClerkID for context compatibility
	user, err := s.db.GetUserByID(ctx, key.UserID)
	if err != nil || user == nil {
		return auth.APIKeyInfo{}, fmt.Errorf("API key owner not found")
	}

	return auth.APIKeyInfo{
		ID:      key.ID,
		UserID:  key.UserID,
		OrgID:   key.OrgID,
		ClerkID: user.ClerkID,
	}, nil
}

func (s *dbAPIKeyStore) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	return s.db.UpdateAPIKeyLastUsed(ctx, id)
}
