package database

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Repository represents a GitHub repository tracked by an organization.
type Repository struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Owner     string
	Name      string
	FullName  string
	CreatedAt time.Time
}

// CreateRepository creates a new repository.
func (db *DB) CreateRepository(ctx context.Context, orgID uuid.UUID, owner, name string) (*Repository, error) {
	var repo Repository
	err := db.pool.QueryRow(ctx,
		`INSERT INTO repositories (org_id, owner, name)
		 VALUES ($1, $2, $3)
		 RETURNING id, org_id, owner, name, full_name, created_at`,
		orgID, owner, name,
	).Scan(&repo.ID, &repo.OrgID, &repo.Owner, &repo.Name, &repo.FullName, &repo.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetRepositoryByID retrieves a repository by ID.
func (db *DB) GetRepositoryByID(ctx context.Context, id uuid.UUID) (*Repository, error) {
	var repo Repository
	err := db.pool.QueryRow(ctx,
		`SELECT id, org_id, owner, name, full_name, created_at
		 FROM repositories WHERE id = $1`,
		id,
	).Scan(&repo.ID, &repo.OrgID, &repo.Owner, &repo.Name, &repo.FullName, &repo.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetRepositoryByName retrieves a repository by org ID and owner/name.
func (db *DB) GetRepositoryByName(ctx context.Context, orgID uuid.UUID, owner, name string) (*Repository, error) {
	var repo Repository
	err := db.pool.QueryRow(ctx,
		`SELECT id, org_id, owner, name, full_name, created_at
		 FROM repositories WHERE org_id = $1 AND owner = $2 AND name = $3`,
		orgID, owner, name,
	).Scan(&repo.ID, &repo.OrgID, &repo.Owner, &repo.Name, &repo.FullName, &repo.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// GetOrCreateRepository returns the repository with the given owner/name, creating one if necessary.
func (db *DB) GetOrCreateRepository(ctx context.Context, orgID uuid.UUID, owner, name string) (*Repository, error) {
	repo, err := db.GetRepositoryByName(ctx, orgID, owner, name)
	if err != nil {
		return nil, err
	}
	if repo != nil {
		return repo, nil
	}
	return db.CreateRepository(ctx, orgID, owner, name)
}

// ListOrgRepositories returns all repositories for an organization.
func (db *DB) ListOrgRepositories(ctx context.Context, orgID uuid.UUID) ([]Repository, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, org_id, owner, name, full_name, created_at
		 FROM repositories WHERE org_id = $1
		 ORDER BY full_name`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []Repository
	for rows.Next() {
		var repo Repository
		if err := rows.Scan(&repo.ID, &repo.OrgID, &repo.Owner, &repo.Name, &repo.FullName, &repo.CreatedAt); err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

// DeleteRepository deletes a repository by ID.
func (db *DB) DeleteRepository(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM repositories WHERE id = $1`,
		id,
	)
	return err
}
