package database

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// APIKey represents a stored API key (hash only, never plaintext).
type APIKey struct {
	ID         uuid.UUID
	KeyHash    string
	UserID     uuid.UUID
	OrgID      uuid.UUID
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateAPIKey stores a new API key hash.
func (db *DB) CreateAPIKey(ctx context.Context, keyHash string, userID, orgID uuid.UUID, name string) (*APIKey, error) {
	var key APIKey
	err := db.pool.QueryRow(ctx,
		`INSERT INTO api_keys (key_hash, user_id, org_id, name)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, key_hash, user_id, org_id, name, created_at, last_used_at`,
		keyHash, userID, orgID, name,
	).Scan(&key.ID, &key.KeyHash, &key.UserID, &key.OrgID, &key.Name, &key.CreatedAt, &key.LastUsedAt)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// GetAPIKeyByHash retrieves an API key by its hash.
func (db *DB) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	var key APIKey
	err := db.pool.QueryRow(ctx,
		`SELECT id, key_hash, user_id, org_id, name, created_at, last_used_at
		 FROM api_keys WHERE key_hash = $1`,
		keyHash,
	).Scan(&key.ID, &key.KeyHash, &key.UserID, &key.OrgID, &key.Name, &key.CreatedAt, &key.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// UpdateAPIKeyLastUsed updates the last_used_at timestamp.
func (db *DB) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

// ListOrgAPIKeys returns all API keys for an organization.
func (db *DB) ListOrgAPIKeys(ctx context.Context, orgID uuid.UUID) ([]APIKey, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, key_hash, user_id, org_id, name, created_at, last_used_at
		 FROM api_keys WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.ID, &key.KeyHash, &key.UserID, &key.OrgID, &key.Name, &key.CreatedAt, &key.LastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// DeleteAPIKey removes an API key.
func (db *DB) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}
