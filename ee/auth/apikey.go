package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
)

const apiKeyPrefix = "hsb_"

// APIKeyInfo contains the resolved identity from an API key lookup.
type APIKeyInfo struct {
	ID      uuid.UUID
	UserID  uuid.UUID
	OrgID   uuid.UUID
	ClerkID string // User's external auth ID (for context compatibility)
}

// APIKeyStore looks up API keys by their hash.
type APIKeyStore interface {
	GetAPIKeyByHash(ctx context.Context, keyHash string) (APIKeyInfo, error)
	UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error
}

// IsAPIKey returns true if the token has the API key prefix.
func IsAPIKey(token string) bool {
	return strings.HasPrefix(token, apiKeyPrefix)
}

// HashAPIKey returns the SHA-256 hex digest of an API key.
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
